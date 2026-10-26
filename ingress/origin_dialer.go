package ingress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const writeDeadlineUDP = 200 * time.Millisecond

// OriginTCPDialer provides a TCP dial operation to a requested address.
type OriginTCPDialer interface {
	DialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error)
}

// OriginUDPDialer provides a UDP dial operation to a requested address.
type OriginUDPDialer interface {
	DialUDP(addr netip.AddrPort) (net.Conn, error)
}

// OriginDialer provides both TCP and UDP dial operations to an address.
type OriginDialer interface {
	OriginTCPDialer
	OriginUDPDialer
}

type OriginConfig struct {
	// The default Dialer used if no reserved services are found for an origin request.
	DefaultDialer OriginDialer
	// Timeout on write operations for TCP connections to the origin.
	TCPWriteTimeout time.Duration
}

// OriginDialerService provides a proxy TCP and UDP dialer to origin services while allowing reserved
// services to be provided. These reserved services are assigned to specific [netip.AddrPort]s
// and provide their own [OriginDialer]'s to handle origin dialing per protocol.
type OriginDialerService struct {
	// Reserved TCP services for reserved AddrPort values
	reservedTCPServices map[netip.AddrPort]OriginTCPDialer
	// Reserved UDP services for reserved AddrPort values
	reservedUDPServices map[netip.AddrPort]OriginUDPDialer
	// The default Dialer used if no reserved services are found for an origin request
	defaultDialer  OriginDialer
	defaultDialerM sync.RWMutex
	// Write timeout for TCP connections
	writeTimeout time.Duration

	logger *zerolog.Logger
}

func NewOriginDialer(config OriginConfig, logger *zerolog.Logger) *OriginDialerService {
	return &OriginDialerService{
		reservedTCPServices: map[netip.AddrPort]OriginTCPDialer{},
		reservedUDPServices: map[netip.AddrPort]OriginUDPDialer{},
		defaultDialer:       config.DefaultDialer,
		writeTimeout:        config.TCPWriteTimeout,
		logger:              logger,
	}
}

// AddReservedService adds a reserved virtual service to dial to.
// Not locked and expected to be initialized before calling first dial and not afterwards.
func (d *OriginDialerService) AddReservedService(service OriginDialer, addrs []netip.AddrPort) {
	for _, addr := range addrs {
		d.reservedTCPServices[addr] = service
		d.reservedUDPServices[addr] = service
	}
}

// UpdateDefaultDialer updates the default dialer.
func (d *OriginDialerService) UpdateDefaultDialer(dialer *Dialer) {
	d.defaultDialerM.Lock()
	defer d.defaultDialerM.Unlock()
	d.defaultDialer = dialer
}

// DialTCP will perform a dial TCP to the requested addr.
func (d *OriginDialerService) DialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	conn, err := d.dialTCP(ctx, addr)
	if err != nil {
		return nil, err
	}
	// Assign the write timeout for the TCP operations
	return &tcpConnection{
		Conn:         conn,
		writeTimeout: d.writeTimeout,
		logger:       d.logger,
	}, nil
}

func (d *OriginDialerService) dialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	// Check to see if any reserved services are available for this addr and call their dialer instead.
	if dialer, ok := d.reservedTCPServices[addr]; ok {
		return dialer.DialTCP(ctx, addr)
	}
	d.defaultDialerM.RLock()
	dialer := d.defaultDialer
	d.defaultDialerM.RUnlock()
	return dialer.DialTCP(ctx, addr)
}

// DialUDP will perform a dial UDP to the requested addr.
func (d *OriginDialerService) DialUDP(addr netip.AddrPort) (net.Conn, error) {
	// Check to see if any reserved services are available for this addr and call their dialer instead.
	if dialer, ok := d.reservedUDPServices[addr]; ok {
		return dialer.DialUDP(addr)
	}
	d.defaultDialerM.RLock()
	dialer := d.defaultDialer
	d.defaultDialerM.RUnlock()
	return dialer.DialUDP(addr)
}

type Dialer struct {
	Dialer net.Dialer
}

func NewDialer(config WarpRoutingConfig) *Dialer {
	return &Dialer{
		Dialer: net.Dialer{
			Timeout:   config.ConnectTimeout.Duration,
			KeepAlive: config.TCPKeepAlive.Duration,
		},
	}
}

func (d *Dialer) DialTCP(ctx context.Context, dest netip.AddrPort) (net.Conn, error) {
	conn, err := d.Dialer.DialContext(ctx, "tcp", dest.String())
	if err != nil {
		return nil, fmt.Errorf("unable to dial tcp to origin %s: %w", dest, err)
	}

	return conn, nil
}

func (d *Dialer) DialUDP(dest netip.AddrPort) (net.Conn, error) {
	conn, err := d.Dialer.Dial("udp", dest.String())
	if err != nil {
		return nil, fmt.Errorf("unable to dial udp to origin %s: %w", dest, err)
	}
	return &writeDeadlineConn{
		Conn: conn,
	}, nil
}

// writeDeadlineConn is a wrapper around a net.Conn that sets a write deadline of 200ms.
// This is to prevent the socket from blocking on the write operation if it were to occur. However,
// we typically never expect this to occur except under high load or kernel issues.
type writeDeadlineConn struct {
	net.Conn
}

func (w *writeDeadlineConn) Write(b []byte) (int, error) {
	if err := w.SetWriteDeadline(time.Now().Add(writeDeadlineUDP)); err != nil {
		return 0, err
	}
	return w.Conn.Write(b)
}
