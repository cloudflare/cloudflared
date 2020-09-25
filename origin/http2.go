package origin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"

	"github.com/pkg/errors"
	"golang.org/x/net/http2"
	"zombiezen.com/go/capnproto2/rpc"
)

const (
	internalUpgradeHeader = "Cf-Cloudflared-Proxy-Connection-Upgrade"
	websocketUpgrade      = "websocket"
	controlPlaneUpgrade   = "control-plane"
)

type http2Server struct {
	server        *http2.Server
	ingressRules  ingress.Ingress
	logger        logger.Service
	connIndexStr  string
	connIndex     uint8
	config        *TunnelConfig
	localAddr     net.Addr
	shutdownChan  chan struct{}
	connectedFuse *h2mux.BooleanFuse
}

func newHTTP2Server(config *TunnelConfig, connIndex uint8, localAddr net.Addr, connectedFuse *h2mux.BooleanFuse) (*http2Server, error) {
	return &http2Server{
		server:        &http2.Server{},
		ingressRules:  config.IngressRules,
		logger:        config.Logger,
		connIndexStr:  uint8ToString(connIndex),
		connIndex:     connIndex,
		config:        config,
		localAddr:     localAddr,
		shutdownChan:  make(chan struct{}),
		connectedFuse: connectedFuse,
	}, nil
}

func (c *http2Server) serve(ctx context.Context, conn net.Conn) {
	go func() {
		<-ctx.Done()
		c.close(conn)
	}()
	c.server.ServeConn(conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: c,
	})
}

func (c *http2Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.config.Metrics.incrementRequests(c.connIndexStr)
	defer c.config.Metrics.decrementConcurrentRequests(c.connIndexStr)

	cfRay := findCfRayHeader(r)
	lbProbe := isLBProbeRequest(r)
	c.logRequest(r, cfRay, lbProbe)

	rule, _ := c.ingressRules.FindMatchingRule(r.Host, r.URL.Path)
	rule.Service.RewriteOriginURL(r.URL)

	var resp *http.Response
	var err error

	if isControlPlaneUpgrade(r) {
		stripWebsocketUpgradeHeader(r)
		err = c.serveControlPlane(w, r)
	} else if isWebsocketUpgrade(r) {
		stripWebsocketUpgradeHeader(r)
		var respBody BidirectionalStream
		respBody, err = newHTTP2Stream(w, r)
		if err == nil {
			resp, err = serveWebsocket(respBody, r, rule)
		}
	} else {
		resp, err = c.serveHTTP(w, r, rule)
	}

	if err != nil {
		c.writeErrorResponse(w, err)
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func (c *http2Server) serveHTTP(w http.ResponseWriter, r *http.Request, rule *ingress.Rule) (*http.Response, error) {
	// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
	if rule.Config.DisableChunkedEncoding {
		r.TransferEncoding = []string{"gzip", "deflate"}
		cLength, err := strconv.Atoi(r.Header.Get("Content-Length"))
		if err == nil {
			r.ContentLength = int64(cLength)
		}
	}

	// Request origin to keep connection alive to improve performance
	r.Header.Set("Connection", "keep-alive")

	if hostHeader := rule.Config.HTTPHostHeader; hostHeader != "" {
		r.Header.Set("Host", hostHeader)
		r.Host = hostHeader
	}

	resp, err := rule.HTTPTransport.RoundTrip(r)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "Copy response error")
	}
	return resp, nil
}

func (c *http2Server) serveControlPlane(w http.ResponseWriter, r *http.Request) error {
	stream, err := newHTTP2Stream(w, r)
	if err != nil {
		return err
	}

	rpcTransport := tunnelrpc.NewTransportLogger(c.logger, rpc.StreamTransport(stream))
	rpcConn := rpc.NewConn(
		rpcTransport,
		tunnelrpc.ConnLog(c.logger),
	)
	rpcClient := tunnelpogs.TunnelServer_PogsClient{Client: rpcConn.Bootstrap(r.Context()), Conn: rpcConn}

	if err = c.registerConnection(r.Context(), rpcClient, 0); err != nil {
		return err
	}
	c.connectedFuse.Fuse(true)

	<-c.shutdownChan
	c.gracefulShutdown(rpcClient)

	// Closing the client will also close the connection
	rpcClient.Close()
	rpcTransport.Close()
	close(c.shutdownChan)
	return nil
}

func (c *http2Server) registerConnection(
	ctx context.Context,
	rpcClient tunnelpogs.TunnelServer_PogsClient,
	numPreviousAttempts uint8,
) error {
	connDetail, err := rpcClient.RegisterConnection(
		ctx,
		c.config.NamedTunnel.Auth,
		c.config.NamedTunnel.ID,
		c.connIndex,
		c.config.ConnectionOptions(c.localAddr.String(), numPreviousAttempts),
	)
	if err != nil {
		c.logger.Errorf("Cannot register connection, err: %v", err)
		return err
	}
	c.logger.Infof("Connection %s registered with %s using ID %s", c.connIndexStr, connDetail.Location, connDetail.UUID)
	return nil
}

func (c *http2Server) gracefulShutdown(rpcClient tunnelpogs.TunnelServer_PogsClient) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GracePeriod)
	defer cancel()
	err := rpcClient.UnregisterConnection(ctx)
	if err != nil {
		c.logger.Errorf("Cannot unregister connection gracefully, err: %v", err)
		return
	}
	c.logger.Info("Sent graceful shutdown signal")

	<-ctx.Done()
}

func (c *http2Server) writeErrorResponse(w http.ResponseWriter, err error) {
	c.logger.Errorf("HTTP request error: %s", err)
	c.config.Metrics.incrementResponses(c.connIndexStr, "502")
	jsonResponseMetaHeader, err := json.Marshal(h2mux.ResponseMetaHeader{Source: h2mux.ResponseSourceCloudflared})
	if err != nil {
		panic(err)
	}
	w.Header().Set(h2mux.ResponseMetaHeaderField, string(jsonResponseMetaHeader))
	w.WriteHeader(http.StatusBadGateway)
}

func (c *http2Server) logRequest(r *http.Request, cfRay string, lbProbe bool) {
	logger := c.logger
	if cfRay != "" {
		logger.Debugf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		logger.Debugf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		logger.Debugf("CF-RAY: %s All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", cfRay, r.Method, r.URL, r.Proto)
	}
	logger.Debugf("CF-RAY: %s Request Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		logger.Debugf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		logger.Debugf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (c *http2Server) logResponseOk(r *http.Response, cfRay string, lbProbe bool) {
	c.config.Metrics.incrementResponses(c.connIndexStr, "200")
	logger := c.logger
	if cfRay != "" {
		logger.Debugf("CF-RAY: %s %s", cfRay, r.Status)
	} else if lbProbe {
		logger.Debugf("Response to Load Balancer health check %s", r.Status)
	} else {
		logger.Infof("%s", r.Status)
	}
	logger.Debugf("CF-RAY: %s Response Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		logger.Debugf("CF-RAY: %s Response content length unknown", cfRay)
	} else {
		logger.Debugf("CF-RAY: %s Response content length %d", cfRay, contentLen)
	}
}

func (c *http2Server) close(conn net.Conn) {
	// Send signal to control loop to start graceful shutdown
	c.shutdownChan <- struct{}{}
	// Wait for control loop to close channel
	<-c.shutdownChan
	conn.Close()
}

type http2Stream struct {
	r       io.Reader
	w       http.ResponseWriter
	flusher http.Flusher
}

func newHTTP2Stream(w http.ResponseWriter, r *http.Request) (*http2Stream, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("ResponseWriter doesn't implement http.Flusher")
	}
	return &http2Stream{r: r.Body, w: w, flusher: flusher}, nil
}

func (wr *http2Stream) WriteRespHeaders(resp *http.Response) error {
	dest := wr.w.Header()
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
	dest.Set(h2mux.ResponseUserHeadersField, h2mux.SerializeHeaders(userHeaders))
	// HTTP2 removes support for 101 Switching Protocols https://tools.ietf.org/html/rfc7540#section-8.1.1
	wr.w.WriteHeader(http.StatusOK)
	wr.flusher.Flush()
	return nil
}

func (wr *http2Stream) Read(p []byte) (n int, err error) {
	return wr.r.Read(p)
}

func (wr *http2Stream) Write(p []byte) (n int, err error) {
	n, err = wr.w.Write(p)
	if err != nil {
		return 0, err
	}
	wr.flusher.Flush()
	return
}

func (wr *http2Stream) Close() error {
	return nil
}

func isControlPlaneUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get(internalUpgradeHeader)) == controlPlaneUpgrade
}

func isWebsocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get(internalUpgradeHeader)) == websocketUpgrade
}

func stripWebsocketUpgradeHeader(r *http.Request) {
	r.Header.Del(internalUpgradeHeader)
}
