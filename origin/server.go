package origin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
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
	// TODO: TUN-3406 support websocket, event stream and WSGI servers.
	var resp *http.Response
	var err error
	if strings.ToLower(r.Header.Get("Cf-Int-Argo-Tunnel-Upgrade")) == "websocket" {
		resp, err = serveWebsocket(newWebsocketBody(w, r, c.logger), r, c.config.HTTPHostHeader, c.config.ClientTlsConfig)
	} else {
		resp, err = c.originClient.RoundTrip(r)
	}
	if err != nil {
		c.writeErrorResponse(w, err)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		c.logger.Errorf("Copy response error, err: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
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
	pr *io.PipeReader
	w  http.ResponseWriter
}

func newWebsocketBody(w http.ResponseWriter, r *http.Request, logger logger.Service) *http2WebsocketResp {
	pr, pw := io.Pipe()
	go func() {
		n, err := io.Copy(pw, r.Body)
		logger.Errorf("websocket body copy ended, err: %v, bytes: %d", err, n)
	}()
	return &http2WebsocketResp{pr: pr, w: w}
}

func (wr *http2WebsocketResp) WriteRespHeaders(resp *http.Response) error {
	dest := wr.w.Header()
	for name, values := range resp.Header {
		for _, v := range values {
			dest.Add(name, v)
		}
	}
	return nil
}

func (wr *http2WebsocketResp) Read(p []byte) (n int, err error) {
	return wr.pr.Read(p)
}

func (wr *http2WebsocketResp) Write(p []byte) (n int, err error) {
	return wr.w.Write(p)
}

type h2muxWebsocketResp struct {
	*h2mux.MuxedStream
}

func (wr *h2muxWebsocketResp) WriteRespHeaders(resp *http.Response) error {
	return wr.WriteHeaders(h2mux.H1ResponseToH2ResponseHeaders(resp))
}
