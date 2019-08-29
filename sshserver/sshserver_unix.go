//+build !windows

package sshserver

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/sirupsen/logrus"
)

type SSHServer struct {
	ssh.Server
	logger      *logrus.Logger
	shutdownC   chan struct{}
	caCert      ssh.PublicKey
	getUserFunc func(string) (*User, error)
}

func New(logger *logrus.Logger, address string, shutdownC chan struct{}, idleTimeout, maxTimeout time.Duration) (*SSHServer, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	if currentUser.Uid != "0" {
		return nil, errors.New("cloudflared SSH server needs to run as root")
	}

	sshServer := SSHServer{
		Server:      ssh.Server{Addr: address, MaxTimeout: maxTimeout, IdleTimeout: idleTimeout},
		logger:      logger,
		shutdownC:   shutdownC,
		getUserFunc: lookupUser,
	}

	if err := sshServer.configureHostKeys(); err != nil {
		return nil, err
	}

	sshServer.configureAuthentication()
	return &sshServer, nil
}

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
	// Get uid and gid of user attempting to login
	sshUser, ok := session.Context().Value("sshUser").(*User)
	if !ok || sshUser == nil {
		s.logger.Error("Error retrieving credentials from session")
		s.CloseSession(session)
		return
	}

	// Spawn shell under user
	cmd := exec.Command(sshUser.Shell)

	ptyReq, winCh, isPty := session.Pty()
	if !isPty {
		if _, err := io.WriteString(session, "No PTY requested.\n"); err != nil {
			s.logger.WithError(err).Error("No PTY requested: Failed to write to SSH session")
		}

		s.CloseSession(session)
		return
	}

	uidInt, err := stringToUint32(sshUser.Uid)
	if err != nil {
		s.logger.WithError(err).Error("Invalid user")
		s.CloseSession(session)
		return
	}
	gidInt, err := stringToUint32(sshUser.Gid)
	if err != nil {
		s.logger.WithError(err).Error("Invalid user group")
		s.CloseSession(session)
		return
	}

	// Supplementary groups are not explicitly specified. They seem to be inherited by default.
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uidInt, Gid: gidInt}}
	cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
	cmd.Env = append(cmd.Env, fmt.Sprintf("USER=%s", sshUser.Username))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", sshUser.HomeDir))
	cmd.Dir = sshUser.HomeDir
	psuedoTTY, err := pty.Start(cmd)
	if err != nil {
		s.logger.WithError(err).Error("Failed to start pty session")
		s.CloseSession(session)
		close(s.shutdownC)
		return
	}

	// Handle terminal window size changes
	go func() {
		for win := range winCh {
			if errNo := setWinsize(psuedoTTY, win.Width, win.Height); errNo != 0 {
				s.logger.WithError(err).Error("Failed to set pty window size: ", err.Error())
				s.CloseSession(session)
				close(s.shutdownC)
				return
			}
		}
	}()

	// Write incoming commands to PTY
	go func() {
		if _, err := io.Copy(psuedoTTY, session); err != nil {
			s.logger.WithError(err).Error("Failed to write incoming command to pty")
		}
	}()
	pr, pw := io.Pipe()
	scanner := bufio.NewScanner(pr)
	go func() {
		for scanner.Scan() {
			s.logger.Info(scanner.Text())
		}
	}()

	// Write outgoing command output to both the command recorder, and remote user
	mw := io.MultiWriter(pw, session)
	if _, err := io.Copy(mw, psuedoTTY); err != nil {
		s.logger.WithError(err).Error("Failed to write command output to user")
	}

	if err := pw.Close(); err != nil {
		s.logger.WithError(err).Error("Failed to close pipe writer")
	}

	if err := pr.Close(); err != nil {
		s.logger.WithError(err).Error("Failed to close pipe reader")
	}

	// Wait for all resources associated with cmd to be released
	// Returns error if shell exited with a non-zero status or received a signal
	if err := cmd.Wait(); err != nil {
		s.logger.WithError(err).Debug("Shell did not close correctly")
	}
}

func (s *SSHServer) CloseSession(session ssh.Session) {
	if err := session.Exit(1); err != nil {
		s.logger.WithError(err).Error("Failed to close SSH session")
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
