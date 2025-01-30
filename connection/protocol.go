package connection

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/edgediscovery"
)

const (
	AvailableProtocolFlagMessage = "Available protocols: 'auto' - automatically chooses the best protocol over time (the default; and also the recommended one); 'quic' - based on QUIC, relying on UDP egress to Cloudflare edge; 'http2' - using Go's HTTP2 library, relying on TCP egress to Cloudflare edge"
	// edgeH2muxTLSServerName is the server name to establish h2mux connection with edge (unused, but kept for legacy reference).
	_ = "cftunnel.com"
	// edgeH2TLSServerName is the server name to establish http2 connection with edge
	edgeH2TLSServerName = "h2.cftunnel.com"
	// edgeQUICServerName is the server name to establish quic connection with edge.
	edgeQUICServerName = "quic.cftunnel.com"
	AutoSelectFlag     = "auto"
	// SRV and TXT record resolution TTL
	ResolveTTL = time.Hour
)

// ProtocolList represents a list of supported protocols for communication with the edge
// in order of precedence for remote percentage fetcher.
var ProtocolList = []Protocol{QUIC, HTTP2}

type Protocol int64

const (
	// HTTP2 using golang HTTP2 library for edge connections.
	HTTP2 Protocol = iota
	// QUIC using quic-go for edge connections.
	QUIC
)

// Fallback returns the fallback protocol and whether the protocol has a fallback
func (p Protocol) fallback() (Protocol, bool) {
	switch p {
	case HTTP2:
		return 0, false
	case QUIC:
		return HTTP2, true
	default:
		return 0, false
	}
}

func (p Protocol) String() string {
	switch p {
	case HTTP2:
		return "http2"
	case QUIC:
		return "quic"
	default:
		return "unknown protocol"
	}
}

func (p Protocol) TLSSettings() *TLSSettings {
	switch p {
	case HTTP2:
		return &TLSSettings{
			ServerName: edgeH2TLSServerName,
		}
	case QUIC:
		return &TLSSettings{
			ServerName: edgeQUICServerName,
			NextProtos: []string{"argotunnel"},
		}
	default:
		return nil
	}
}

type TLSSettings struct {
	ServerName string
	NextProtos []string
}

type ProtocolSelector interface {
	Current() Protocol
	Fallback() (Protocol, bool)
}

// staticProtocolSelector will not provide a different protocol for Fallback
type staticProtocolSelector struct {
	current Protocol
}

func (s *staticProtocolSelector) Current() Protocol {
	return s.current
}

func (s *staticProtocolSelector) Fallback() (Protocol, bool) {
	return s.current, false
}

// remoteProtocolSelector will fetch a list of remote protocols to provide for edge discovery
type remoteProtocolSelector struct {
	lock sync.RWMutex

	current Protocol

	// protocolPool is desired protocols in the order of priority they should be picked in.
	protocolPool []Protocol

	switchThreshold int32
	fetchFunc       edgediscovery.PercentageFetcher
	refreshAfter    time.Time
	ttl             time.Duration
	log             *zerolog.Logger
}

func newRemoteProtocolSelector(
	current Protocol,
	protocolPool []Protocol,
	switchThreshold int32,
	fetchFunc edgediscovery.PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
) *remoteProtocolSelector {
	return &remoteProtocolSelector{
		current:         current,
		protocolPool:    protocolPool,
		switchThreshold: switchThreshold,
		fetchFunc:       fetchFunc,
		refreshAfter:    time.Now().Add(ttl),
		ttl:             ttl,
		log:             log,
	}
}

func (s *remoteProtocolSelector) Current() Protocol {
	s.lock.Lock()
	defer s.lock.Unlock()
	if time.Now().Before(s.refreshAfter) {
		return s.current
	}

	protocol, err := getProtocol(s.protocolPool, s.fetchFunc, s.switchThreshold)
	if err != nil {
		s.log.Err(err).Msg("Failed to refresh protocol")
		return s.current
	}
	s.current = protocol

	s.refreshAfter = time.Now().Add(s.ttl)
	return s.current
}

func (s *remoteProtocolSelector) Fallback() (Protocol, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.current.fallback()
}

func getProtocol(protocolPool []Protocol, fetchFunc edgediscovery.PercentageFetcher, switchThreshold int32) (Protocol, error) {
	protocolPercentages, err := fetchFunc()
	if err != nil {
		return 0, err
	}
	for _, protocol := range protocolPool {
		protocolPercentage := protocolPercentages.GetPercentage(protocol.String())
		if protocolPercentage > switchThreshold {
			return protocol, nil
		}
	}

	// Default to first index in protocolPool list
	return protocolPool[0], nil
}

// defaultProtocolSelector will allow for a protocol to have a fallback
type defaultProtocolSelector struct {
	lock    sync.RWMutex
	current Protocol
}

func newDefaultProtocolSelector(
	current Protocol,
) *defaultProtocolSelector {
	return &defaultProtocolSelector{
		current: current,
	}
}

func (s *defaultProtocolSelector) Current() Protocol {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.current
}

func (s *defaultProtocolSelector) Fallback() (Protocol, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.current.fallback()
}

func NewProtocolSelector(
	protocolFlag string,
	accountTag string,
	tunnelTokenProvided bool,
	needPQ bool,
	protocolFetcher edgediscovery.PercentageFetcher,
	resolveTTL time.Duration,
	log *zerolog.Logger,
) (ProtocolSelector, error) {
	// With --post-quantum, we force quic
	if needPQ {
		return &staticProtocolSelector{
			current: QUIC,
		}, nil
	}

	threshold := switchThreshold(accountTag)
	fetchedProtocol, err := getProtocol(ProtocolList, protocolFetcher, threshold)
	log.Debug().Msgf("Fetched protocol: %s", fetchedProtocol)
	if err != nil {
		log.Warn().Msg("Unable to lookup protocol percentage.")
		// Falling through here since 'auto' is handled in the switch and failing
		// to do the protocol lookup isn't a failure since it can be triggered again
		// after the TTL.
	}

	// If the user picks a protocol, then we stick to it no matter what.
	switch protocolFlag {
	case "h2mux":
		// Any users still requesting h2mux will be upgraded to http2 instead
		log.Warn().Msg("h2mux is no longer a supported protocol: upgrading edge connection to http2. Please remove '--protocol h2mux' from runtime arguments to remove this warning.")
		return &staticProtocolSelector{current: HTTP2}, nil
	case QUIC.String():
		return &staticProtocolSelector{current: QUIC}, nil
	case HTTP2.String():
		return &staticProtocolSelector{current: HTTP2}, nil
	case AutoSelectFlag:
		// When a --token is provided, we want to start with QUIC but have fallback to HTTP2
		if tunnelTokenProvided {
			return newDefaultProtocolSelector(QUIC), nil
		}
		return newRemoteProtocolSelector(fetchedProtocol, ProtocolList, threshold, protocolFetcher, resolveTTL, log), nil
	}

	return nil, fmt.Errorf("unknown protocol %s, %s", protocolFlag, AvailableProtocolFlagMessage)
}

func switchThreshold(accountTag string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountTag))
	return int32(h.Sum32() % 100) // nolint: gosec
}
