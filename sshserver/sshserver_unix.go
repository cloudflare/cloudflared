//+build !windows

package sshserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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
)

type auditEvent struct {
	Event     string `json:"event,omitempty"`
	EventType string `json:"event_type,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	User      string `json:"user,omitempty"`
	Login     string `json:"login,omitempty"`
	Datetime  string `json:"datetime,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
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
		Addr:        address,
		MaxTimeout:  maxTimeout,
		IdleTimeout: idleTimeout,
		Version:     fmt.Sprintf("SSH-2.0-Cloudflare-Access_%s_%s", version, runtime.GOOS),
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session": sshProxy.channelHandler,
		},
	}

	// AUTH-2050: This is a temporary workaround of a timing issue in the tunnel muxer to allow further testing.
	// TODO: Remove this
	sshProxy.ConnCallback = func(conn net.Conn) net.Conn {
		time.Sleep(10 * time.Millisecond)
		return conn
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

// channelHandler proxies incoming and outgoing SSH traffic back and forth over an SSH Channel
func (s *SSHProxy) channelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	if err := s.configureAuditLogger(ctx); err != nil {
		s.logger.WithError(err).Error("Failed to configure audit logging")
		return
	}

	clientConfig := &gossh.ClientConfig{
		User: conn.User(),
		// AUTH-2103 TODO: proper host key check
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		//  AUTH-2114 TODO: replace with short lived cert auth
		Auth:          []gossh.AuthMethod{gossh.Password("test")},
		ClientVersion: s.Version,
	}

	switch newChan.ChannelType() {
	case "session":
		// Accept incoming channel request from client
		localChan, localChanReqs, err := newChan.Accept()
		if err != nil {
			s.logger.WithError(err).Error("Failed to accept session channel")
			return
		}
		defer localChan.Close()

		// AUTH-2088 TODO: retrieve ssh target from tunnel
		// Create outgoing ssh connection to destination SSH server
		client, err := gossh.Dial("tcp", "localhost:22", clientConfig)
		if err != nil {
			s.logger.WithError(err).Error("Failed to dial remote server")
			return
		}
		defer client.Close()

		// Open channel session channel to destination server
		remoteChan, remoteChanReqs, err := client.OpenChannel("session", []byte{})
		if err != nil {
			s.logger.WithError(err).Error("Failed to open remote channel")
			return
		}

		defer remoteChan.Close()

		// Proxy ssh traffic back and forth between client and destination
		s.proxyChannel(localChan, remoteChan, localChanReqs, remoteChanReqs, conn, ctx)
	}
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
	}
	s.logAuditEvent(conn, event, eventType, ctx)
}

func (s *SSHProxy) configureAuditLogger(ctx ssh.Context) error {
	sessionUUID, err := uuid.NewRandom()
	if err != nil {
		return errors.New("failed to generate session ID")
	}
	sessionID := sessionUUID.String()

	eventLogger, err := s.logManager.NewLogger(fmt.Sprintf("%s-event.log", sessionID), s.logger)
	if err != nil {
		return errors.New("failed to create event log")
	}

	ctx.SetValue(sshContextSessionID, sessionID)
	ctx.SetValue(sshContextEventLogger, eventLogger)
	return nil
}

func (s *SSHProxy) logAuditEvent(conn *gossh.ServerConn, event, eventType string, ctx ssh.Context) {
	sessionID, ok := ctx.Value(sshContextSessionID).(string)
	if !ok {
		s.logger.Error("Failed to retrieve sessionID from context")
		return
	}
	writer, ok := ctx.Value(sshContextEventLogger).(io.WriteCloser)
	if !ok {
		s.logger.Error("Failed to retrieve eventLogger from context")
		return
	}

	ae := auditEvent{
		Event:     event,
		EventType: eventType,
		SessionID: sessionID,
		User:      conn.User(),
		Login:     conn.User(),
		Datetime:  time.Now().UTC().Format(time.RFC3339),
		IPAddress: conn.RemoteAddr().String(),
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
