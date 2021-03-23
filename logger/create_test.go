package logger

import (
	"io"
	"testing"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

var writeCalls int

type mockedWriter struct {
	wantErr bool
}

func (c mockedWriter) Write(p []byte) (int, error) {
	writeCalls++

	if c.wantErr {
		return -1, errors.New("Expected error")
	}

	return len(p), nil
}

// Tests that a new writer is only used if it actually works.
func TestResilientMultiWriter(t *testing.T) {
	tests := []struct {
		name    string
		writers []io.Writer
	}{
		{
			name: "All valid writers",
			writers: []io.Writer{
				mockedWriter{
					wantErr: false,
				},
				mockedWriter{
					wantErr: false,
				},
			},
		},
		{
			name: "All invalid writers",
			writers: []io.Writer{
				mockedWriter{
					wantErr: true,
				},
				mockedWriter{
					wantErr: true,
				},
			},
		},
		{
			name: "First invalid writer",
			writers: []io.Writer{
				mockedWriter{
					wantErr: true,
				},
				mockedWriter{
					wantErr: false,
				},
			},
		},
		{
			name: "First valid writer",
			writers: []io.Writer{
				mockedWriter{
					wantErr: false,
				},
				mockedWriter{
					wantErr: true,
				},
			},
		},
	}

	for _, tt := range tests {
		writers := tt.writers
		multiWriter := resilientMultiWriter{writers}

		logger := zerolog.New(multiWriter).With().Timestamp().Logger().Level(zerolog.InfoLevel)
		logger.Info().Msg("Test msg")

		assert.Equal(t, len(writers), writeCalls)
		writeCalls = 0
	}
}
