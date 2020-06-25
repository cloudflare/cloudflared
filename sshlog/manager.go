package sshlog

import (
	"io"
	"path/filepath"
	"time"

	"github.com/cloudflare/cloudflared/logger"
)

// Manager be managing logs bruh
type Manager interface {
	NewLogger(string, logger.Service) (io.WriteCloser, error)
	NewSessionLogger(string, logger.Service) (io.WriteCloser, error)
}

type manager struct {
	baseDirectory string
}

// New creates a new instance of a log manager
func New(baseDirectory string) Manager {
	return &manager{
		baseDirectory: baseDirectory,
	}
}

func (m *manager) NewLogger(name string, logger logger.Service) (io.WriteCloser, error) {
	return NewLogger(filepath.Join(m.baseDirectory, name), logger, time.Second, defaultFileSizeLimit)
}

func (m *manager) NewSessionLogger(name string, logger logger.Service) (io.WriteCloser, error) {
	return NewSessionLogger(filepath.Join(m.baseDirectory, name), logger, time.Second, defaultFileSizeLimit)
}
