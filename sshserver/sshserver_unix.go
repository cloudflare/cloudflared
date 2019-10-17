//+build !windows

package sshserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/sshgen"
	"github.com/cloudflare/cloudflared/sshlog"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

const (
	auditEventStart       = "session_start"
	auditEventStop        = "session_stop"
	auditEventExec        = "exec"
	auditEventScp         = "scp"
	auditEventResize      = "resize"
	auditEventShell       = "shell"
	sshContextSessionID   = "sessionID"
	sshContextEventLogger = "eventLogger"
	sshContextPreamble    = "sshPreamble"
	sshContextSSHClient   = "sshClient"
	SSHPreambleLength     = 2
	defaultSSHPort        = "22"
)

type auditEvent struct {
	Event       string `json:"event,omitempty"`
	EventType   string `json:"event_type,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	User        string `json:"user,omitempty"`
	Login       string `json:"login,omitempty"`
	Datetime    string `json:"datetime,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Destination string `json:"destination,omitempty"`
}

// sshConn wraps the incoming net.Conn and a cleanup function
// This is done to allow the outgoing SSH client to be retrieved and closed when the conn itself is closed.
type sshConn struct {
	net.Conn
	cleanupFunc func()
}

// close calls the cleanupFunc before closing the conn
func (c sshConn) Close() error {
	c.cleanupFunc()
	return c.Conn.Close()
}

type SSHProxy struct {
	ssh.Server
	hostname   string
	logger     *logrus.Logger
	shutdownC  chan struct{}
	caCert     ssh.PublicKey
	logManager sshlog.Manager
}

type SSHPreamble struct {
	Destination string
	JWT         string
}

// New creates a new SSHProxy and configures its host keys and authentication by the data provided
func New(logManager sshlog.Manager, logger *logrus.Logger, version, localAddress, hostname, hostKeyDir string, shutdownC chan struct{}, idleTimeout, maxTimeout time.Duration) (*SSHProxy, error) {
	sshProxy := SSHProxy{
		hostname:   hostname,
		logger:     logger,
		shutdownC:  shutdownC,
		logManager: logManager,
	}

	sshProxy.Server = ssh.Server{
		Addr:             localAddress,
		MaxTimeout:       maxTimeout,
		IdleTimeout:      idleTimeout,
		Version:          fmt.Sprintf("SSH-2.0-Cloudflare-Access_%s_%s", version, runtime.GOOS),
		PublicKeyHandler: sshProxy.proxyAuthCallback,
		ConnCallback:     sshProxy.connCallback,
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"default": sshProxy.channelHandler,
		},
	}

	if err := sshProxy.configureHostKeys(hostKeyDir); err != nil {
		return nil, err
	}

	return &sshProxy, nil
}

// Start the SSH proxy listener to start handling SSH connections from clients
func (s *SSHProxy) Start() error {
	s.logger.Infof("Starting SSH server at %s", s.Addr)

	go func() {
		<-s.shutdownC
		if err := s.Close(); err != nil {
			s.logger.WithError(err).Error("Cannot close SSH server")
		}
	}()

	return s.ListenAndServe()
}

// proxyAuthCallback attempts to connect to ultimate SSH destination. If successful, it allows the incoming connection
// to connect to the proxy and saves the outgoing SSH client to the context. Otherwise, no connection to the
// the proxy is allowed.
func (s *SSHProxy) proxyAuthCallback(ctx ssh.Context, key ssh.PublicKey) bool {
	client, err := s.dialDestination(ctx)
	if err != nil {
		return false
	}
	ctx.SetValue(sshContextSSHClient, client)
	return true
}

// connCallback reads the preamble sent from the proxy server and saves an audit event logger to the context.
// If any errors occur, the connection is terminated by returning nil from the callback.
func (s *SSHProxy) connCallback(ctx ssh.Context, conn net.Conn) net.Conn {
	// AUTH-2050: This is a temporary workaround of a timing issue in the tunnel muxer to allow further testing.
	// TODO: Remove this
	time.Sleep(10 * time.Millisecond)

	preamble, err := s.readPreamble(conn)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			s.logger.Warn("Could not establish session. Client likely does not have --destination set and is using old-style ssh config")
		} else if err != io.EOF {
			s.logger.WithError(err).Error("failed to read SSH preamble")
		}
		return nil
	}
	ctx.SetValue(sshContextPreamble, preamble)

	logger, sessionID, err := s.auditLogger()
	if err != nil {
		s.logger.WithError(err).Error("failed to configure logger")
		return nil
	}
	ctx.SetValue(sshContextEventLogger, logger)
	ctx.SetValue(sshContextSessionID, sessionID)

	// attempts to retrieve and close the outgoing ssh client when the incoming conn is closed.
	// If no client exists, the conn is being closed before the PublicKeyCallback was called (where the client is created).
	cleanupFunc := func() {
		client, ok := ctx.Value(sshContextSSHClient).(*gossh.Client)
		if ok && client != nil {
			client.Close()
		}
	}

	return sshConn{conn, cleanupFunc}
}

// channelHandler proxies incoming and outgoing SSH traffic back and forth over an SSH Channel
func (s *SSHProxy) channelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	if newChan.ChannelType() != "session" && newChan.ChannelType() != "direct-tcpip" {
		msg := fmt.Sprintf("channel type %s is not supported", newChan.ChannelType())
		s.logger.Info(msg)
		if err := newChan.Reject(gossh.UnknownChannelType, msg); err != nil {
			s.logger.WithError(err).Error("Error rejecting SSH channel")
		}
		return
	}

	localChan, localChanReqs, err := newChan.Accept()
	if err != nil {
		s.logger.WithError(err).Error("Failed to accept session channel")
		return
	}
	defer localChan.Close()

	// client will be closed when the sshConn is closed
	client, ok := ctx.Value(sshContextSSHClient).(*gossh.Client)
	if !ok {
		s.logger.Error("Could not retrieve client from context")
		return
	}

	remoteChan, remoteChanReqs, err := client.OpenChannel(newChan.ChannelType(), newChan.ExtraData())
	if err != nil {
		s.logger.WithError(err).Error("Failed to open remote channel")
		return
	}

	defer remoteChan.Close()

	// Proxy ssh traffic back and forth between client and destination
	s.proxyChannel(localChan, remoteChan, localChanReqs, remoteChanReqs, conn, ctx)
}

// proxyChannel couples two SSH channels and proxies SSH traffic and channel requests back and forth.
func (s *SSHProxy) proxyChannel(localChan, remoteChan gossh.Channel, localChanReqs, remoteChanReqs <-chan *gossh.Request, conn *gossh.ServerConn, ctx ssh.Context) {
	done := make(chan struct{}, 2)
	go func() {
		if _, err := io.Copy(localChan, remoteChan); err != nil {
			s.logger.WithError(err).Error("remote to local copy error")
		}
		done <- struct{}{}
	}()
	go func() {
		if _, err := io.Copy(remoteChan, localChan); err != nil {
			s.logger.WithError(err).Error("local to remote copy error")
		}
		done <- struct{}{}
	}()
	s.logAuditEvent(conn, "", auditEventStart, ctx)
	defer s.logAuditEvent(conn, "", auditEventStop, ctx)

	// Proxy channel requests
	for {
		select {
		case req := <-localChanReqs:
			if req == nil {
				return
			}

			if err := s.forwardChannelRequest(remoteChan, req); err != nil {
				s.logger.WithError(err).Error("Failed to forward request")
				return
			}

			s.logChannelRequest(req, conn, ctx)

		case req := <-remoteChanReqs:
			if req == nil {
				return
			}
			if err := s.forwardChannelRequest(localChan, req); err != nil {
				s.logger.WithError(err).Error("Failed to forward request")
				return
			}
		case <-done:
			return
		}
	}
}

// readPreamble reads a preamble from the SSH connection before any SSH traffic is sent.
// This preamble is a JSON encoded struct containing the users JWT and ultimate destination.
// The first 4 bytes contain the length of the preamble which follows immediately.
func (s *SSHProxy) readPreamble(conn net.Conn) (*SSHPreamble, error) {
	// Set conn read deadline while reading preamble to prevent hangs if preamble wasnt sent.
	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return nil, errors.Wrap(err, "failed to set conn deadline")
	}
	defer func() {
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			s.logger.WithError(err).Error("Failed to unset conn read deadline")
		}
	}()

	size := make([]byte, SSHPreambleLength)
	if _, err := io.ReadFull(conn, size); err != nil {
		return nil, err
	}
	payloadLength := binary.BigEndian.Uint16(size)
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}

	var preamble SSHPreamble
	err := json.Unmarshal(payload, &preamble)
	if err != nil {
		return nil, err
	}

	preamble.Destination, err = canonicalizeDest(preamble.Destination)
	if err != nil {
		return nil, err
	}
	return &preamble, nil
}

// canonicalizeDest adds a default port if one doesnt exist
func canonicalizeDest(dest string) (string, error) {
	_, _, err := net.SplitHostPort(dest)
	// if host and port are split without error, a port exists.
	if err != nil {
		addrErr, ok := err.(*net.AddrError)
		if !ok {
			return "", err
		}
		// If the port is missing, append it.
		if addrErr.Err == "missing port in address" {
			return fmt.Sprintf("%s:%s", dest, defaultSSHPort), nil
		}

		// If there are too many colons and address is IPv6, wrap in brackets and append port. Otherwise invalid address
		ip := net.ParseIP(dest)
		if addrErr.Err == "too many colons in address" && ip != nil && ip.To4() == nil {
			return fmt.Sprintf("[%s]:%s", dest, defaultSSHPort), nil
		}
		return "", addrErr
	}

	return dest, nil
}

// dialDestination creates a new SSH client and dials the destination server
func (s *SSHProxy) dialDestination(ctx ssh.Context) (*gossh.Client, error) {
	preamble, ok := ctx.Value(sshContextPreamble).(*SSHPreamble)
	if !ok {
		msg := "failed to retrieve SSH preamble from context"
		s.logger.Error(msg)
		return nil, errors.New(msg)
	}

	signer, err := s.genSSHSigner(preamble.JWT)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate signed short lived cert")
		return nil, err
	}

	clientConfig := &gossh.ClientConfig{
		User: ctx.User(),
		// AUTH-2103 TODO: proper host key check
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		ClientVersion:   ctx.ServerVersion(),
	}

	client, err := gossh.Dial("tcp", preamble.Destination, clientConfig)
	if err != nil {
		s.logger.WithError(err).Info("Failed to connect to destination SSH server")
		return nil, err
	}
	return client, nil
}

// Generates a key pair and sends public key to get signed by CA
func (s *SSHProxy) genSSHSigner(jwt string) (gossh.Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate ecdsa key pair")
	}

	pub, err := gossh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert ecdsa public key to SSH public key")
	}

	pubBytes := gossh.MarshalAuthorizedKey(pub)
	signedCertBytes, err := sshgen.SignCert(jwt, string(pubBytes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve cert from SSHCAAPI")
	}

	signedPub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(signedCertBytes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse SSH public key")
	}

	cert, ok := signedPub.(*gossh.Certificate)
	if !ok {
		return nil, errors.Wrap(err, "failed to assert public key as certificate")
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create signer")
	}

	certSigner, err := gossh.NewCertSigner(cert, signer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create cert signer")
	}
	return certSigner, nil
}

// forwardChannelRequest sends request req to SSH channel sshChan, waits for reply, and sends the reply back.
func (s *SSHProxy) forwardChannelRequest(sshChan gossh.Channel, req *gossh.Request) error {
	reply, err := sshChan.SendRequest(req.Type, req.WantReply, req.Payload)
	if err != nil {
		return errors.Wrap(err, "Failed to send request")
	}
	if err := req.Reply(reply, nil); err != nil {
		return errors.Wrap(err, "Failed to reply to request")
	}
	return nil
}

// logChannelRequest creates an audit log for different types of channel requests
func (s *SSHProxy) logChannelRequest(req *gossh.Request, conn *gossh.ServerConn, ctx ssh.Context) {
	var eventType string
	var event string
	switch req.Type {
	case "exec":
		var payload struct{ Value string }
		if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
			s.logger.WithError(err).Errorf("Failed to unmarshal channel request payload: %s:%s", req.Type, req.Payload)
		}
		event = payload.Value

		eventType = auditEventExec
		if strings.HasPrefix(string(req.Payload), "scp") {
			eventType = auditEventScp
		}
	case "shell":
		eventType = auditEventShell
	case "window-change":
		eventType = auditEventResize
	default:
		return
	}
	s.logAuditEvent(conn, event, eventType, ctx)
}

func (s *SSHProxy) auditLogger() (io.WriteCloser, string, error) {
	sessionUUID, err := uuid.NewRandom()
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to create sessionID")
	}
	sessionID := sessionUUID.String()

	writer, err := s.logManager.NewLogger(fmt.Sprintf("%s-event.log", sessionID), s.logger)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to create logger")
	}
	return writer, sessionID, nil
}

func (s *SSHProxy) logAuditEvent(conn *gossh.ServerConn, event, eventType string, ctx ssh.Context) {
	sessionID, sessionIDOk := ctx.Value(sshContextSessionID).(string)
	writer, writerOk := ctx.Value(sshContextEventLogger).(io.WriteCloser)
	if !writerOk || !sessionIDOk {
		s.logger.Error("Failed to retrieve audit logger from context")
		return
	}

	var destination string
	preamble, ok := ctx.Value(sshContextPreamble).(*SSHPreamble)
	if ok {
		destination = preamble.Destination
	} else {
		s.logger.Error("Failed to retrieve SSH preamble from context")
	}

	ae := auditEvent{
		Event:       event,
		EventType:   eventType,
		SessionID:   sessionID,
		User:        conn.User(),
		Login:       conn.User(),
		Datetime:    time.Now().UTC().Format(time.RFC3339),
		Hostname:    s.hostname,
		Destination: destination,
	}
	data, err := json.Marshal(&ae)
	if err != nil {
		s.logger.WithError(err).Error("Failed to marshal audit event. malformed audit object")
		return
	}
	line := string(data) + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		s.logger.WithError(err).Error("Failed to write audit event.")
	}
}
