//+build !windows

package sshserver

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

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
	sshContextDestination = "sshDest"
	sshPreambleLength     = 4
)

type auditEvent struct {
	Event       string `json:"event,omitempty"`
	EventType   string `json:"event_type,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	User        string `json:"user,omitempty"`
	Login       string `json:"login,omitempty"`
	Datetime    string `json:"datetime,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type SSHProxy struct {
	ssh.Server
	logger     *logrus.Logger
	shutdownC  chan struct{}
	caCert     ssh.PublicKey
	logManager sshlog.Manager
}

// New creates a new SSHProxy and configures its host keys and authentication by the data provided
func New(logManager sshlog.Manager, logger *logrus.Logger, version, address string, shutdownC chan struct{}, idleTimeout, maxTimeout time.Duration) (*SSHProxy, error) {
	sshProxy := SSHProxy{
		logger:     logger,
		shutdownC:  shutdownC,
		logManager: logManager,
	}

	sshProxy.Server = ssh.Server{
		Addr:         address,
		MaxTimeout:   maxTimeout,
		IdleTimeout:  idleTimeout,
		Version:      fmt.Sprintf("SSH-2.0-Cloudflare-Access_%s_%s", version, runtime.GOOS),
		ConnCallback: sshProxy.connCallback,
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"default": sshProxy.channelHandler,
		},
	}

	if err := sshProxy.configureHostKeys(); err != nil {
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

func (s *SSHProxy) connCallback(ctx ssh.Context, conn net.Conn) net.Conn {
	// AUTH-2050: This is a temporary workaround of a timing issue in the tunnel muxer to allow further testing.
	// TODO: Remove this
	time.Sleep(10 * time.Millisecond)

	if err := s.configureSSHDestination(conn, ctx); err != nil {
		if err != io.EOF {
			s.logger.WithError(err).Error("failed to read SSH destination")
		}
		return nil
	}

	if err := s.configureLogger(ctx); err != nil {
		s.logger.WithError(err).Error("failed to configure logger")
		return nil
	}
	return conn
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

	// AUTH-2136 TODO: multiplex ssh client between channels
	client, err := s.createSSHClient(ctx)
	if err != nil {
		s.logger.WithError(err).Error("Failed to dial remote server")
		return
	}
	defer client.Close()

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

// configureSSHDestination reads a preamble from the SSH connection before any SSH traffic is sent.
// This preamble contains the ultimate SSH destination the proxy will connect too.
// The first 4 bytes contain the length of the destination which follows immediately.
func (s *SSHProxy) configureSSHDestination(conn net.Conn, ctx ssh.Context) error {
	size := make([]byte, sshPreambleLength)
	if _, err := io.ReadFull(conn, size); err != nil {
		return err
	}
	payloadLength := binary.BigEndian.Uint32(size)
	data := make([]byte, payloadLength)
	if _, err := io.ReadFull(conn, data); err != nil {
		return err
	}

	destAddr := string(data)
	destUrl, err := url.Parse(destAddr)
	if err != nil {
		return errors.Wrap(err, "failed to parse URL")
	}
	if destUrl.Port() == "" {
		destAddr += ":22"
	}
	ctx.SetValue(sshContextDestination, destAddr)
	return nil
}

// createSSHClient creates a new SSH client and dials the destination server
func (s *SSHProxy) createSSHClient(ctx ssh.Context) (*gossh.Client, error) {
	clientConfig := &gossh.ClientConfig{
		User: ctx.User(),
		// AUTH-2103 TODO: proper host key check
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		//  AUTH-2114 TODO: replace with short lived cert auth
		Auth:          []gossh.AuthMethod{gossh.Password("test")},
		ClientVersion: ctx.ServerVersion(),
	}

	address, ok := ctx.Value(sshContextDestination).(string)
	if !ok {
		return nil, errors.New("failed to retrieve SSH destination from context")
	}
	client, err := gossh.Dial("tcp", address, clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
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

func (s *SSHProxy) configureLogger(ctx ssh.Context) error {
	sessionUUID, err := uuid.NewRandom()
	if err != nil {
		return errors.Wrap(err, "failed to create sessionID")
	}
	sessionID := sessionUUID.String()

	writer, err := s.logManager.NewLogger(fmt.Sprintf("%s-event.log", sessionID), s.logger)
	if err != nil {
		return errors.Wrap(err, "failed to create logger")
	}
	ctx.SetValue(sshContextEventLogger, writer)
	ctx.SetValue(sshContextSessionID, sessionID)
	return nil
}

func (s *SSHProxy) logAuditEvent(conn *gossh.ServerConn, event, eventType string, ctx ssh.Context) {
	sessionID, sessionIDOk := ctx.Value(sshContextSessionID).(string)
	writer, writerOk := ctx.Value(sshContextEventLogger).(io.WriteCloser)
	if !writerOk || !sessionIDOk {
		s.logger.Error("Failed to retrieve audit logger from context")
		return
	}

	destination, destOk := ctx.Value(sshContextDestination).(string)
	if !destOk {
		s.logger.Error("Failed to retrieve SSH destination from context")
	}

	ae := auditEvent{
		Event:       event,
		EventType:   eventType,
		SessionID:   sessionID,
		User:        conn.User(),
		Login:       conn.User(),
		Datetime:    time.Now().UTC().Format(time.RFC3339),
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
