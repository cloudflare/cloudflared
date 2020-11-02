package connection

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/cloudflare/cloudflared/h2mux"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"

	"golang.org/x/net/http2"
)

const (
	internalUpgradeHeader = "Cf-Cloudflared-Proxy-Connection-Upgrade"
	websocketUpgrade      = "websocket"
	controlStreamUpgrade  = "control-stream"
)

var (
	errNotFlusher = errors.New("ResponseWriter doesn't implement http.Flusher")
)

type HTTP2Connection struct {
	conn          net.Conn
	server        *http2.Server
	config        *Config
	namedTunnel   *NamedTunnelConfig
	connOptions   *tunnelpogs.ConnectionOptions
	observer      *Observer
	connIndexStr  string
	connIndex     uint8
	wg            *sync.WaitGroup
	connectedFuse ConnectedFuse
}

func NewHTTP2Connection(
	conn net.Conn,
	config *Config,
	namedTunnelConfig *NamedTunnelConfig,
	connOptions *tunnelpogs.ConnectionOptions,
	observer *Observer,
	connIndex uint8,
	connectedFuse ConnectedFuse,
) *HTTP2Connection {
	return &HTTP2Connection{
		conn: conn,
		server: &http2.Server{
			MaxConcurrentStreams: math.MaxUint32,
		},
		config:        config,
		namedTunnel:   namedTunnelConfig,
		connOptions:   connOptions,
		observer:      observer,
		connIndexStr:  uint8ToString(connIndex),
		connIndex:     connIndex,
		wg:            &sync.WaitGroup{},
		connectedFuse: connectedFuse,
	}
}

func (c *HTTP2Connection) Serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		c.close()
	}()
	c.server.ServeConn(c.conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: c,
	})
}

func (c *HTTP2Connection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.wg.Add(1)
	defer c.wg.Done()

	respWriter := &http2RespWriter{
		r: r.Body,
		w: w,
	}
	flusher, isFlusher := w.(http.Flusher)
	if !isFlusher {
		c.observer.Errorf("%T doesn't implement http.Flusher", w)
		respWriter.WriteErrorResponse(errNotFlusher)
		return
	}
	respWriter.flusher = flusher
	if isControlStreamUpgrade(r) {
		respWriter.shouldFlush = true
		err := c.serveControlStream(r.Context(), respWriter)
		if err != nil {
			respWriter.WriteErrorResponse(err)
		}
	} else if isWebsocketUpgrade(r) {
		respWriter.shouldFlush = true
		stripWebsocketUpgradeHeader(r)
		c.config.OriginClient.Proxy(respWriter, r, true)
	} else {
		c.config.OriginClient.Proxy(respWriter, r, false)
	}
}

func (c *HTTP2Connection) serveControlStream(ctx context.Context, respWriter *http2RespWriter) error {
	rpcClient := newRegistrationRPCClient(ctx, respWriter, c.observer)
	defer rpcClient.close()

	if err := registerConnection(ctx, rpcClient, c.namedTunnel, c.connOptions, c.connIndex, c.observer); err != nil {
		return err
	}
	c.connectedFuse.Connected()

	<-ctx.Done()
	c.gracefulShutdown(ctx, rpcClient)
	return nil
}

func (c *HTTP2Connection) registerConnection(
	ctx context.Context,
	rpcClient tunnelpogs.RegistrationServer_PogsClient,
) error {
	connDetail, err := rpcClient.RegisterConnection(
		ctx,
		c.namedTunnel.Auth,
		c.namedTunnel.ID,
		c.connIndex,
		c.connOptions,
	)
	if err != nil {
		c.observer.Errorf("Cannot register connection, err: %v", err)
		return err
	}
	c.observer.Infof("Connection %s registered with %s using ID %s", c.connIndexStr, connDetail.Location, connDetail.UUID)
	return nil
}

func (c *HTTP2Connection) gracefulShutdown(ctx context.Context, rpcClient *registrationServerClient) {
	ctx, cancel := context.WithTimeout(ctx, c.config.GracePeriod)
	defer cancel()
	rpcClient.client.UnregisterConnection(ctx)
}

func (c *HTTP2Connection) close() {
	// Wait for all serve HTTP handlers to return
	c.wg.Wait()
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
	if isServerSentEvent(resp.Header) {
		rp.shouldFlush = true
	}
	if rp.shouldFlush {
		rp.flusher.Flush()
	}
	return nil
}

func (rp *http2RespWriter) WriteErrorResponse(err error) {
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
