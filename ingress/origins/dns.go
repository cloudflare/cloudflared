package origins

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ingress"
)

const (
	// We need a DNS record:
	// 1. That will be around for as long as cloudflared is
	// 2. That Cloudflare controls: to allow us to make changes if needed
	// 3. That is an external record to a typical customer's network: enforcing that the DNS request go to the
	//    local DNS resolver over any local /etc/host configurations setup.
	// 4. That cloudflared would normally query: ensuring that users with a positive security model for DNS queries
	//    don't need to adjust anything.
	//
	// This hostname is one that used during the edge discovery process and as such satisfies the above constraints.
	defaultLookupHost          = "region1.v2.argotunnel.com"
	defaultResolverPort uint16 = 53

	// We want the refresh time to be short to accommodate DNS resolver changes locally, but not too frequent as to
	// shuffle the resolver if multiple are configured.
	refreshFreq    = 5 * time.Minute
	refreshTimeout = 5 * time.Second
)

var (
	// Virtual DNS service address
	VirtualDNSServiceAddr = netip.AddrPortFrom(netip.MustParseAddr("2606:4700:0cf1:2000:0000:0000:0000:0001"), 53)

	defaultResolverAddr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), defaultResolverPort)
)

type netDial func(network string, address string) (net.Conn, error)

// DNSResolverService will make DNS requests to the local DNS resolver via the Dial method.
type DNSResolverService struct {
	address  netip.AddrPort
	addressM sync.RWMutex

	dialer   ingress.UDPOriginProxy
	resolver peekResolver
	logger   *zerolog.Logger
}

func NewDNSResolver(logger *zerolog.Logger) *DNSResolverService {
	return &DNSResolverService{
		address:  defaultResolverAddr,
		dialer:   ingress.DefaultUDPDialer,
		resolver: &resolver{dialFunc: net.Dial},
		logger:   logger,
	}
}

func (s *DNSResolverService) DialUDP(_ netip.AddrPort) (net.Conn, error) {
	s.addressM.RLock()
	dest := s.address
	s.addressM.RUnlock()
	// The dialer ignores the provided address because the request will instead go to the local DNS resolver.
	return s.dialer.DialUDP(dest)
}

// StartRefreshLoop is a routine that is expected to run in the background to update the DNS local resolver if
// adjusted while the cloudflared process is running.
func (s *DNSResolverService) StartRefreshLoop(ctx context.Context) {
	// Call update first to load an address before handling traffic
	err := s.update(ctx)
	if err != nil {
		s.logger.Err(err).Msg("Failed to initialize DNS local resolver")
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.Tick(refreshFreq):
			err := s.update(ctx)
			if err != nil {
				s.logger.Err(err).Msg("Failed to refresh DNS local resolver")
			}
		}
	}
}

func (s *DNSResolverService) update(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	// Make a standard DNS request to a well-known DNS record that will last a long time
	_, err := s.resolver.lookupNetIP(ctx, defaultLookupHost)
	if err != nil {
		return err
	}

	// Validate the address before updating internal reference
	_, address := s.resolver.addr()
	peekAddrPort, err := netip.ParseAddrPort(address)
	if err == nil {
		s.setAddress(peekAddrPort)
		return nil
	}
	// It's possible that the address didn't have an attached port, attempt to parse just the address and use
	// the default port 53
	peekAddr, err := netip.ParseAddr(address)
	if err != nil {
		return err
	}
	s.setAddress(netip.AddrPortFrom(peekAddr, defaultResolverPort))
	return nil
}

// lock and update the address used for the local DNS resolver
func (s *DNSResolverService) setAddress(addr netip.AddrPort) {
	s.addressM.Lock()
	defer s.addressM.Unlock()
	if s.address != addr {
		s.logger.Debug().Msgf("Updating DNS local resolver: %s", addr)
	}
	s.address = addr
}

type peekResolver interface {
	addr() (network string, address string)
	lookupNetIP(ctx context.Context, host string) ([]netip.Addr, error)
}

// resolver is a shim that inspects the go runtime's DNS resolution process to capture the DNS resolver
// address used to complete a DNS request.
type resolver struct {
	network  string
	address  string
	dialFunc netDial
}

func (r *resolver) addr() (network string, address string) {
	return r.network, r.address
}

func (r *resolver) lookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		// Use the peekDial to inspect the results of the DNS resolver used during the LookupIPAddr call.
		Dial: r.peekDial,
	}
	return resolver.LookupNetIP(ctx, "ip", host)
}

func (r *resolver) peekDial(ctx context.Context, network, address string) (net.Conn, error) {
	r.network = network
	r.address = address
	return r.dialFunc(network, address)
}
