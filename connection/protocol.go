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
	AvailableProtocolFlagMessage = "Available protocols: http2 - Go's implementation, h2mux - Cloudflare's implementation of HTTP/2, and auto - automatically select between http2 and h2mux"
	// edgeH2muxTLSServerName is the server name to establish h2mux connection with edge
	edgeH2muxTLSServerName = "cftunnel.com"
	// edgeH2TLSServerName is the server name to establish http2 connection with edge
	edgeH2TLSServerName = "h2.cftunnel.com"
	// edgeQUICServerName is the server name to establish quic connection with edge.
	edgeQUICServerName = "quic.cftunnel.com"
	// threshold to switch back to h2mux when the user intentionally pick --protocol http2
	explicitHTTP2FallbackThreshold = -1
	autoSelectFlag                 = "auto"
)

var (
	// ProtocolList represents a list of supported protocols for communication with the edge.
	ProtocolList = []Protocol{H2mux, HTTP2, HTTP2Warp, QUIC, QUICWarp}
)

type Protocol int64

const (
	// H2mux protocol can be used both with Classic and Named Tunnels. .
	H2mux Protocol = iota
	// HTTP2 is used only with named tunnels. It's more efficient than H2mux for L4 proxying.
	HTTP2
	// QUIC is used only with named tunnels.
	QUIC
	// HTTP2Warp is used only with named tunnels. It's useful for warp-routing where we don't want to fallback to
	// H2mux on HTTP2 failure to connect.
	HTTP2Warp
	//QUICWarp is used only with named tunnels. It's useful for warp-routing where we want to fallback to HTTP2 but
	// dont' want HTTP2 to fallback to H2mux
	QUICWarp
)

// Fallback returns the fallback protocol and whether the protocol has a fallback
func (p Protocol) fallback() (Protocol, bool) {
	switch p {
	case H2mux:
		return 0, false
	case HTTP2:
		return H2mux, true
	case HTTP2Warp:
		return 0, false
	case QUIC:
		return HTTP2, true
	case QUICWarp:
		return HTTP2Warp, true
	default:
		return 0, false
	}
}

func (p Protocol) String() string {
	switch p {
	case H2mux:
		return "h2mux"
	case HTTP2, HTTP2Warp:
		return "http2"
	case QUIC, QUICWarp:
		return "quic"
	default:
		return fmt.Sprintf("unknown protocol")
	}
}

func (p Protocol) TLSSettings() *TLSSettings {
	switch p {
	case H2mux:
		return &TLSSettings{
			ServerName: edgeH2muxTLSServerName,
		}
	case HTTP2, HTTP2Warp:
		return &TLSSettings{
			ServerName: edgeH2TLSServerName,
		}
	case QUIC, QUICWarp:
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
	lock sync.RWMutex

	current Protocol

	// protocolPool is desired protocols in the order of priority they should be picked in.
	protocolPool []Protocol

	switchThreshold int32
	fetchFunc       PercentageFetcher
	refreshAfter    time.Time
	ttl             time.Duration
	log             *zerolog.Logger
}

func newAutoProtocolSelector(
	current Protocol,
	protocolPool []Protocol,
	switchThreshold int32,
	fetchFunc PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
) *autoProtocolSelector {
	return &autoProtocolSelector{
		current:         current,
		protocolPool:    protocolPool,
		switchThreshold: switchThreshold,
		fetchFunc:       fetchFunc,
		refreshAfter:    time.Now().Add(ttl),
		ttl:             ttl,
		log:             log,
	}
}

func (s *autoProtocolSelector) Current() Protocol {
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

func getProtocol(protocolPool []Protocol, fetchFunc PercentageFetcher, switchThreshold int32) (Protocol, error) {
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

	return protocolPool[len(protocolPool)-1], nil
}

func (s *autoProtocolSelector) Fallback() (Protocol, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.current.fallback()
}

type PercentageFetcher func() (edgediscovery.ProtocolPercents, error)

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

	threshold := switchThreshold(namedTunnel.Credentials.AccountTag)
	fetchedProtocol, err := getProtocol([]Protocol{QUIC, HTTP2}, fetchFunc, threshold)
	if err != nil {
		log.Err(err).Msg("Unable to lookup protocol. Defaulting to `http2`. If this fails, you can set `--protocol h2mux` in your cloudflared command.")
		return &staticProtocolSelector{
			current: HTTP2,
		}, nil
	}
	if warpRoutingEnabled {
		if protocolFlag == H2mux.String() || fetchedProtocol == H2mux {
			log.Warn().Msg("Warp routing is not supported in h2mux protocol. Upgrading to http2 to allow it.")
			protocolFlag = HTTP2.String()
			fetchedProtocol = HTTP2Warp
		}
		return selectWarpRoutingProtocols(protocolFlag, fetchFunc, ttl, log, threshold, fetchedProtocol)
	}

	return selectNamedTunnelProtocols(protocolFlag, fetchFunc, ttl, log, threshold, fetchedProtocol)
}

func selectNamedTunnelProtocols(
	protocolFlag string,
	fetchFunc PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
	threshold int32,
	protocol Protocol,
) (ProtocolSelector, error) {
	if protocolFlag == H2mux.String() {
		return &staticProtocolSelector{
			current: H2mux,
		}, nil
	}

	if protocolFlag == QUIC.String() {
		return newAutoProtocolSelector(QUIC, []Protocol{QUIC, HTTP2, H2mux}, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
	}

	if protocolFlag == HTTP2.String() {
		return newAutoProtocolSelector(HTTP2, []Protocol{HTTP2, H2mux}, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
	}

	if protocolFlag != autoSelectFlag {
		return nil, fmt.Errorf("Unknown protocol %s, %s", protocolFlag, AvailableProtocolFlagMessage)
	}

	return newAutoProtocolSelector(protocol, []Protocol{QUIC, HTTP2, H2mux}, threshold, fetchFunc, ttl, log), nil
}

func selectWarpRoutingProtocols(
	protocolFlag string,
	fetchFunc PercentageFetcher,
	ttl time.Duration,
	log *zerolog.Logger,
	threshold int32,
	protocol Protocol,
) (ProtocolSelector, error) {
	if protocolFlag == QUIC.String() {
		return newAutoProtocolSelector(QUICWarp, []Protocol{QUICWarp, HTTP2Warp}, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
	}

	if protocolFlag == HTTP2.String() {
		return newAutoProtocolSelector(HTTP2Warp, []Protocol{HTTP2Warp}, explicitHTTP2FallbackThreshold, fetchFunc, ttl, log), nil
	}

	if protocolFlag != autoSelectFlag {
		return nil, fmt.Errorf("Unknown protocol %s, %s", protocolFlag, AvailableProtocolFlagMessage)
	}

	return newAutoProtocolSelector(protocol, []Protocol{QUICWarp, HTTP2Warp}, threshold, fetchFunc, ttl, log), nil
}

func switchThreshold(accountTag string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountTag))
	return int32(h.Sum32() % 100)
}
