package connection

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
)

const LogFieldConnIndex = "connIndex"

type Config struct {
	OriginClient    OriginClient
	GracePeriod     time.Duration
	ReplaceExisting bool
}

type NamedTunnelConfig struct {
	Credentials Credentials
	Client      pogs.ClientInfo
}

// Credentials are stored in the credentials file and contain all info needed to run a tunnel.
type Credentials struct {
	AccountTag   string
	TunnelSecret []byte
	TunnelID     uuid.UUID
	TunnelName   string
}

func (c *Credentials) Auth() pogs.TunnelAuth {
	return pogs.TunnelAuth{
		AccountTag:   c.AccountTag,
		TunnelSecret: c.TunnelSecret,
	}
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

type OriginClient interface {
	Proxy(w ResponseWriter, req *http.Request, isWebsocket bool) error
}

type ResponseWriter interface {
	WriteRespHeaders(*http.Response) error
	WriteErrorResponse()
	io.ReadWriter
}

type ConnectedFuse interface {
	Connected()
	IsConnected() bool
}

func IsServerSentEvent(headers http.Header) bool {
	if contentType := headers.Get("content-type"); contentType != "" {
		return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
	}
	return false
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}
