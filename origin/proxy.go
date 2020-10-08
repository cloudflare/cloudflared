package origin

import (
	"bufio"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflared/buffer"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/pkg/errors"
)

const (
	TagHeaderNamePrefix = "Cf-Warp-Tag-"
)

type client struct {
	config     *ProxyConfig
	logger     logger.Service
	bufferPool *buffer.Pool
}

func NewClient(config *ProxyConfig, logger logger.Service) connection.OriginClient {
	return &client{
		config:     config,
		logger:     logger,
		bufferPool: buffer.NewPool(512 * 1024),
	}
}

type ProxyConfig struct {
	Client            http.RoundTripper
	URL               *url.URL
	TLSConfig         *tls.Config
	HostHeader        string
	NoChunkedEncoding bool
	Tags              []tunnelpogs.Tag
}

func (c *client) Proxy(w connection.ResponseWriter, req *http.Request, isWebsocket bool) error {
	incrementRequests()
	defer decrementConcurrentRequests()

	cfRay := findCfRayHeader(req)
	lbProbe := isLBProbeRequest(req)

	c.appendTagHeaders(req)
	c.logRequest(req, cfRay, lbProbe)
	var (
		resp *http.Response
		err  error
	)
	if isWebsocket {
		resp, err = c.proxyWebsocket(w, req)
	} else {
		resp, err = c.proxyHTTP(w, req)
	}
	if err != nil {
		c.logger.Errorf("HTTP request error: %s", err)
		responseByCode.WithLabelValues("502").Inc()
		w.WriteErrorResponse(err)
		return err
	}
	c.logResponseOk(resp, cfRay, lbProbe)
	return nil
}

func (c *client) proxyHTTP(w connection.ResponseWriter, req *http.Request) (*http.Response, error) {
	// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
	if c.config.NoChunkedEncoding {
		req.TransferEncoding = []string{"gzip", "deflate"}
		cLength, err := strconv.Atoi(req.Header.Get("Content-Length"))
		if err == nil {
			req.ContentLength = int64(cLength)
		}
	}

	// Request origin to keep connection alive to improve performance
	req.Header.Set("Connection", "keep-alive")

	c.setHostHeader(req)

	resp, err := c.config.Client.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	defer resp.Body.Close()

	err = w.WriteRespHeaders(resp)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	if isEventStream(resp) {
		//h.observer.Debug("Detected Server-Side Events from Origin")
		c.writeEventStream(w, resp.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		buf := c.bufferPool.Get()
		defer c.bufferPool.Put(buf)
		io.CopyBuffer(w, resp.Body, buf)
	}
	return resp, nil
}

func (c *client) proxyWebsocket(w connection.ResponseWriter, req *http.Request) (*http.Response, error) {
	c.setHostHeader(req)

	conn, resp, err := websocket.ClientConnect(req, c.config.TLSConfig)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = w.WriteRespHeaders(resp)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	// Copy to/from stream to the undelying connection. Use the underlying
	// connection because cloudflared doesn't operate on the message themselves
	websocket.Stream(conn.UnderlyingConn(), w)

	return resp, nil
}

func (c *client) writeEventStream(w connection.ResponseWriter, respBody io.ReadCloser) {
	reader := bufio.NewReader(respBody)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		w.Write(line)
	}
}

func (c *client) setHostHeader(req *http.Request) {
	if c.config.HostHeader != "" {
		req.Header.Set("Host", c.config.HostHeader)
		req.Host = c.config.HostHeader
	}
}

func (c *client) appendTagHeaders(r *http.Request) {
	for _, tag := range c.config.Tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (c *client) logRequest(r *http.Request, cfRay string, lbProbe bool) {
	if cfRay != "" {
		c.logger.Debugf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		c.logger.Debugf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		c.logger.Debugf("CF-RAY: %s All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", cfRay, r.Method, r.URL, r.Proto)
	}
	c.logger.Debugf("CF-RAY: %s Request Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		c.logger.Debugf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		c.logger.Debugf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (c *client) logResponseOk(r *http.Response, cfRay string, lbProbe bool) {
	responseByCode.WithLabelValues("200").Inc()
	if cfRay != "" {
		c.logger.Debugf("CF-RAY: %s %s", cfRay, r.Status)
	} else if lbProbe {
		c.logger.Debugf("Response to Load Balancer health check %s", r.Status)
	} else {
		c.logger.Infof("%s", r.Status)
	}
	c.logger.Debugf("CF-RAY: %s Response Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		c.logger.Debugf("CF-RAY: %s Response content length unknown", cfRay)
	} else {
		c.logger.Debugf("CF-RAY: %s Response content length %d", cfRay, contentLen)
	}
}

func findCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}

func isEventStream(response *http.Response) bool {
	if response.Header.Get("content-type") == "text/event-stream" {
		return true
	}
	return false
}
