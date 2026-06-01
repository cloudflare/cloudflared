package quic

import (
	"errors"
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

// mockQuicConnection is a minimal test double for [quic.Connection].
type mockQuicConnection struct {
	quic.Connection
	closeWithErrorErr error
}

func (m *mockQuicConnection) CloseWithError(_ quic.ApplicationErrorCode, _ string) error {
	return m.closeWithErrorErr
}

func TestNewConnWithCloser_NilConn(t *testing.T) {
	t.Parallel()
	conn, err := NewQUICConnection(nil, &mockCloser{})
	require.ErrorIs(t, err, ErrNilQuicConnection)
	require.Nil(t, conn)
}

func TestNewConnWithCloser_NilCloser(t *testing.T) {
	t.Parallel()
	conn, err := NewQUICConnection(&mockQuicConnection{}, nil)
	require.ErrorIs(t, err, ErrNilCloser)
	require.Nil(t, conn)
}

func TestNewConnWithCloser_Success(t *testing.T) {
	t.Parallel()
	qc := &mockQuicConnection{}
	cl := &mockCloser{}
	conn, err := NewQUICConnection(qc, cl)
	require.NoError(t, err)
	require.NotNil(t, conn)
}

func TestConnWithCloser_CloseWithError_BothSucceed(t *testing.T) {
	t.Parallel()
	qc := &mockQuicConnection{}
	cl := &mockCloser{}
	conn, err := NewQUICConnection(qc, cl)
	require.NoError(t, err)

	err = conn.CloseWithError(0, "test")
	require.NoError(t, err)
}

func TestConnWithCloser_CloseWithError_QuicFails(t *testing.T) {
	t.Parallel()
	quicErr := errors.New("quic close failed")
	qc := &mockQuicConnection{closeWithErrorErr: quicErr}
	cl := &mockCloser{}
	conn, err := NewQUICConnection(qc, cl)
	require.NoError(t, err)

	err = conn.CloseWithError(0, "test")
	require.ErrorIs(t, err, quicErr)
}

func TestConnWithCloser_CloseWithError_CloserFails(t *testing.T) {
	t.Parallel()
	closerErr := errors.New("closer failed")
	qc := &mockQuicConnection{}
	cl := &mockCloser{closeErr: closerErr}
	conn, err := NewQUICConnection(qc, cl)
	require.NoError(t, err)

	err = conn.CloseWithError(0, "test")
	require.ErrorIs(t, err, closerErr)
}

func TestConnWithCloser_CloseWithError_BothFail(t *testing.T) {
	t.Parallel()
	quicErr := errors.New("quic close failed")
	closerErr := errors.New("closer failed")
	qc := &mockQuicConnection{closeWithErrorErr: quicErr}
	cl := &mockCloser{closeErr: closerErr}
	conn, err := NewQUICConnection(qc, cl)
	require.NoError(t, err)

	err = conn.CloseWithError(0, "test")
	require.ErrorIs(t, err, quicErr)
	require.ErrorIs(t, err, closerErr)
}

// TestConnWithCloser_ImplementsInterface is a runtime assertion that
// *ConnWithCloser satisfies QUICConnection. The compile-time assertion is in
// quic_connection.go.
func TestConnWithCloser_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ QUICConnection = (*ConnWithCloser)(nil)
}
