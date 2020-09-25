package origin

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/cloudflare/cloudflared/buffer"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/pkg/errors"
)

type TunnelHandler struct {
	ingressRules ingress.Ingress
	muxer        *h2mux.Muxer
	tags         []tunnelpogs.Tag
	metrics      *TunnelMetrics
	// connectionID is only used by metrics, and prometheus requires labels to be string
	connectionID string
	logger       logger.Service

	bufferPool *buffer.Pool
}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewTunnelHandler(ctx context.Context,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connectionID uint8,
	bufferPool *buffer.Pool,
) (*TunnelHandler, string, error) {
	h := &TunnelHandler{
		ingressRules: config.IngressRules,
		tags:         config.Tags,
		metrics:      config.Metrics,
		connectionID: uint8ToString(connectionID),
		logger:       config.Logger,
		bufferPool:   bufferPool,
	}

	edgeConn, err := connection.DialEdge(ctx, dialTimeout, config.TlsConfig, addr)
	if err != nil {
		return nil, "", err
	}
	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	h.muxer, err = h2mux.Handshake(edgeConn, edgeConn, config.muxerConfig(h), h.metrics.activeStreams)
	if err != nil {
		return nil, "", errors.Wrap(err, "h2mux handshake with edge error")
	}
	return h, edgeConn.LocalAddr().String(), nil
}

func (h *TunnelHandler) AppendTagHeaders(r *http.Request) {
	for _, tag := range h.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (h *TunnelHandler) ServeStream(stream *h2mux.MuxedStream) error {
	h.metrics.incrementRequests(h.connectionID)
	defer h.metrics.decrementConcurrentRequests(h.connectionID)

	req, rule, reqErr := h.createRequest(stream)
	if reqErr != nil {
		h.writeErrorResponse(stream, reqErr)
		return reqErr
	}

	cfRay := findCfRayHeader(req)
	lbProbe := isLBProbeRequest(req)
	h.logRequest(req, cfRay, lbProbe)

	var resp *http.Response
	var respErr error
	if websocket.IsWebSocketUpgrade(req) {
		resp, respErr = serveWebsocket(&h2muxWebsocketResp{stream}, req, rule)
	} else {
		resp, respErr = h.serveHTTP(stream, req, rule)
	}
	if respErr != nil {
		h.writeErrorResponse(stream, respErr)
		return respErr
	}
	h.logResponseOk(resp, cfRay, lbProbe)
	return nil
}

func (h *TunnelHandler) createRequest(stream *h2mux.MuxedStream) (*http.Request, *ingress.Rule, error) {
	req, err := http.NewRequest("GET", "http://localhost:8080", h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		return nil, nil, errors.Wrap(err, "Unexpected error from http.NewRequest")
	}
	err = h2mux.H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "invalid request received")
	}
	rule, _ := h.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	rule.Service.RewriteOriginURL(req.URL)
	return req, rule, nil
}

func (h *TunnelHandler) serveHTTP(stream *h2mux.MuxedStream, req *http.Request, rule *ingress.Rule) (*http.Response, error) {
	// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
	if rule.Config.DisableChunkedEncoding {
		req.TransferEncoding = []string{"gzip", "deflate"}
		cLength, err := strconv.Atoi(req.Header.Get("Content-Length"))
		if err == nil {
			req.ContentLength = int64(cLength)
		}
	}

	// Request origin to keep connection alive to improve performance
	req.Header.Set("Connection", "keep-alive")

	if hostHeader := rule.Config.HTTPHostHeader; hostHeader != "" {
		req.Header.Set("Host", hostHeader)
		req.Host = hostHeader
	}

	response, err := h.httpClient.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	defer response.Body.Close()

	headers := h2mux.H1ResponseToH2ResponseHeaders(response)
	headers = append(headers, h2mux.CreateResponseMetaHeader(h2mux.ResponseMetaHeaderField, h2mux.ResponseSourceOrigin))
	err = stream.WriteHeaders(headers)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	if h.isEventStream(response) {
		h.writeEventStream(stream, response.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		buf := h.bufferPool.Get()
		defer h.bufferPool.Put(buf)
		io.CopyBuffer(stream, response.Body, buf)
	}
	return response, nil
}

func (h *TunnelHandler) writeEventStream(stream *h2mux.MuxedStream, responseBody io.ReadCloser) {
	reader := bufio.NewReader(responseBody)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		stream.Write(line)
	}
}

func (h *TunnelHandler) isEventStream(response *http.Response) bool {
	if response.Header.Get("content-type") == "text/event-stream" {
		h.logger.Debug("Detected Server-Side Events from Origin")
		return true
	}
	return false
}

func (h *TunnelHandler) writeErrorResponse(stream *h2mux.MuxedStream, err error) {
	h.logger.Errorf("HTTP request error: %s", err)
	stream.WriteHeaders([]h2mux.Header{
		{Name: ":status", Value: "502"},
		h2mux.CreateResponseMetaHeader(h2mux.ResponseMetaHeaderField, h2mux.ResponseSourceCloudflared),
	})
	stream.Write([]byte("502 Bad Gateway"))
	h.metrics.incrementResponses(h.connectionID, "502")
}

func (h *TunnelHandler) logRequest(req *http.Request, cfRay string, lbProbe bool) {
	logger := h.logger
	if cfRay != "" {
		logger.Debugf("CF-RAY: %s %s %s %s", cfRay, req.Method, req.URL, req.Proto)
	} else if lbProbe {
		logger.Debugf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, req.Method, req.URL, req.Proto)
	} else {
		logger.Infof("CF-RAY: %s All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", cfRay, req.Method, req.URL, req.Proto)
	}
	logger.Debugf("CF-RAY: %s Request Headers %+v", cfRay, req.Header)

	if contentLen := req.ContentLength; contentLen == -1 {
		logger.Debugf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		logger.Debugf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (h *TunnelHandler) logResponseOk(r *http.Response, cfRay string, lbProbe bool) {
	h.metrics.incrementResponses(h.connectionID, "200")
	logger := h.logger
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

func (h *TunnelHandler) UpdateMetrics(connectionID string) {
	h.metrics.updateMuxerMetrics(connectionID, h.muxer.Metrics())
}

type h2muxWebsocketResp struct {
	*h2mux.MuxedStream
}

func (wr *h2muxWebsocketResp) WriteRespHeaders(resp *http.Response) error {
	return wr.WriteHeaders(h2mux.H1ResponseToH2ResponseHeaders(resp))
}
