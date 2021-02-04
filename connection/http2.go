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

	"github.com/cloudflare/cloudflared/h2mux"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"

	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

const (
	internalUpgradeHeader = "Cf-Cloudflared-Proxy-Connection-Upgrade"
	websocketUpgrade      = "websocket"
	controlStreamUpgrade  = "control-stream"
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

	respWriter := &http2RespWriter{
		r: r.Body,
		w: w,
	}
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		c.observer.log.Error().Msgf("%T doesn't implement http.Flusher", w)
		respWriter.WriteErrorResponse()
		return
	}
	respWriter.flusher = flusher
	var err error
	if isControlStreamUpgrade(r) {
		respWriter.shouldFlush = true
		err = c.serveControlStream(r.Context(), respWriter)
		c.controlStreamErr = err
	} else if isWebsocketUpgrade(r) {
		respWriter.shouldFlush = true
		stripWebsocketUpgradeHeader(r)
		err = c.config.OriginClient.Proxy(respWriter, r, true)
	} else {
		err = c.config.OriginClient.Proxy(respWriter, r, false)
	}

	if err != nil {
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

func (rp *http2RespWriter) WriteRespHeaders(resp *http.Response) error {
	dest := rp.w.Header()
	userHeaders := make(http.Header, len(resp.Header))
	for header, values := range resp.Header {
		// Since these are http2 headers, they're required to be lowercase
		h2name := strings.ToLower(header)
		for _, v := range values {
			if h2name == "content-length" {
				// This header has meaning in HTTP/2 and will be used by the edge,
				// so it should be sent as an HTTP/2 response header.
				dest.Add(h2name, v)
				// Since these are http2 headers, they're required to be lowercase
			} else if !h2mux.IsControlHeader(h2name) || h2mux.IsWebsocketClientHeader(h2name) {
				// User headers, on the other hand, must all be serialized so that
				// HTTP/2 header validation won't be applied to HTTP/1 header values
				userHeaders.Add(h2name, v)
			}
		}
	}

	// Perform user header serialization and set them in the single header
	dest.Set(canonicalResponseUserHeadersField, h2mux.SerializeHeaders(userHeaders))
	rp.setResponseMetaHeader(responseMetaHeaderOrigin)
	status := resp.StatusCode
	// HTTP2 removes support for 101 Switching Protocols https://tools.ietf.org/html/rfc7540#section-8.1.1
	if status == http.StatusSwitchingProtocols {
		status = http.StatusOK
	}
	rp.w.WriteHeader(status)
	if IsServerSentEvent(resp.Header) {
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
	rp.w.Header().Set(canonicalResponseMetaHeaderField, value)
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

func isControlStreamUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get(internalUpgradeHeader)) == controlStreamUpgrade
}

func isWebsocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get(internalUpgradeHeader)) == websocketUpgrade
}

func stripWebsocketUpgradeHeader(r *http.Request) {
	r.Header.Del(internalUpgradeHeader)
}
