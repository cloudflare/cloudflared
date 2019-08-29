//+build windows

package sshserver

import (
	"errors"

	"github.com/sirupsen/logrus"
	"time"
)

type SSHServer struct{}

func New(_ *logrus.Logger, _ string, _ chan struct{}, _, _ time.Duration) (*SSHServer, error) {
	return nil, errors.New("cloudflared ssh server is not supported on windows")
}

func (s *SSHServer) Start() error {
	return errors.New("cloudflared ssh server is not supported on windows")
}
