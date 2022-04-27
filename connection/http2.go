package connection

import (
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

	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
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
	connOptions  *tunnelpogs.ConnectionOptions
	observer     *Observer
	connIndex    uint8
	// newRPCClientFunc allows us to mock RPCs during testing
	newRPCClientFunc func(context.Context, io.ReadWriteCloser, *zerolog.Logger) NamedTunnelRPCClient

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
	connOptions *tunnelpogs.ConnectionOptions,
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
		newRPCClientFunc:     newRegistrationRPCClient,
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

	switch connType {
	case TypeControlStream:
		if err := c.controlStreamHandler.ServeControlStream(r.Context(), respWriter, c.connOptions, c.orchestrator); err != nil {
			c.controlStreamErr = err
			c.log.Error().Err(err)
			respWriter.WriteErrorResponse()
		}

	case TypeConfiguration:
		if err := c.handleConfigurationUpdate(respWriter, r); err != nil {
			c.log.Error().Err(err)
			respWriter.WriteErrorResponse()
		}

	case TypeWebsocket, TypeHTTP:
		stripWebsocketUpgradeHeader(r)
		// Check for tracing on request
		tr := tracing.NewTracedRequest(r)
		if err := originProxy.ProxyHTTP(respWriter, tr, connType == TypeWebsocket); err != nil {
			err := fmt.Errorf("Failed to proxy HTTP: %w", err)
			c.log.Error().Err(err)
			respWriter.WriteErrorResponse()
		}

	case TypeTCP:
		host, err := getRequestHost(r)
		if err != nil {
			err := fmt.Errorf(`cloudflared received a warp-routing request with an empty host value: %w`, err)
			c.log.Error().Err(err)
			respWriter.WriteErrorResponse()
		}

		rws := NewHTTPResponseReadWriterAcker(respWriter, r)
		if err := originProxy.ProxyTCP(r.Context(), rws, &TCPRequest{
			Dest:    host,
			CFRay:   FindCfRayHeader(r),
			LBProbe: IsLBProbeRequest(r),
		}); err != nil {
			respWriter.WriteErrorResponse()
		}

	default:
		err := fmt.Errorf("Received unknown connection type: %s", connType)
		c.log.Error().Err(err)
		respWriter.WriteErrorResponse()
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
	r           io.Reader
	w           http.ResponseWriter
	flusher     http.Flusher
	shouldFlush bool
	log         *zerolog.Logger
}

func NewHTTP2RespWriter(r *http.Request, w http.ResponseWriter, connType Type, log *zerolog.Logger) (*http2RespWriter, error) {
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		respWriter := &http2RespWriter{
			r:   r.Body,
			w:   w,
			log: log,
		}
		respWriter.WriteErrorResponse()
		return nil, fmt.Errorf("%T doesn't implement http.Flusher", w)
	}

	return &http2RespWriter{
		r:           r.Body,
		w:           w,
		flusher:     flusher,
		shouldFlush: connType.shouldFlush(),
		log:         log,
	}, nil
}

func (rp *http2RespWriter) WriteRespHeaders(status int, header http.Header) error {
	dest := rp.w.Header()
	userHeaders := make(http.Header, len(header))
	for name, values := range header {
		// Since these are http2 headers, they're required to be lowercase
		h2name := strings.ToLower(name)

		if h2name == "content-length" {
			// This header has meaning in HTTP/2 and will be used by the edge,
			// so it should be sent *also* as an HTTP/2 response header.
			dest[name] = values
		}

		if h2name == tracing.IntCloudflaredTracingHeader {
			// Add cf-int-cloudflared-tracing header outside of serialized userHeaders
			rp.w.Header()[tracing.CanonicalCloudflaredTracingHeader] = values
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
	if IsServerSentEvent(header) {
		rp.shouldFlush = true
	}
	if rp.shouldFlush {
		rp.flusher.Flush()
	}
	return nil
}

func (rp *http2RespWriter) WriteErrorResponse() {
	rp.setResponseMetaHeader(responseMetaHeaderCfd)
	rp.w.WriteHeader(http.StatusBadGateway)
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
		// for proxying. We use the same values as we used to in h2mux. For proxying they should not matter since we
		// control the dialer on every egress proxied.
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
