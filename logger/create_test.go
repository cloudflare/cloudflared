package logger

import (
	"io"
	"testing"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

type mockedWriter struct {
	wantErr    bool
	writeCalls int
}

func (c *mockedWriter) Write(p []byte) (int, error) {
	c.writeCalls++

	if c.wantErr {
		return -1, errors.New("Expected error")
	}

	return len(p), nil
}

// Tests that a new writer is only used if it actually works.
func TestResilientMultiWriter_Errors(t *testing.T) {
	tests := []struct {
		name    string
		writers []*mockedWriter
	}{
		{
			name: "All valid writers",
			writers: []*mockedWriter{
				{
					wantErr: false,
				},
				{
					wantErr: false,
				},
			},
		},
		{
			name: "All invalid writers",
			writers: []*mockedWriter{
				{
					wantErr: true,
				},
				{
					wantErr: true,
				},
			},
		},
		{
			name: "First invalid writer",
			writers: []*mockedWriter{
				{
					wantErr: true,
				},
				{
					wantErr: false,
				},
			},
		},
		{
			name: "First valid writer",
			writers: []*mockedWriter{
				{
					wantErr: false,
				},
				{
					wantErr: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writers := []io.Writer{}
			for _, w := range test.writers {
				writers = append(writers, w)
			}
			multiWriter := resilientMultiWriter{zerolog.InfoLevel, writers, nil}

			logger := zerolog.New(multiWriter).With().Timestamp().Logger()
			logger.Info().Msg("Test msg")

			for _, w := range test.writers {
				// Expect each writer to be written to regardless of the previous writers returning an error
				assert.Equal(t, 1, w.writeCalls)
			}
		})
	}
}

type mockedManagementWriter struct {
	WriteCalls int
}

func (c *mockedManagementWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *mockedManagementWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	c.WriteCalls++
	return len(p), nil
}

// Tests that management writer receives write calls of all levels except Disabled
func TestResilientMultiWriter_Management(t *testing.T) {
	for _, level := range []zerolog.Level{
		zerolog.DebugLevel,
		zerolog.InfoLevel,
		zerolog.WarnLevel,
		zerolog.ErrorLevel,
		zerolog.FatalLevel,
		zerolog.PanicLevel,
	} {
		t.Run(level.String(), func(t *testing.T) {
			managementWriter := mockedManagementWriter{}
			multiWriter := resilientMultiWriter{level, []io.Writer{&mockedWriter{}}, &managementWriter}

			logger := zerolog.New(multiWriter).With().Timestamp().Logger()
			logger.Info().Msg("Test msg")

			// Always write to management
			assert.Equal(t, 1, managementWriter.WriteCalls)
		})
	}
}
