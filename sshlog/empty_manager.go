package sshlog

import (
	"io"

	"github.com/cloudflare/cloudflared/logger"
)

//empty manager implements the Manager but does nothing (for testing and to disable logging unless the logs are set)
type emptyManager struct {
}

type emptyWriteCloser struct {
}

// NewEmptyManager creates a new instance of a log empty log manager that does nothing
func NewEmptyManager() Manager {
	return &emptyManager{}
}

func (m *emptyManager) NewLogger(name string, logger logger.Service) (io.WriteCloser, error) {
	return &emptyWriteCloser{}, nil
}

func (m *emptyManager) NewSessionLogger(name string, logger logger.Service) (io.WriteCloser, error) {
	return &emptyWriteCloser{}, nil
}

// emptyWriteCloser

func (w *emptyWriteCloser) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (w *emptyWriteCloser) Close() error {
	return nil
}
