package origin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"
)

type cfdServer struct {
	httpServer      *http2.Server
	originClient    http.RoundTripper
	logger          logger.Service
	originURL       *url.URL
	connectionIndex string
	config          *TunnelConfig
}

func (c *cfdServer) serve(ctx context.Context, conn net.Conn) {
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	c.httpServer.ServeConn(conn, &http2.ServeConnOpts{
		Context: ctx,
		Handler: c,
	})
}

func (c *cfdServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.config.Metrics.incrementRequests(c.connectionIndex)
	defer c.config.Metrics.decrementConcurrentRequests(c.connectionIndex)

	cfRay := findCfRayHeader(r)
	lbProbe := isLBProbeRequest(r)
	c.logRequest(r, cfRay, lbProbe)

	r.URL = c.originURL
	c.logger.Infof("URL %v", r.URL)
	// TODO: TUN-3406 support websocket, event stream and WSGI servers.
	var resp *http.Response
	var err error

	if isWebsocketUpgrade(r) {
		var respBody WebsocketResp
		respBody, err = newWebsocketBody(w, r)
		if err == nil {
			resp, err = serveWebsocket(respBody, r, c.config.HTTPHostHeader, c.config.ClientTlsConfig)
		}
	} else {
		resp, err = c.serveHTTP(w, r)
	}

	if err != nil {
		c.writeErrorResponse(w, err)
		return
	}
	defer resp.Body.Close()

}

func (c *cfdServer) serveHTTP(w http.ResponseWriter, r *http.Request) (*http.Response, error) {
	resp, err := c.originClient.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "Copy response error")
	}
	return resp, nil
}

func (c *cfdServer) writeErrorResponse(w http.ResponseWriter, err error) {
	c.logger.Errorf("HTTP request error: %s", err)
	c.config.Metrics.incrementResponses(c.connectionIndex, "502")
	jsonResponseMetaHeader, err := json.Marshal(h2mux.ResponseMetaHeader{Source: h2mux.ResponseSourceCloudflared})
	if err != nil {
		panic(err)
	}
	w.Header().Set(h2mux.ResponseMetaHeaderField, string(jsonResponseMetaHeader))
	w.WriteHeader(http.StatusBadGateway)
}

func (c *cfdServer) logRequest(r *http.Request, cfRay string, lbProbe bool) {
	logger := c.logger
	if cfRay != "" {
		logger.Debugf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		logger.Debugf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		logger.Debugf("CF-RAY: %s All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", cfRay, r.Method, r.URL, r.Proto)
	}
	logger.Infof("CF-RAY: %s Request Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		logger.Debugf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		logger.Debugf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (c *cfdServer) logResponseOk(r *http.Response, cfRay string, lbProbe bool) {
	c.config.Metrics.incrementResponses(c.connectionIndex, "200")
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

type WebsocketResp interface {
	WriteRespHeaders(*http.Response) error
	io.ReadWriter
}

type http2WebsocketResp struct {
	r       io.Reader
	w       http.ResponseWriter
	flusher http.Flusher
}

func newWebsocketBody(w http.ResponseWriter, r *http.Request) (*http2WebsocketResp, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("ResponseWriter doesn't implement http.Flusher")
	}
	return &http2WebsocketResp{r: r.Body, w: w, flusher: flusher}, nil
}

func (wr *http2WebsocketResp) WriteRespHeaders(resp *http.Response) error {
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

func (wr *http2WebsocketResp) Read(p []byte) (n int, err error) {
	return wr.r.Read(p)
}

func (wr *http2WebsocketResp) Write(p []byte) (n int, err error) {
	n, err = wr.w.Write(p)
	if err != nil {
		return 0, err
	}
	wr.flusher.Flush()
	return
}

type h2muxWebsocketResp struct {
	*h2mux.MuxedStream
}

func (wr *h2muxWebsocketResp) WriteRespHeaders(resp *http.Response) error {
	return wr.WriteHeaders(h2mux.H1ResponseToH2ResponseHeaders(resp))
}

func isWebsocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Cf-Int-Tunnel-Upgrade")) == "websocket"
}
