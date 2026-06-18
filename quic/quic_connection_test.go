package quic

import (
	"testing"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

// mockCloser is an [io.Closer] that returns a configurable error.
type mockCloser struct {
	closeErr error
}

func (m *mockCloser) Close() error {
	return m.closeErr
}

func TestNewConnWithCloser_NilConn(t *testing.T) {
	t.Parallel()
	conn, err := NewQUICConnection(nil, &mockCloser{})
	require.ErrorIs(t, err, ErrNilQuicConnection)
	require.Nil(t, conn)
}

func TestNewConnWithCloser_NilCloser(t *testing.T) {
	t.Parallel()
	conn, err := NewQUICConnection(&quic.Conn{}, nil)
	require.ErrorIs(t, err, ErrNilCloser)
	require.Nil(t, conn)
}
