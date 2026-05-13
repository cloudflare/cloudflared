package prechecks

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection/dialopts"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

// DNSResolver abstracts edge DNS discovery used by DNS probes.
type DNSResolver interface {
	// Resolve performs edge discovery for the given region string and returns
	// the resolved edge regions.
	Resolve(region string) ([][]*allregions.EdgeAddr, error)
}

// TCPDialer abstracts the TCP + TLS handshake used by HTTP/2 connectivity probes.
type TCPDialer interface {
	// DialEdge dials the given edge TCP address with TLS and returns the
	// established connection. The caller is responsible for closing the connection.
	DialEdge(ctx context.Context, timeout time.Duration, tlsConfig *tls.Config, addr *net.TCPAddr, localIP net.IP) (net.Conn, error)
}

// QUICDialer abstracts the UDP + QUIC handshake used by QUIC connectivity probes.
type QUICDialer interface {
	// DialQuic performs a QUIC handshake to the given edge address and returns
	// the established connection. The caller is responsible for closing the
	// connection. connIndex is used for UDP port reuse bookkeeping consistent
	// with the production dial path.
	DialQuic(
		ctx context.Context,
		quicConfig *quic.Config,
		tlsConfig *tls.Config,
		addr netip.AddrPort,
		localAddr net.IP,
		connIndex uint8,
		logger *zerolog.Logger,
		opts dialopts.DialOpts,
	) (quic.Connection, error)
}

// ManagementDialer abstracts the TCP dial to api.cloudflare.com:443 used by
// the Management API probe.
type ManagementDialer interface {
	// DialContext opens a TCP connection to the given network address. The
	// caller is responsible for closing the connection.
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}
