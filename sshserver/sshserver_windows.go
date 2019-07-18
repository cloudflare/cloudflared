//+build windows

package sshserver

import (
	"github.com/sirupsen/logrus"
)

type SSHServer struct{}

func New(_ *logrus.Logger, _ string, _ chan struct{}) (*SSHServer, error) {
	return nil, nil
}

func (s *SSHServer) Start() error {
	return nil
}
