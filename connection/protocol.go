package connection

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	AvailableProtocolFlagMessage = "Available protocols: http2 - Go's implementation, h2mux - Cloudflare's implementation of HTTP/2, and auto - automatically select between http2 and h2mux"
	// edgeH2muxTLSServerName is the server name to establish h2mux connection with edge
	edgeH2muxTLSServerName = "cftunnel.com"
	// edgeH2TLSServerName is the server name to establish http2 connection with edge
	edgeH2TLSServerName = "h2.cftunnel.com"
	// threshold to switch back to h2mux when the user intentionally pick --protocol http2
	explicitHTTP2FallbackThreshold = -1
	autoSelectFlag                 = "auto"
)

var (
	ProtocolList = []Protocol{H2mux, HTTP2}
)

type Protocol int64

const (
	H2mux Protocol = iota
	HTTP2
)

func (p Protocol) ServerName() string {
	switch p {
	case H2mux:
		return edgeH2muxTLSServerName
	case HTTP2:
		return edgeH2TLSServerName
	default:
		return ""
	}
}

// Fallback returns the fallback protocol and whether the protocol has a fallback
func (p Protocol) fallback() (Protocol, bool) {
	switch p {
	case H2mux:
		return 0, false
	case HTTP2:
		return H2mux, true
	default:
		return 0, false
	}
}

func (p Protocol) String() string {
	switch p {
	case H2mux:
		return "h2mux"
	case HTTP2:
		return "http2"
	default:
		return fmt.Sprintf("unknown protocol")
	}
}

type ProtocolSelector interface {
	Current() Protocol
	Fallback() (Protocol, bool)
}

type staticProtocolSelector struct {
	current Protocol
}

func (s *staticProtocolSelector) Current() Protocol {
	return s.current
}

func (s *staticProtocolSelector) Fallback() (Protocol, bool) {
	return 0, false
}

type autoProtocolSelector struct {
	lock           sync.RWMutex
	current        Protocol
	switchThrehold int32
	fetchFunc      PercentageFetcher
	refreshAfter   time.Time
	ttl            time.Duration
	log            *zerolog.Logger
}

func newAutoProtocolSelector(
	current Protocol,
	switchThrehold int32,
	fetchFunc PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
) *autoProtocolSelector {
	return &autoProtocolSelector{
		current:        current,
		switchThrehold: switchThrehold,
		fetchFunc:      fetchFunc,
		refreshAfter:   time.Now().Add(ttl),
		ttl:            ttl,
		log:            log,
	}
}

func (s *autoProtocolSelector) Current() Protocol {
	s.lock.Lock()
	defer s.lock.Unlock()
	if time.Now().Before(s.refreshAfter) {
		return s.current
	}

	percentage, err := s.fetchFunc()
	if err != nil {
		s.log.Err(err).Msg("Failed to refresh protocol")
		return s.current
	}

	if s.switchThrehold < percentage {
		s.current = HTTP2
	} else {
		s.current = H2mux
	}
	s.refreshAfter = time.Now().Add(s.ttl)
	return s.current
}

func (s *autoProtocolSelector) Fallback() (Protocol, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.current.fallback()
}

type PercentageFetcher func() (int32, error)

func NewProtocolSelector(
	protocolFlag string,
	warpRoutingEnabled bool,
	namedTunnel *NamedTunnelConfig,
	fetchFunc PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
) (ProtocolSelector, error) {
	// Classic tunnel is only supported with h2mux
	if namedTunnel == nil {
		return &staticProtocolSelector{
			current: H2mux,
		}, nil
	}

	// warp routing can only be served over http2 connections
	if warpRoutingEnabled {
		if protocolFlag == H2mux.String() {
			log.Warn().Msg("Warp routing is only supported by http2 protocol. Upgrading protocol to http2")
		}
		return &staticProtocolSelector{
			current: HTTP2,
		}, nil
	}

	if protocolFlag == H2mux.String() {
		return &staticProtocolSelector{
			current: H2mux,
		}, nil
	}

	http2Percentage, err := fetchFunc()
	if err != nil {
		return nil, err
	}
	if protocolFlag == HTTP2.String() {
		if http2Percentage < 0 {
			return newAutoProtocolSelector(H2mux, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
		}
		return newAutoProtocolSelector(HTTP2, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
	}

	if protocolFlag != autoSelectFlag {
		return nil, fmt.Errorf("Unknown protocol %s, %s", protocolFlag, AvailableProtocolFlagMessage)
	}
	threshold := switchThreshold(namedTunnel.Credentials.AccountTag)
	if threshold < http2Percentage {
		return newAutoProtocolSelector(HTTP2, threshold, fetchFunc, ttl, log), nil
	}
	return newAutoProtocolSelector(H2mux, threshold, fetchFunc, ttl, log), nil
}

func switchThreshold(accountTag string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountTag))
	return int32(h.Sum32() % 100)
}
