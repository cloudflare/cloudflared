package sshlog

import (
	"io"

	"github.com/sirupsen/logrus"
)

// Manager be managing logs bruh
type Manager interface {
	NewLogger(string, *logrus.Logger) (io.WriteCloser, error)
}

type manager struct{}

// New creates a new instance of a log manager
func New() Manager {
	return &manager{}
}

func (m *manager) NewLogger(name string, logger *logrus.Logger) (io.WriteCloser, error) {
	return NewLogger(name, logger)
}
