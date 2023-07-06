package connection

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net"
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

	contentTypeHeader = "content-type"
	sseContentType    = "text/event-stream"
	grpcContentType   = "application/grpc"
)

var (
	switchingProtocolText = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))
	flushableContentTypes = []string{sseContentType, grpcContentType}
)

type Orchestrator interface {
	UpdateConfig(version int32, config []byte) *pogs.UpdateConfigurationResponse
	GetConfigJSON() ([]byte, error)
	GetOriginProxy() (OriginProxy, error)
	WarpRoutingEnabled() (enabled bool)
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
	ProxyHTTP(w ResponseWriter, tr *tracing.TracedHTTPRequest, isWebsocket bool) error
	ProxyTCP(ctx context.Context, rwa ReadWriteAcker, req *TCPRequest) error
}

// TCPRequest defines the input format needed to perform a TCP proxy.
type TCPRequest struct {
	Dest      string
	CFRay     string
	LBProbe   bool
	FlowID    string
	CfTraceID string
	ConnIndex uint8
}

// ReadWriteAcker is a readwriter with the ability to Acknowledge to the downstream (edge) that the origin has
// accepted the connection.
type ReadWriteAcker interface {
	io.ReadWriter
	AckConnection(tracePropagation string) error
}

// HTTPResponseReadWriteAcker is an HTTP implementation of ReadWriteAcker.
type HTTPResponseReadWriteAcker struct {
	r   io.Reader
	w   ResponseWriter
	f   http.Flusher
	req *http.Request
}

// NewHTTPResponseReadWriterAcker returns a new instance of HTTPResponseReadWriteAcker.
func NewHTTPResponseReadWriterAcker(w ResponseWriter, flusher http.Flusher, req *http.Request) *HTTPResponseReadWriteAcker {
	return &HTTPResponseReadWriteAcker{
		r:   req.Body,
		w:   w,
		f:   flusher,
		req: req,
	}
}

func (h *HTTPResponseReadWriteAcker) Read(p []byte) (int, error) {
	return h.r.Read(p)
}

func (h *HTTPResponseReadWriteAcker) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	if n > 0 {
		h.f.Flush()
	}
	return n, err
}

// AckConnection acks an HTTP connection by sending a switch protocols status code that enables the caller to
// upgrade to streams.
func (h *HTTPResponseReadWriteAcker) AckConnection(tracePropagation string) error {
	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		ContentLength: -1,
		Header:        http.Header{},
	}

	if secWebsocketKey := h.req.Header.Get("Sec-WebSocket-Key"); secWebsocketKey != "" {
		resp.Header = websocket.NewResponseHeader(h.req)
	}

	if tracePropagation != "" {
		resp.Header.Add(tracing.CanonicalCloudflaredTracingHeader, tracePropagation)
	}

	return h.w.WriteRespHeaders(resp.StatusCode, resp.Header)
}

// localProxyConnection emulates an incoming connection to cloudflared as a net.Conn.
// Used when handling a "hijacked" connection from connection.ResponseWriter
type localProxyConnection struct {
	io.ReadWriteCloser
}

func (c *localProxyConnection) Read(b []byte) (int, error) {
	return c.ReadWriteCloser.Read(b)
}

func (c *localProxyConnection) Write(b []byte) (int, error) {
	return c.ReadWriteCloser.Write(b)
}

func (c *localProxyConnection) Close() error {
	return c.ReadWriteCloser.Close()
}

func (c *localProxyConnection) LocalAddr() net.Addr {
	// Unused LocalAddr
	return &net.TCPAddr{IP: net.IPv6loopback, Port: 0, Zone: ""}
}

func (c *localProxyConnection) RemoteAddr() net.Addr {
	// Unused RemoteAddr
	return &net.TCPAddr{IP: net.IPv6loopback, Port: 0, Zone: ""}
}

func (c *localProxyConnection) SetDeadline(t time.Time) error {
	// ignored since we can't set the read/write Deadlines for the tunnel back to origintunneld
	return nil
}

func (c *localProxyConnection) SetReadDeadline(t time.Time) error {
	// ignored since we can't set the read/write Deadlines for the tunnel back to origintunneld
	return nil
}

func (c *localProxyConnection) SetWriteDeadline(t time.Time) error {
	// ignored since we can't set the read/write Deadlines for the tunnel back to origintunneld
	return nil
}

// ResponseWriter is the response path for a request back through cloudflared's tunnel.
type ResponseWriter interface {
	WriteRespHeaders(status int, header http.Header) error
	AddTrailer(trailerName, trailerValue string)
	http.ResponseWriter
	http.Hijacker
	io.Writer
}

type ConnectedFuse interface {
	Connected()
	IsConnected() bool
}

// Helper method to let the caller know what content-types should require a flush on every
// write to a ResponseWriter.
func shouldFlush(headers http.Header) bool {
	if contentType := headers.Get(contentTypeHeader); contentType != "" {
		contentType = strings.ToLower(contentType)
		for _, c := range flushableContentTypes {
			if strings.HasPrefix(contentType, c) {
				return true
			}
		}
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
