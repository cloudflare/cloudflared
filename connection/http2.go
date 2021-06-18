package connection

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"golang.org/x/net/http2"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// note: these constants are exported so we can reuse them in the edge-side code
const (
	InternalUpgradeHeader     = "Cf-Cloudflared-Proxy-Connection-Upgrade"
	InternalTCPProxySrcHeader = "Cf-Cloudflared-Proxy-Src"
	WebsocketUpgrade          = "websocket"
	ControlStreamUpgrade      = "control-stream"
)

var errEdgeConnectionClosed = fmt.Errorf("connection with edge closed")

type http2Connection struct {
	conn         net.Conn
	server       *http2.Server
	config       *Config
	namedTunnel  *NamedTunnelConfig
	connOptions  *tunnelpogs.ConnectionOptions
	observer     *Observer
	connIndexStr string
	connIndex    uint8
	// newRPCClientFunc allows us to mock RPCs during testing
	newRPCClientFunc func(context.Context, io.ReadWriteCloser, *zerolog.Logger) NamedTunnelRPCClient

	activeRequestsWG  sync.WaitGroup
	connectedFuse     ConnectedFuse
	gracefulShutdownC <-chan struct{}
	stoppedGracefully bool
	controlStreamErr  error // result of running control stream handler
}

func NewHTTP2Connection(
	conn net.Conn,
	config *Config,
	namedTunnelConfig *NamedTunnelConfig,
	connOptions *tunnelpogs.ConnectionOptions,
	observer *Observer,
	connIndex uint8,
	connectedFuse ConnectedFuse,
	gracefulShutdownC <-chan struct{},
) *http2Connection {
	return &http2Connection{
		conn: conn,
		server: &http2.Server{
			MaxConcurrentStreams: math.MaxUint32,
		},
		config:            config,
		namedTunnel:       namedTunnelConfig,
		connOptions:       connOptions,
		observer:          observer,
		connIndexStr:      uint8ToString(connIndex),
		connIndex:         connIndex,
		newRPCClientFunc:  newRegistrationRPCClient,
		connectedFuse:     connectedFuse,
		gracefulShutdownC: gracefulShutdownC,
	}
}

func (c *http2Connection) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		c.close()
	}()
	c.server.ServeConn(c.conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: c,
	})

	switch {
	case c.stoppedGracefully:
		return nil
	case c.controlStreamErr != nil:
		return c.controlStreamErr
	default:
		c.observer.log.Info().Uint8(LogFieldConnIndex, c.connIndex).Msg("Lost connection with the edge")
		return errEdgeConnectionClosed
	}
}

func (c *http2Connection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.activeRequestsWG.Add(1)
	defer c.activeRequestsWG.Done()

	connType := determineHTTP2Type(r)
	handleMissingRequestParts(connType, r)

	respWriter, err := newHTTP2RespWriter(r, w, connType)
	if err != nil {
		c.observer.log.Error().Msg(err.Error())
		return
	}

	var proxyErr error
	switch connType {
	case TypeControlStream:
		proxyErr = c.serveControlStream(r.Context(), respWriter)
		c.controlStreamErr = proxyErr
	case TypeWebsocket:
		stripWebsocketUpgradeHeader(r)
		proxyErr = c.config.OriginProxy.Proxy(respWriter, r, TypeWebsocket)
	default:
		proxyErr = c.config.OriginProxy.Proxy(respWriter, r, connType)
	}
	if proxyErr != nil {
		respWriter.WriteErrorResponse()
	}
}

func (c *http2Connection) serveControlStream(ctx context.Context, respWriter *http2RespWriter) error {
	rpcClient := c.newRPCClientFunc(ctx, respWriter, c.observer.log)
	defer rpcClient.Close()

	if err := rpcClient.RegisterConnection(ctx, c.namedTunnel, c.connOptions, c.connIndex, c.observer); err != nil {
		return err
	}
	c.connectedFuse.Connected()

	// wait for connection termination or start of graceful shutdown
	select {
	case <-ctx.Done():
		break
	case <-c.gracefulShutdownC:
		c.stoppedGracefully = true
	}

	c.observer.sendUnregisteringEvent(c.connIndex)
	rpcClient.GracefulShutdown(ctx, c.config.GracePeriod)
	c.observer.log.Info().Uint8(LogFieldConnIndex, c.connIndex).Msg("Unregistered tunnel connection")
	return nil
}

func (c *http2Connection) close() {
	// Wait for all serve HTTP handlers to return
	c.activeRequestsWG.Wait()
	c.conn.Close()
}

type http2RespWriter struct {
	r           io.Reader
	w           http.ResponseWriter
	flusher     http.Flusher
	shouldFlush bool
}

func newHTTP2RespWriter(r *http.Request, w http.ResponseWriter, connType Type) (*http2RespWriter, error) {
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		respWriter := &http2RespWriter{
			r: r.Body,
			w: w,
		}
		respWriter.WriteErrorResponse()
		return nil, fmt.Errorf("%T doesn't implement http.Flusher", w)
	}

	return &http2RespWriter{
		r:           r.Body,
		w:           w,
		flusher:     flusher,
		shouldFlush: connType.shouldFlush(),
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
			// so it should be sent as an HTTP/2 response header.
			dest[name] = values
			// Since these are http2 headers, they're required to be lowercase
		} else if !IsControlHeader(h2name) || IsWebsocketClientHeader(h2name) {
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
			println("Recover from http2 response writer panic, error", r)
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

// IsTCPStream discerns if the connection request needs a tcp stream proxy.
func IsTCPStream(r *http.Request) bool {
	return r.Header.Get(InternalTCPProxySrcHeader) != ""
}

func stripWebsocketUpgradeHeader(r *http.Request) {
	r.Header.Del(InternalUpgradeHeader)
}
