package connection

import (
	"bufio"
	"context"
	gojson "encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"

	"github.com/cloudflare/cloudflared/client"
	cfdflow "github.com/cloudflare/cloudflared/flow"

	"github.com/cloudflare/cloudflared/tracing"
)

// note: these constants are exported so we can reuse them in the edge-side code
const (
	InternalUpgradeHeader     = "Cf-Cloudflared-Proxy-Connection-Upgrade"
	InternalTCPProxySrcHeader = "Cf-Cloudflared-Proxy-Src"
	WebsocketUpgrade          = "websocket"
	ControlStreamUpgrade      = "control-stream"
	ConfigurationUpdate       = "update-configuration"
)

var errEdgeConnectionClosed = fmt.Errorf("connection with edge closed")

// HTTP2Connection represents a net.Conn that uses HTTP2 frames to proxy traffic from the edge to cloudflared on the
// origin.
type HTTP2Connection struct {
	conn         net.Conn
	server       *http2.Server
	orchestrator Orchestrator
	connOptions  *client.ConnectionOptionsSnapshot
	observer     *Observer
	connIndex    uint8

	log                  *zerolog.Logger
	activeRequestsWG     sync.WaitGroup
	controlStreamHandler ControlStreamHandler
	stoppedGracefully    bool
	controlStreamErr     error // result of running control stream handler
}

// NewHTTP2Connection returns a new instance of HTTP2Connection.
func NewHTTP2Connection(
	conn net.Conn,
	orchestrator Orchestrator,
	connOptions *client.ConnectionOptionsSnapshot,
	observer *Observer,
	connIndex uint8,
	controlStreamHandler ControlStreamHandler,
	log *zerolog.Logger,
) *HTTP2Connection {
	return &HTTP2Connection{
		conn: conn,
		server: &http2.Server{
			MaxConcurrentStreams: MaxConcurrentStreams,
		},
		orchestrator:         orchestrator,
		connOptions:          connOptions,
		observer:             observer,
		connIndex:            connIndex,
		controlStreamHandler: controlStreamHandler,
		log:                  log,
	}
}

// Serve serves an HTTP2 server that the edge can talk to.
func (c *HTTP2Connection) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		c.close()
	}()
	c.server.ServeConn(c.conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: c,
	})

	switch {
	case c.controlStreamHandler.IsStopped():
		return nil
	case c.controlStreamErr != nil:
		return c.controlStreamErr
	default:
		c.observer.log.Info().Uint8(LogFieldConnIndex, c.connIndex).Msg("Lost connection with the edge")
		return errEdgeConnectionClosed
	}
}

func (c *HTTP2Connection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.activeRequestsWG.Add(1)
	defer c.activeRequestsWG.Done()

	connType := determineHTTP2Type(r)
	handleMissingRequestParts(connType, r)

	respWriter, err := NewHTTP2RespWriter(r, w, connType, c.log)
	if err != nil {
		c.observer.log.Error().Msg(err.Error())
		return
	}

	originProxy, err := c.orchestrator.GetOriginProxy()
	if err != nil {
		c.observer.log.Error().Msg(err.Error())
		return
	}

	var requestErr error
	switch connType {
	case TypeControlStream:
		requestErr = c.controlStreamHandler.ServeControlStream(r.Context(), respWriter, c.connOptions.ConnectionOptions(), c.orchestrator)
		if requestErr != nil {
			c.controlStreamErr = requestErr
		}

	case TypeConfiguration:
		requestErr = c.handleConfigurationUpdate(respWriter, r)

	case TypeWebsocket, TypeHTTP:
		stripWebsocketUpgradeHeader(r)
		// Check for tracing on request
		tr := tracing.NewTracedHTTPRequest(r, c.connIndex, c.log)
		if err := originProxy.ProxyHTTP(respWriter, tr, connType == TypeWebsocket); err != nil {
			requestErr = fmt.Errorf("Failed to proxy HTTP: %w", err)
		}

	case TypeTCP:
		host, err := getRequestHost(r)
		if err != nil {
			requestErr = fmt.Errorf(`cloudflared received a warp-routing request with an empty host value: %w`, err)
			break
		}

		rws := NewHTTPResponseReadWriterAcker(respWriter, respWriter, r)
		requestErr = originProxy.ProxyTCP(r.Context(), rws, &TCPRequest{
			Dest:      host,
			CFRay:     FindCfRayHeader(r),
			LBProbe:   IsLBProbeRequest(r),
			CfTraceID: r.Header.Get(tracing.TracerContextName),
			ConnIndex: c.connIndex,
		})

	default:
		requestErr = fmt.Errorf("Received unknown connection type: %s", connType)
	}

	if requestErr != nil {
		c.log.Error().Err(requestErr).Msg("failed to serve incoming request")

		// WriteErrorResponse will return false if status was already written. we need to abort handler.
		if !respWriter.WriteErrorResponse(requestErr) {
			c.log.Debug().Msg("Handler aborted due to failure to write error response after status already sent")
			panic(http.ErrAbortHandler)
		}
	}
}

// ConfigurationUpdateBody is the representation followed by the edge to send updates to cloudflared.
type ConfigurationUpdateBody struct {
	Version int32             `json:"version"`
	Config  gojson.RawMessage `json:"config"`
}

func (c *HTTP2Connection) handleConfigurationUpdate(respWriter *http2RespWriter, r *http.Request) error {
	var configBody ConfigurationUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&configBody); err != nil {
		return err
	}
	resp := c.orchestrator.UpdateConfig(configBody.Version, configBody.Config)
	bdy, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = respWriter.Write(bdy)
	return err
}

func (c *HTTP2Connection) close() {
	// Wait for all serve HTTP handlers to return
	c.activeRequestsWG.Wait()
	c.conn.Close()
}

type http2RespWriter struct {
	r             io.Reader
	w             http.ResponseWriter
	flusher       http.Flusher
	shouldFlush   bool
	statusWritten bool
	respHeaders   http.Header
	hijackedMutex sync.Mutex
	hijackedv     bool
	log           *zerolog.Logger
}

func NewHTTP2RespWriter(r *http.Request, w http.ResponseWriter, connType Type, log *zerolog.Logger) (*http2RespWriter, error) {
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		respWriter := &http2RespWriter{
			r:   r.Body,
			w:   w,
			log: log,
		}
		err := fmt.Errorf("%T doesn't implement http.Flusher", w)
		respWriter.WriteErrorResponse(err)
		return nil, err
	}

	return &http2RespWriter{
		r:           r.Body,
		w:           w,
		flusher:     flusher,
		shouldFlush: connType.shouldFlush(),
		respHeaders: make(http.Header),
		log:         log,
	}, nil
}

func (rp *http2RespWriter) AddTrailer(trailerName, trailerValue string) {
	if !rp.statusWritten {
		rp.log.Warn().Msg("Tried to add Trailer to response before status written. Ignoring...")
		return
	}

	rp.w.Header().Add(http2.TrailerPrefix+trailerName, trailerValue)
}

func (rp *http2RespWriter) WriteRespHeaders(status int, header http.Header) error {
	if rp.hijacked() {
		rp.log.Warn().Msg("WriteRespHeaders after hijack")
		return nil
	}
	dest := rp.w.Header()
	userHeaders := make(http.Header, len(header))
	for name, values := range header {
		// lowercase headers for simplicity check
		h2name := strings.ToLower(name)

		if h2name == "content-length" {
			// This header has meaning in HTTP/2 and will be used by the edge,
			// so it should be sent *also* as an HTTP/2 response header.
			dest[name] = values
		}

		if h2name == tracing.IntCloudflaredTracingHeader {
			// Add cf-int-cloudflared-tracing header outside of serialized userHeaders
			dest[tracing.CanonicalCloudflaredTracingHeader] = values
			continue
		}

		if !IsControlResponseHeader(h2name) || IsWebsocketClientHeader(h2name) {
			// User headers, on the other hand, must all be serialized so that
			// HTTP/2 header validation won't be applied to HTTP/1 header values
			userHeaders[name] = values
		}
	}

	// Perform user header serialization and set them in the single header
	dest.Set(CanonicalResponseUserHeaders, SerializeHeaders(userHeaders))

	rp.setResponseMetaHeader(responseMetaHeaderOrigin)
	// HTTP2 removes support for 101 Switching Protocols https://tools.ietf.org/html/rfc7540#section-8.1.1
	if status == http.StatusSwitchingProtocols {
		status = http.StatusOK
	}
	rp.w.WriteHeader(status)
	if shouldFlush(header) {
		rp.shouldFlush = true
	}
	if rp.shouldFlush {
		rp.flusher.Flush()
	}

	rp.statusWritten = true
	return nil
}

func (rp *http2RespWriter) Header() http.Header {
	return rp.respHeaders
}

func (rp *http2RespWriter) Flush() {
	rp.flusher.Flush()
}

func (rp *http2RespWriter) WriteHeader(status int) {
	if rp.hijacked() {
		rp.log.Warn().Msg("WriteHeader after hijack")
		return
	}
	_ = rp.WriteRespHeaders(status, rp.respHeaders)
}

func (rp *http2RespWriter) hijacked() bool {
	rp.hijackedMutex.Lock()
	defer rp.hijackedMutex.Unlock()
	return rp.hijackedv
}

func (rp *http2RespWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !rp.statusWritten {
		return nil, nil, fmt.Errorf("status not yet written before attempting to hijack connection")
	}
	// Make sure to flush anything left in the buffer before hijacking
	if rp.shouldFlush {
		rp.flusher.Flush()
	}
	rp.hijackedMutex.Lock()
	defer rp.hijackedMutex.Unlock()
	if rp.hijackedv {
		return nil, nil, http.ErrHijacked
	}
	rp.hijackedv = true
	conn := &localProxyConnection{rp}
	// We return the http2RespWriter here because we want to make sure that we flush after every write
	// otherwise the HTTP2 write buffer waits a few seconds before sending.
	readWriter := bufio.NewReadWriter(
		bufio.NewReader(rp),
		bufio.NewWriter(rp),
	)
	return conn, readWriter, nil
}

func (rp *http2RespWriter) WriteErrorResponse(err error) bool {
	if rp.statusWritten {
		return false
	}

	if errors.Is(err, cfdflow.ErrTooManyActiveFlows) {
		rp.setResponseMetaHeader(responseMetaHeaderCfdFlowRateLimited)
	} else {
		rp.setResponseMetaHeader(responseMetaHeaderCfd)
	}
	rp.w.WriteHeader(http.StatusBadGateway)
	rp.statusWritten = true

	return true
}

func (rp *http2RespWriter) setResponseMetaHeader(value string) {
	rp.w.Header().Set(CanonicalResponseMetaHeader, value)
}

func (rp *http2RespWriter) Read(p []byte) (n int, err error) {
	return rp.r.Read(p)
}

func (rp *http2RespWriter) Write(p []byte) (n int, err error) {
	defer func() {
		// Implementer of OriginClient should make sure it doesn't write to the connection after Proxy returns
		// Register a recover routine just in case.
		if r := recover(); r != nil {
			rp.log.Debug().Msgf("Recover from http2 response writer panic, error %s", debug.Stack())
		}
	}()
	n, err = rp.w.Write(p)
	if err == nil && rp.shouldFlush {
		rp.flusher.Flush()
	}
	return n, err
}

func (rp *http2RespWriter) Close() error {
	return nil
}

func determineHTTP2Type(r *http.Request) Type {
	switch {
	case isConfigurationUpdate(r):
		return TypeConfiguration
	case isWebsocketUpgrade(r):
		return TypeWebsocket
	case IsTCPStream(r):
		return TypeTCP
	case isControlStreamUpgrade(r):
		return TypeControlStream
	default:
		return TypeHTTP
	}
}

func handleMissingRequestParts(connType Type, r *http.Request) {
	if connType == TypeHTTP {
		// http library has no guarantees that we receive a filled URL. If not, then we fill it, as we reuse the request
		// for proxying. For proxying they should not matter since we control the dialer on every egress proxied.
		if len(r.URL.Scheme) == 0 {
			r.URL.Scheme = "http"
		}
		if len(r.URL.Host) == 0 {
			r.URL.Host = "localhost:8080"
		}
	}
}

func isControlStreamUpgrade(r *http.Request) bool {
	return r.Header.Get(InternalUpgradeHeader) == ControlStreamUpgrade
}

func isWebsocketUpgrade(r *http.Request) bool {
	return r.Header.Get(InternalUpgradeHeader) == WebsocketUpgrade
}

func isConfigurationUpdate(r *http.Request) bool {
	return r.Header.Get(InternalUpgradeHeader) == ConfigurationUpdate
}

// IsTCPStream discerns if the connection request needs a tcp stream proxy.
func IsTCPStream(r *http.Request) bool {
	return r.Header.Get(InternalTCPProxySrcHeader) != ""
}

func stripWebsocketUpgradeHeader(r *http.Request) {
	r.Header.Del(InternalUpgradeHeader)
}

// getRequestHost returns the host of the http.Request.
func getRequestHost(r *http.Request) (string, error) {
	if r.Host != "" {
		return r.Host, nil
	}
	if r.URL != nil {
		return r.URL.Host, nil
	}
	return "", errors.New("host not set in incoming request")
}
