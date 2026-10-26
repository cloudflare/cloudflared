package connection

import (
	"fmt"

	"github.com/rs/zerolog"
)

const (
	AvailableProtocolFlagMessage = "Available protocols: 'auto' - starts with QUIC and falls back to HTTP/2 (the default and recommended option); 'quic' - based on QUIC, relying on UDP egress to Cloudflare edge; 'http2' - using Go's HTTP2 library, relying on TCP egress to Cloudflare edge"
	// edgeH2muxTLSServerName is the server name to establish h2mux connection with edge (unused, but kept for legacy reference).
	_ = "cftunnel.com"
	// edgeH2TLSServerName is the server name to establish http2 connection with edge
	edgeH2TLSServerName = "h2.cftunnel.com"
	// edgeQUICServerName is the server name to establish quic connection with edge.
	edgeQUICServerName = "quic.cftunnel.com"
	// probeTLSServerName is the server name used for pre-flight connectivity checks.
	probeTLSServerName = "probe.cftunnel.com"
	quicProtos         = "argotunnel"
	AutoSelectFlag     = "auto"
)

// ProtocolList represents the supported protocols for communication with the edge.
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
			NextProtos: []string{quicProtos},
		}
	default:
		return nil
	}
}

// ProbeTLSSettings returns TLS settings for pre-flight connectivity checks.
func (p Protocol) ProbeTLSSettings() *TLSSettings {
	switch p {
	case HTTP2:
		return &TLSSettings{
			ServerName: probeTLSServerName,
		}
	case QUIC:
		return &TLSSettings{
			ServerName: probeTLSServerName,
			NextProtos: []string{quicProtos},
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

type protocolSelector struct {
	current       Protocol
	allowFallback bool
}

func (s *protocolSelector) Current() Protocol {
	return s.current
}

func (s *protocolSelector) Fallback() (Protocol, bool) {
	if !s.allowFallback {
		return s.current, false
	}
	return s.current.fallback()
}

// NewProtocolSelector selects the configured edge transport protocol.
func NewProtocolSelector(
	protocolFlag string,
	log *zerolog.Logger,
) (ProtocolSelector, error) {
	// If the user picks a protocol, then we stick to it no matter what.
	switch protocolFlag {
	case "h2mux":
		// Any users still requesting h2mux will be upgraded to http2 instead
		log.Warn().Msg("h2mux is no longer a supported protocol: upgrading edge connection to http2. Please remove '--protocol h2mux' from runtime arguments to remove this warning.")
		return &protocolSelector{current: HTTP2}, nil
	case QUIC.String():
		return &protocolSelector{current: QUIC}, nil
	case HTTP2.String():
		return &protocolSelector{current: HTTP2}, nil
	case AutoSelectFlag:
		return &protocolSelector{current: QUIC, allowFallback: true}, nil
	}

	return nil, fmt.Errorf("unknown protocol %s, %s", protocolFlag, AvailableProtocolFlagMessage)
}
