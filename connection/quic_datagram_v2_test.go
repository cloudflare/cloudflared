package connection

import (
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cfdflow "github.com/cloudflare/cloudflared/flow"
	"github.com/cloudflare/cloudflared/mocks"
)

func TestRateLimitOnNewDatagramV2UDPSession(t *testing.T) {
	log := zerolog.Nop()
	ctrl := gomock.NewController(t)
	flowLimiterMock := mocks.NewMockLimiter(ctrl)
	connMock := mocks.NewMockQUICConnection(ctrl)

	datagramConn := NewDatagramV2Connection(
		t.Context(),
		connMock,
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
