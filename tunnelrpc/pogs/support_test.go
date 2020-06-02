package pogs

import (
	"context"

	"github.com/google/uuid"
)

// mockTunnelServerBase provides a placeholder implementation
// for TunnelServer interface that can be used to build
// mocks for specific unit tests without having to implement every method
type mockTunnelServerBase struct{}

func (mockTunnelServerBase) Register(ctx context.Context, auth []byte, tunnelUUID uuid.UUID, connIndex byte, options *ConnectionOptions) (*ConnectionDetails, error) {
	panic("unexpected call to Register")
}

func (mockTunnelServerBase) Unregister(ctx context.Context) {
	panic("unexpected call to Unregister")
}

func (mockTunnelServerBase) RegisterTunnel(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) *TunnelRegistration {
	panic("unexpected call to RegisterTunnel")
}

func (mockTunnelServerBase) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	panic("unexpected call to GetServerInfo")
}

func (mockTunnelServerBase) UnregisterTunnel(ctx context.Context, gracePeriodNanoSec int64) error {
	panic("unexpected call to UnregisterTunnel")
}

func (mockTunnelServerBase) Authenticate(ctx context.Context, originCert []byte, hostname string, options *RegistrationOptions) (*AuthenticateResponse, error) {
	panic("unexpected call to Authenticate")
}

func (mockTunnelServerBase) ReconnectTunnel(ctx context.Context, jwt, eventDigest, connDigest []byte, hostname string, options *RegistrationOptions) (*TunnelRegistration, error) {
	panic("unexpected call to ReconnectTunnel")
}

