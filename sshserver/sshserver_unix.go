//+build !windows

package sshserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/cloudflare/cloudflared/sshlog"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	auditEventAuth   = "auth"
	auditEventStart  = "session_start"
	auditEventStop   = "session_stop"
	auditEventExec   = "exec"
	auditEventScp    = "scp"
	auditEventResize = "resize"
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

// SSHServer adds on to the ssh.Server of the gliderlabs package
type SSHServer struct {
	ssh.Server
	logger     *logrus.Logger
	shutdownC  chan struct{}
	caCert     ssh.PublicKey
	logManager sshlog.Manager
}

// New creates a new SSHServer and configures its host keys and authenication by the data provided
func New(logManager sshlog.Manager, logger *logrus.Logger, version, address string, shutdownC chan struct{}, idleTimeout, maxTimeout time.Duration, enablePortForwarding bool) (*SSHServer, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	if currentUser.Uid != "0" {
		return nil, errors.New("cloudflared SSH server needs to run as root")
	}

	forwardHandler := &ssh.ForwardedTCPHandler{}
	sshServer := SSHServer{
		Server: ssh.Server{
			Addr:        address,
			MaxTimeout:  maxTimeout,
			IdleTimeout: idleTimeout,
			Version:     fmt.Sprintf("SSH-2.0-Cloudflare-Access_%s_%s", version, runtime.GOOS),
			// Register SSH global Request handlers to respond to tcpip forwarding
			RequestHandlers: map[string]ssh.RequestHandler{
				"tcpip-forward":        forwardHandler.HandleSSHRequest,
				"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
			},
			// Register SSH channel types
			ChannelHandlers: map[string]ssh.ChannelHandler{
				"session":      ssh.DefaultSessionHandler,
				"direct-tcpip": ssh.DirectTCPIPHandler,
			},
		},
		logger:     logger,
		shutdownC:  shutdownC,
		logManager: logManager,
	}

	// AUTH-2050: This is a temporary workaround of a timing issue in the tunnel muxer to allow further testing.
	// TODO: Remove this 
	sshServer.ConnCallback = func(conn net.Conn) net.Conn {
		time.Sleep(10 * time.Millisecond)
		return conn
	}

	if enablePortForwarding {
		sshServer.LocalPortForwardingCallback = allowForward
		sshServer.ReversePortForwardingCallback = allowForward
	}

	if err := sshServer.configureHostKeys(); err != nil {
		return nil, err
	}

	sshServer.configureAuthentication()

	return &sshServer, nil
}

// Start the SSH server listener to start handling SSH connections from clients
func (s *SSHServer) Start() error {
	s.logger.Infof("Starting SSH server at %s", s.Addr)

	go func() {
		<-s.shutdownC
		if err := s.Close(); err != nil {
			s.logger.WithError(err).Error("Cannot close SSH server")
		}
	}()

	s.Handle(s.connectionHandler)
	return s.ListenAndServe()
}

func (s *SSHServer) connectionHandler(session ssh.Session) {
	sessionUUID, err := uuid.NewRandom()

	if err != nil {
		if _, err := io.WriteString(session, "Failed to generate session ID\n"); err != nil {
			s.logger.WithError(err).Error("Failed to generate session ID: Failed to write to SSH session")
		}
		s.errorAndExit(session, "", nil)
		return
	}
	sessionID := sessionUUID.String()

	eventLogger, err := s.logManager.NewLogger(fmt.Sprintf("%s-event.log", sessionID), s.logger)
	if err != nil {
		if _, err := io.WriteString(session, "Failed to create event log\n"); err != nil {
			s.logger.WithError(err).Error("Failed to create event log: Failed to write to create event logger")
		}
		s.errorAndExit(session, "", nil)
		return
	}

	// Get uid and gid of user attempting to login
	sshUser, uidInt, gidInt, success := s.getSSHUser(session, sessionID, eventLogger)
	if !success {
		return
	}

	// Spawn shell under user
	cmd := s.spawnCmd(session, sshUser, uidInt, gidInt)

	var shellInput io.WriteCloser
	var shellOutput io.ReadCloser
	pr, pw := io.Pipe()
	defer pw.Close()

	ptyReq, winCh, isPty := session.Pty()

	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		tty, err := s.startPtySession(cmd, winCh, func() {
			s.logAuditEvent(eventLogger, session, sessionID, auditEventResize)
		})
		shellInput = tty
		shellOutput = tty
		if err != nil {
			s.logger.WithError(err).Error("Failed to start pty session")
			close(s.shutdownC)
			return
		}
		s.logAuditEvent(eventLogger, session, sessionID, auditEventStart)
		defer s.logAuditEvent(eventLogger, session, sessionID, auditEventStop)
	} else {
		var shellError io.ReadCloser
		shellInput, shellOutput, shellError, err = s.startNonPtySession(cmd)
		if err != nil {
			s.logger.WithError(err).Error("Failed to start non-pty session")
			close(s.shutdownC)
			return
		}
		event := auditEventExec
		if strings.HasPrefix(session.RawCommand(), "scp") {
			event = auditEventScp
		}
		s.logAuditEvent(eventLogger, session, sessionID, event)

		// Write stderr to both the command recorder, and remote user
		go func() {
			mw := io.MultiWriter(pw, session.Stderr())
			if _, err := io.Copy(mw, shellError); err != nil {
				s.logger.WithError(err).Error("Failed to write stderr to user")
			}
		}()
	}

	sessionLogger, err := s.logManager.NewSessionLogger(fmt.Sprintf("%s-session.log", sessionID), s.logger)
	if err != nil {
		if _, err := io.WriteString(session, "Failed to create log\n"); err != nil {
			s.logger.WithError(err).Error("Failed to create log: Failed to write to SSH session")
		}
		s.errorAndExit(session, "", nil)
		return
	}
	defer sessionLogger.Close()
	go func() {
		defer pr.Close()
		_, err := io.Copy(sessionLogger, pr)
		if err != nil {
			s.logger.WithError(err).Error("Failed to write session log")
		}
	}()

	// Write stdin to shell
	go func() {
		defer shellInput.Close()
		if _, err := io.Copy(shellInput, session); err != nil {
			s.logger.WithError(err).Error("Failed to write incoming command to pty")
		}
	}()

	// Write stdout to both the command recorder, and remote user
	mw := io.MultiWriter(pw, session)
	if _, err := io.Copy(mw, shellOutput); err != nil {
		s.logger.WithError(err).Error("Failed to write stdout to user")
	}

	// Wait for all resources associated with cmd to be released
	// Returns error if shell exited with a non-zero status or received a signal
	if err := cmd.Wait(); err != nil {
		s.logger.WithError(err).Debug("Shell did not close correctly")
	}
}

// spawnCmd spawns a shell under the user
func (s *SSHServer) spawnCmd(session ssh.Session, sshUser *User, uidInt, gidInt uint32) *exec.Cmd {
	var cmd *exec.Cmd
	if session.RawCommand() != "" {
		cmd = exec.Command(sshUser.Shell, "-c", session.RawCommand())
	} else {
		cmd = exec.Command(sshUser.Shell)
	}
	// Supplementary groups are not explicitly specified. They seem to be inherited by default.
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uidInt, Gid: gidInt}, Setsid: true}
	cmd.Env = append(cmd.Env, session.Environ()...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("USER=%s", sshUser.Username))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", sshUser.HomeDir))
	cmd.Dir = sshUser.HomeDir
	return cmd
}

// getSSHUser gets the ssh user, uid, and gid of the user attempting to login
func (s *SSHServer) getSSHUser(session ssh.Session, sessionID string, eventLogger io.WriteCloser) (*User, uint32, uint32, bool) {
	// Get uid and gid of user attempting to login
	sshUser, ok := session.Context().Value("sshUser").(*User)
	if !ok || sshUser == nil {
		s.errorAndExit(session, "Error retrieving credentials from session", nil)
		return nil, 0, 0, false
	}
	s.logAuditEvent(eventLogger, session, sessionID, auditEventAuth)

	uidInt, err := stringToUint32(sshUser.Uid)
	if err != nil {
		s.errorAndExit(session, "Invalid user", err)
		return sshUser, 0, 0, false
	}
	gidInt, err := stringToUint32(sshUser.Gid)
	if err != nil {
		s.errorAndExit(session, "Invalid user group", err)
		return sshUser, 0, 0, false
	}
	return sshUser, uidInt, gidInt, true
}

// errorAndExit reports an error with the session and exits
func (s *SSHServer) errorAndExit(session ssh.Session, errText string, err error) {
	if exitError := session.Exit(1); exitError != nil {
		s.logger.WithError(exitError).Error("Failed to close SSH session")
	} else if err != nil {
		s.logger.WithError(err).Error(errText)
	} else if errText != "" {
		s.logger.Error(errText)
	}
}

func (s *SSHServer) startNonPtySession(cmd *exec.Cmd) (stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, err error) {
	stdin, err = cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	if err = cmd.Start(); err != nil {
		return
	}
	return
}

func (s *SSHServer) startPtySession(cmd *exec.Cmd, winCh <-chan ssh.Window, logCallback func()) (io.ReadWriteCloser, error) {
	tty, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	// Handle terminal window size changes
	go func() {
		for win := range winCh {
			if errNo := setWinsize(tty, win.Width, win.Height); errNo != 0 {
				s.logger.WithError(err).Error("Failed to set pty window size")
				close(s.shutdownC)
				return
			}
			logCallback()
		}
	}()

	return tty, nil
}

func (s *SSHServer) logAuditEvent(writer io.WriteCloser, session ssh.Session, sessionID string, eventType string) {
	username := "unknown"
	sshUser, ok := session.Context().Value("sshUser").(*User)
	if ok && sshUser != nil {
		username = sshUser.Username
	}

	event := auditEvent{
		Event:     session.RawCommand(),
		EventType: eventType,
		SessionID: sessionID,
		User:      username,
		Login:     username,
		Datetime:  time.Now().UTC().Format(time.RFC3339),
		IPAddress: session.RemoteAddr().String(),
	}
	data, err := json.Marshal(&event)
	if err != nil {
		s.logger.WithError(err).Error("Failed to marshal audit event. malformed audit object")
		return
	}
	line := string(data) + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		s.logger.WithError(err).Error("Failed to write audit event.")
	}

}

// Sets PTY window size for terminal
func setWinsize(f *os.File, w, h int) syscall.Errno {
	_, _, errNo := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
	return errNo
}

func stringToUint32(str string) (uint32, error) {
	uid, err := strconv.ParseUint(str, 10, 32)
	return uint32(uid), err
}

func allowForward(_ ssh.Context, _ string, _ uint32) bool {
	return true
}
