package connection

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
)

const (
	// edgeH2muxTLSServerName is the server name to establish h2mux connection with edge
	edgeH2muxTLSServerName = "cftunnel.com"
	// edgeH2TLSServerName is the server name to establish http2 connection with edge
	edgeH2TLSServerName    = "h2.cftunnel.com"
	lbProbeUserAgentPrefix = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
)

type Config struct {
	OriginClient    OriginClient
	GracePeriod     time.Duration
	ReplaceExisting bool
}

type NamedTunnelConfig struct {
	Auth     pogs.TunnelAuth
	ID       uuid.UUID
	Client   pogs.ClientInfo
	Protocol Protocol
}

type ClassicTunnelConfig struct {
	Hostname   string
	OriginCert []byte
	// feature-flag to use new edge reconnect tokens
	UseReconnectToken bool
}

func (c *ClassicTunnelConfig) IsTrialZone() bool {
	return c.Hostname == ""
}

type Protocol int64

const (
	H2mux Protocol = iota
	HTTP2
)

func ParseProtocol(s string) (Protocol, bool) {
	switch s {
	case "h2mux":
		return H2mux, true
	case "http2":
		return HTTP2, true
	default:
		return 0, false
	}
}

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

type OriginClient interface {
	Proxy(w ResponseWriter, req *http.Request, isWebsocket bool) error
}

type ResponseWriter interface {
	WriteRespHeaders(*http.Response) error
	WriteErrorResponse(error)
	io.ReadWriter
}

type ConnectedFuse interface {
	Connected()
	IsConnected() bool
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}
