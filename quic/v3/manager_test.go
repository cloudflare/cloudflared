package v3_test

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/mocks"

	cfdflow "github.com/cloudflare/cloudflared/flow"
	"github.com/cloudflare/cloudflared/ingress"
	v3 "github.com/cloudflare/cloudflared/quic/v3"
)

var (
	testDefaultDialer = ingress.NewDialer(ingress.WarpRoutingConfig{
		ConnectTimeout: config.CustomDuration{Duration: 1 * time.Second},
		TCPKeepAlive:   config.CustomDuration{Duration: 15 * time.Second},
		MaxActiveFlows: 0,
	})
)

func TestRegisterSession(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	manager := v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0))

	request := v3.UDPSessionRegistrationDatagram{
		RequestID:        testRequestID,
		Dest:             netip.MustParseAddrPort("127.0.0.1:5000"),
		Traced:           false,
		IdleDurationHint: 5 * time.Second,
		Payload:          nil,
	}
	session, err := manager.RegisterSession(&request, &noopEyeball{})
	if err != nil {
		t.Fatalf("register session should've succeeded: %v", err)
	}
	if request.RequestID != session.ID() {
		t.Fatalf("session id doesn't match: %v != %v", request.RequestID, session.ID())
	}

	// We shouldn't be able to register another session with the same request id
	_, err = manager.RegisterSession(&request, &noopEyeball{})
	if !errors.Is(err, v3.ErrSessionAlreadyRegistered) {
		t.Fatalf("session is already registered for this connection: %v", err)
	}

	// We shouldn't be able to register another session with the same request id for a different connection
	_, err = manager.RegisterSession(&request, &noopEyeball{connID: 1})
	if !errors.Is(err, v3.ErrSessionBoundToOtherConn) {
		t.Fatalf("session is already registered for a separate connection: %v", err)
	}

	// Get session
	sessionGet, err := manager.GetSession(request.RequestID)
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if session.ID() != sessionGet.ID() {
		t.Fatalf("session's do not match: %v != %v", session.ID(), sessionGet.ID())
	}

	// Remove the session
	manager.UnregisterSession(request.RequestID)

	// Get session should fail
	_, err = manager.GetSession(request.RequestID)
	if !errors.Is(err, v3.ErrSessionNotFound) {
		t.Fatalf("get session failed: %v", err)
	}

	// Closing the original session should return that the socket is already closed (by the session unregistration)
	err = session.Close()
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatalf("session should've closed without issue: %v", err)
	}
}

func TestGetSession_Empty(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	manager := v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, cfdflow.NewLimiter(0))

	_, err := manager.GetSession(testRequestID)
	if !errors.Is(err, v3.ErrSessionNotFound) {
		t.Fatalf("get session find no session: %v", err)
	}
}

func TestRegisterSessionRateLimit(t *testing.T) {
	log := zerolog.Nop()
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 0,
	}, &log)
	ctrl := gomock.NewController(t)

	flowLimiterMock := mocks.NewMockLimiter(ctrl)

	flowLimiterMock.EXPECT().Acquire("udp").Return(cfdflow.ErrTooManyActiveFlows)
	flowLimiterMock.EXPECT().Release().Times(0)

	manager := v3.NewSessionManager(&noopMetrics{}, &log, originDialerService, flowLimiterMock)

	request := v3.UDPSessionRegistrationDatagram{
		RequestID:        testRequestID,
		Dest:             netip.MustParseAddrPort("127.0.0.1:5000"),
		Traced:           false,
		IdleDurationHint: 5 * time.Second,
		Payload:          nil,
	}
	_, err := manager.RegisterSession(&request, &noopEyeball{})
	require.ErrorIs(t, err, v3.ErrSessionRegistrationRateLimited)
}
