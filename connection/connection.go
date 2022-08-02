package connection

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	lbProbeUserAgentPrefix = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
	LogFieldConnIndex      = "connIndex"
	MaxGracePeriod         = time.Minute * 3
	MaxConcurrentStreams   = math.MaxUint32
)

var switchingProtocolText = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))

type Orchestrator interface {
	UpdateConfig(version int32, config []byte) *pogs.UpdateConfigurationResponse
	GetConfigJSON() ([]byte, error)
	GetOriginProxy() (OriginProxy, error)
}

type NamedTunnelProperties struct {
	Credentials    Credentials
	Client         pogs.ClientInfo
	QuickTunnelUrl string
}

// Credentials are stored in the credentials file and contain all info needed to run a tunnel.
type Credentials struct {
	AccountTag   string
	TunnelSecret []byte
	TunnelID     uuid.UUID
}

func (c *Credentials) Auth() pogs.TunnelAuth {
	return pogs.TunnelAuth{
		AccountTag:   c.AccountTag,
		TunnelSecret: c.TunnelSecret,
	}
}

// TunnelToken are Credentials but encoded with custom fields namings.
type TunnelToken struct {
	AccountTag   string    `json:"a"`
	TunnelSecret []byte    `json:"s"`
	TunnelID     uuid.UUID `json:"t"`
}

func (t TunnelToken) Credentials() Credentials {
	return Credentials{
		AccountTag:   t.AccountTag,
		TunnelSecret: t.TunnelSecret,
		TunnelID:     t.TunnelID,
	}
}

func (t TunnelToken) Encode() (string, error) {
	val, err := json.Marshal(t)
	if err != nil {
		return "", errors.Wrap(err, "could not JSON encode token")
	}

	return base64.StdEncoding.EncodeToString(val), nil
}

type ClassicTunnelProperties struct {
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
	TypeConfiguration
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
	ProxyHTTP(w ResponseWriter, tr *tracing.TracedRequest, isWebsocket bool) error
	ProxyTCP(ctx context.Context, rwa ReadWriteAcker, req *TCPRequest) error
}

// TCPRequest defines the input format needed to perform a TCP proxy.
type TCPRequest struct {
	Dest    string
	CFRay   string
	LBProbe bool
	FlowID  string
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
