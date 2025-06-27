package ingress

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/rs/zerolog"
)

// UDPOriginService provides a proxy UDP dialer to origin services while allowing reserved
// services to be provided. These reserved services are assigned to specific [netip.AddrPort]s
// and provide their own [UDPOriginProxy]s to handle UDP origin dialing.
type UDPOriginService struct {
	// Reserved services for reserved AddrPort values
	reservedServices map[netip.AddrPort]UDPOriginProxy
	// The default UDP Dialer used if no reserved services are found for an origin request.
	defaultDialer UDPOriginProxy

	logger *zerolog.Logger
}

// UDPOriginProxy provides a UDP dial operation to a requested addr.
type UDPOriginProxy interface {
	DialUDP(addr netip.AddrPort) (net.Conn, error)
}

func NewUDPOriginService(reserved map[netip.AddrPort]UDPOriginProxy, logger *zerolog.Logger) *UDPOriginService {
	return &UDPOriginService{
		reservedServices: reserved,
		defaultDialer:    DefaultUDPDialer,
		logger:           logger,
	}
}

// SetUDPDialer updates the default UDP Dialer used.
// Typically used in unit testing.
func (s *UDPOriginService) SetDefaultDialer(dialer UDPOriginProxy) {
	s.defaultDialer = dialer
}

// DialUDP will perform a dial UDP to the requested addr.
func (s *UDPOriginService) DialUDP(addr netip.AddrPort) (net.Conn, error) {
	// Check to see if any reserved services are available for this addr and call their dialer instead.
	if dialer, ok := s.reservedServices[addr]; ok {
		return dialer.DialUDP(addr)
	}
	return s.defaultDialer.DialUDP(addr)
}

type defaultUDPDialer struct{}

var DefaultUDPDialer UDPOriginProxy = &defaultUDPDialer{}

func (d *defaultUDPDialer) DialUDP(dest netip.AddrPort) (net.Conn, error) {
	addr := net.UDPAddrFromAddrPort(dest)

	// We use nil as local addr to force runtime to find the best suitable local address IP given the destination
	// address as context.
	udpConn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("unable to dial udp to origin %s: %w", dest, err)
	}

	return udpConn, nil
}
