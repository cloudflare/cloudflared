package connection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	lbProbeUserAgentPrefix = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
	LogFieldConnIndex      = "connIndex"
	MaxGracePeriod         = time.Minute * 3
)

var switchingProtocolText = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))

type Config struct {
	OriginProxy     OriginProxy
	GracePeriod     time.Duration
	ReplaceExisting bool
}

type NamedTunnelConfig struct {
	Credentials    Credentials
	Client         pogs.ClientInfo
	QuickTunnelUrl string
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

// Type indicates the connection type of the  connection.
type Type int

const (
	TypeWebsocket Type = iota
	TypeTCP
	TypeControlStream
	TypeHTTP
)

// ShouldFlush returns whether this kind of connection should actively flush data
func (t Type) shouldFlush() bool {
	switch t {
	case TypeWebsocket, TypeTCP, TypeControlStream:
		return true
	default:
		return false
	}
}

func (t Type) String() string {
	switch t {
	case TypeWebsocket:
		return "websocket"
	case TypeTCP:
		return "tcp"
	case TypeControlStream:
		return "control stream"
	case TypeHTTP:
		return "http"
	default:
		return fmt.Sprintf("Unknown Type %d", t)
	}
}

// OriginProxy is how data flows from cloudflared to the origin services running behind it.
type OriginProxy interface {
	ProxyHTTP(w ResponseWriter, req *http.Request, isWebsocket bool) error
	ProxyTCP(ctx context.Context, rwa ReadWriteAcker, req *TCPRequest) error
}

// TCPRequest defines the input format needed to perform a TCP proxy.
type TCPRequest struct {
	Dest    string
	CFRay   string
	LBProbe bool
}

// ReadWriteAcker is a readwriter with the ability to Acknowledge to the downstream (edge) that the origin has
// accepted the connection.
type ReadWriteAcker interface {
	io.ReadWriter
	AckConnection() error
}

// HTTPResponseReadWriteAcker is an HTTP implementation of ReadWriteAcker.
type HTTPResponseReadWriteAcker struct {
	r   io.Reader
	w   ResponseWriter
	req *http.Request
}

// NewHTTPResponseReadWriterAcker returns a new instance of HTTPResponseReadWriteAcker.
func NewHTTPResponseReadWriterAcker(w ResponseWriter, req *http.Request) *HTTPResponseReadWriteAcker {
	return &HTTPResponseReadWriteAcker{
		r:   req.Body,
		w:   w,
		req: req,
	}
}

func (h *HTTPResponseReadWriteAcker) Read(p []byte) (int, error) {
	return h.r.Read(p)
}

func (h *HTTPResponseReadWriteAcker) Write(p []byte) (int, error) {
	return h.w.Write(p)
}

// AckConnection acks an HTTP connection by sending a switch protocols status code that enables the caller to
// upgrade to streams.
func (h *HTTPResponseReadWriteAcker) AckConnection() error {
	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		ContentLength: -1,
	}

	if secWebsocketKey := h.req.Header.Get("Sec-WebSocket-Key"); secWebsocketKey != "" {
		resp.Header = websocket.NewResponseHeader(h.req)
	}

	return h.w.WriteRespHeaders(resp.StatusCode, resp.Header)
}

type ResponseWriter interface {
	WriteRespHeaders(status int, header http.Header) error
	io.Writer
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

func FindCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func IsLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}
