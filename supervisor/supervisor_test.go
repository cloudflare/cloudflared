package supervisor

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/signal"
)

type mockProtocolSelector struct {
	protocols []connection.Protocol
	index     int
}

func (m *mockProtocolSelector) Current() connection.Protocol {
	return m.protocols[m.index]
}

func (m *mockProtocolSelector) Fallback() (connection.Protocol, bool) {
	m.index++
	if m.index == len(m.protocols) {
		return m.protocols[len(m.protocols)-1], false
	}

	return m.protocols[m.index], true
}

type mockEdgeTunnelServer struct {
	config *TunnelConfig
}

func (m *mockEdgeTunnelServer) Serve(ctx context.Context, connIndex uint8, protocolFallback *protocolFallback, connectedSignal *signal.Signal) error {
	// This is to mock the first connection falling back because of connectivity issues.
	protocolFallback.protocol, _ = m.config.ProtocolSelector.Fallback()
	connectedSignal.Notify()
	return nil
}

// Test to check if initialize sets all the different connections to the same protocol should the first
// tunnel fall back.
func Test_Initialize_Same_Protocol(t *testing.T) {
	edgeIPs, err := edgediscovery.ResolveEdge(&zerolog.Logger{}, "us", allregions.Auto)
	assert.Nil(t, err)
	s := Supervisor{
		edgeIPs: edgeIPs,
		config: &TunnelConfig{
			ProtocolSelector: &mockProtocolSelector{protocols: []connection.Protocol{connection.QUIC, connection.HTTP2, connection.H2mux}},
		},
		tunnelsProtocolFallback: make(map[int]*protocolFallback),
		edgeTunnelServer: &mockEdgeTunnelServer{
			config: &TunnelConfig{
				ProtocolSelector: &mockProtocolSelector{protocols: []connection.Protocol{connection.QUIC, connection.HTTP2, connection.H2mux}},
			},
		},
	}

	ctx := context.Background()
	connectedSignal := signal.New(make(chan struct{}))
	s.initialize(ctx, connectedSignal)

	// Make sure we fell back to http2 as the mock Serve is wont to do.
	assert.Equal(t, s.tunnelsProtocolFallback[0].protocol, connection.HTTP2)

	// Ensure all the protocols we set to try are the same as what the first tunnel has fallen back to.
	for _, protocolFallback := range s.tunnelsProtocolFallback {
		assert.Equal(t, protocolFallback.protocol, s.tunnelsProtocolFallback[0].protocol)
	}
}
