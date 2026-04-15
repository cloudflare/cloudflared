package prechecks

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

// DNSResolver abstracts edge DNS discovery used by DNS probes.
//
// The production implementation wraps allregions.EdgeDiscovery
// (edgediscovery/allregions/discovery.go), which performs an SRV lookup for
// _v2-origintunneld._tcp.argotunnel.com, falls back to DNS-over-TLS when the
// system resolver fails, and resolves each discovered hostname via
// net.LookupIP. The returned slice already has each address tagged with
// .IPVersion = V4 or V6.
//
// Note: allregions.EdgeDiscovery must be exported (currently unexported as
// edgeDiscovery) before a production adapter can be wired up.
type DNSResolver interface {
	// Resolve performs edge discovery for the given region string (empty for
	// global, "us" / "fed" for regional endpoints) and returns the resolved
	// addresses grouped by CNAME target, mirroring the structure returned by
	// allregions.EdgeDiscovery.
	Resolve(region string) ([][]*allregions.EdgeAddr, error)
}

// TCPDialer abstracts the TCP + TLS handshake used by HTTP/2 connectivity probes.
//
// The production implementation wraps edgediscovery.DialEdge
// (edgediscovery/dial.go), which is the same function supervisor/tunnel.go
// uses for production HTTP/2 connections. Reusing it ensures the pre-check
// validates the identical dial path the tunnel will take.
type TCPDialer interface {
	// DialEdge dials the given edge TCP address with TLS, respecting the
	// provided timeout, and returns the established connection. The caller is
	// responsible for closing the connection.
	DialEdge(ctx context.Context, timeout time.Duration, tlsConfig *tls.Config, addr *net.TCPAddr, localIP net.IP) (net.Conn, error)
}

// QUICDialer abstracts the UDP + QUIC handshake used by QUIC connectivity probes.
//
// The production implementation wraps connection.DialQuic
// (connection/quic.go), which is the same function supervisor/tunnel.go uses
// for production QUIC connections. The pre-check performs a handshake only —
// no streams are opened and no RPC frames are sent — to avoid triggering the
// OTD registration timeout described in TUN-6732.
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
	) (quic.Connection, error)
}

// ManagementDialer abstracts the TCP dial to api.cloudflare.com:443 used by
// the Management API probe.
//
// A successful TCP connection (no TLS handshake required) is sufficient to
// confirm that port 443 is reachable. This probe is always a soft failure:
// the tunnel can run without it, but automatic software updates will be
// unavailable.
type ManagementDialer interface {
	// DialContext opens a TCP connection to the given network address. The
	// caller is responsible for closing the connection.
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}
