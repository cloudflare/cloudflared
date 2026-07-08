package connection

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cfdflow "github.com/cloudflare/cloudflared/flow"
	"github.com/cloudflare/cloudflared/mocks"
)

type mockQuicConnection struct{}

func (m *mockQuicConnection) AcceptStream(_ context.Context) (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) AcceptUniStream(_ context.Context) (quic.ReceiveStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenStream() (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenStreamSync(_ context.Context) (quic.Stream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenUniStream() (quic.SendStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) OpenUniStreamSync(_ context.Context) (quic.SendStream, error) {
	return nil, nil
}

func (m *mockQuicConnection) LocalAddr() net.Addr {
	return nil
}

func (m *mockQuicConnection) RemoteAddr() net.Addr {
	return nil
}

func (m *mockQuicConnection) CloseWithError(_ quic.ApplicationErrorCode, s string) error {
	return nil
}

func (m *mockQuicConnection) Context() context.Context {
	return nil
}

func (m *mockQuicConnection) ConnectionState() quic.ConnectionState {
	panic("not meant to be called")
}

func (m *mockQuicConnection) SendDatagram(_ []byte) error {
	return nil
}

func (m *mockQuicConnection) ReceiveDatagram(_ context.Context) ([]byte, error) {
	return nil, nil
}

func (m *mockQuicConnection) AddPath(*quic.Transport) (*quic.Path, error) {
	return nil, nil
}

func TestRateLimitOnNewDatagramV2UDPSession(t *testing.T) {
	log := zerolog.Nop()
	conn := &mockQuicConnection{}
	ctrl := gomock.NewController(t)
	flowLimiterMock := mocks.NewMockLimiter(ctrl)

	datagramConn := NewDatagramV2Connection(
		t.Context(),
		conn,
		nil,
		nil,
		0,
		0*time.Second,
		0*time.Second,
		flowLimiterMock,
		&log,
	)

	flowLimiterMock.EXPECT().Acquire("udp").Return(cfdflow.ErrTooManyActiveFlows)
	flowLimiterMock.EXPECT().Release().Times(0)

	_, err := datagramConn.RegisterUdpSession(t.Context(), uuid.New(), net.IPv4(0, 0, 0, 0), 1000, 1*time.Second, "")
	require.ErrorIs(t, err, cfdflow.ErrTooManyActiveFlows)
}
