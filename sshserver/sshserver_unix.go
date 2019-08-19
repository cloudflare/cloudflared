//+build !windows

package sshserver

import (
	"bufio"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/sirupsen/logrus"
)

const (
	defaultShellPrompt = `\e[0;31m[\u@\h \W]\$ \e[m `
	configDir          = "/etc/cloudflared/"
)

type SSHServer struct {
	ssh.Server
	logger    *logrus.Logger
	shutdownC chan struct{}
}

func New(logger *logrus.Logger, address string, shutdownC chan struct{}) (*SSHServer, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}
	if currentUser.Uid != "0" {
		return nil, errors.New("cloudflared SSH server needs to run as root")
	}

	sshServer := SSHServer{ssh.Server{Addr: address}, logger, shutdownC}
	if err := sshServer.configureHostKeys(); err != nil {
		return nil, err
	}

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
	uid, gid, err := getUser(session.User())
	if err != nil {
		if _, err := io.WriteString(session, "Invalid credentials\n"); err != nil {
			s.logger.WithError(err).Error("Invalid credentials: Failed to write to SSH session")
		}
		if err := session.Exit(1); err != nil {
			s.logger.WithError(err).Error("Failed to close SSH session")
		}
		return
	}

	// Spawn shell under user
	cmd := exec.Command("/bin/bash")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}

	ptyReq, winCh, isPty := session.Pty()
	if !isPty {
		if _, err := io.WriteString(session, "No PTY requested.\n"); err != nil {
			s.logger.WithError(err).Error("No PTY requested: Failed to write to SSH session")
		}

		if err := session.Exit(1); err != nil {
			s.logger.WithError(err).Error("Failed to close SSH session")
		}
		return
	}

	cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
	cmd.Env = append(cmd.Env, fmt.Sprintf("PS1=%s", defaultShellPrompt))
	psuedoTTY, err := pty.Start(cmd)
	if err != nil {
		s.logger.WithError(err).Error("Failed to start pty session")
		if err := session.Exit(1); err != nil {
			s.logger.WithError(err).Error("Failed to close SSH session")
		}
		close(s.shutdownC)
		return
	}

	// Handle terminal window size changes
	go func() {
		for win := range winCh {
			if errNo := setWinsize(psuedoTTY, win.Width, win.Height); errNo != 0 {
				s.logger.WithError(err).Error("Failed to set pty window size: ", err.Error())
				if err := session.Exit(1); err != nil {
					s.logger.WithError(err).Error("Failed to close SSH session")
				}
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

// Sets PTY window size for terminal
func setWinsize(f *os.File, w, h int) syscall.Errno {
	_, _, errNo := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
	return errNo
}

// Only works on POSIX systems
func getUser(username string) (uint32, uint32, error) {
	sshUser, err := user.Lookup(username)
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.ParseUint(sshUser.Uid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.ParseUint(sshUser.Gid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return uint32(uid), uint32(gid), nil
}
