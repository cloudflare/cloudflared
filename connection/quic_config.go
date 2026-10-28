package connection

import (
	"net/netip"

	"github.com/quic-go/quic-go"

	cfdquic "github.com/cloudflare/cloudflared/quic"
)

// QUICConfigOptions contains the configurable edge QUIC transport settings.
type QUICConfigOptions struct {
	DisablePathMTUDiscovery    bool
	MaxConnectionReceiveWindow uint64
	MaxStreamReceiveWindow     uint64
}

// NewQUICConfig builds a fresh edge transport config for tunnel connections and
// connectivity probes. Callers may attach their own per-connection tracer.
func NewQUICConfig(edgeAddr netip.AddrPort, options QUICConfigOptions) *quic.Config {
	initialPacketSize := cfdquic.InitialPacketSizeIPv6
	if edgeAddr.Addr().Is4() {
		initialPacketSize = cfdquic.InitialPacketSizeIPv4
	}

	return &quic.Config{
		HandshakeIdleTimeout:       cfdquic.HandshakeIdleTimeout,
		MaxIdleTimeout:             cfdquic.MaxIdleTimeout,
		KeepAlivePeriod:            cfdquic.MaxIdlePingPeriod,
		MaxIncomingStreams:         cfdquic.MaxIncomingStreams,
		MaxIncomingUniStreams:      cfdquic.MaxIncomingStreams,
		EnableDatagrams:            true,
		DisablePathMTUDiscovery:    options.DisablePathMTUDiscovery,
		MaxConnectionReceiveWindow: options.MaxConnectionReceiveWindow,
		MaxStreamReceiveWindow:     options.MaxStreamReceiveWindow,
		InitialPacketSize:          initialPacketSize,
	}
}
