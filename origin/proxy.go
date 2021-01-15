package origin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflared/buffer"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	TagHeaderNamePrefix = "Cf-Warp-Tag-"
)

type client struct {
	ingressRules ingress.Ingress
	tags         []tunnelpogs.Tag
	log          *zerolog.Logger
	bufferPool   *buffer.Pool
}

func NewClient(ingressRules ingress.Ingress, tags []tunnelpogs.Tag, log *zerolog.Logger) connection.OriginClient {
	return &client{
		ingressRules: ingressRules,
		tags:         tags,
		log:          log,
		bufferPool:   buffer.NewPool(512 * 1024),
	}
}

func (c *client) Proxy(w connection.ResponseWriter, req *http.Request, isWebsocket bool) error {
	incrementRequests()
	defer decrementConcurrentRequests()

	cfRay := findCfRayHeader(req)
	lbProbe := isLBProbeRequest(req)

	c.appendTagHeaders(req)
	rule, ruleNum := c.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	c.logRequest(req, cfRay, lbProbe, ruleNum)

	var (
		resp *http.Response
		err  error
	)
	if isWebsocket {
		resp, err = c.proxyWebsocket(w, req, rule)
	} else {
		resp, err = c.proxyHTTP(w, req, rule)
	}
	if err != nil {
		c.logRequestError(err, cfRay, ruleNum)
		w.WriteErrorResponse()
		return err
	}
	c.logOriginResponse(resp, cfRay, lbProbe, ruleNum)
	return nil
}

func (c *client) proxyHTTP(w connection.ResponseWriter, req *http.Request, rule *ingress.Rule) (*http.Response, error) {
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

	resp, err := rule.Service.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	defer resp.Body.Close()

	err = w.WriteRespHeaders(resp)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	if connection.IsServerSentEvent(resp.Header) {
		c.log.Debug().Msg("Detected Server-Side Events from Origin")
		c.writeEventStream(w, resp.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		buf := c.bufferPool.Get()
		defer c.bufferPool.Put(buf)
		_, _ = io.CopyBuffer(w, resp.Body, buf)
	}
	return resp, nil
}

func (c *client) proxyWebsocket(w connection.ResponseWriter, req *http.Request, rule *ingress.Rule) (*http.Response, error) {
	if hostHeader := rule.Config.HTTPHostHeader; hostHeader != "" {
		req.Header.Set("Host", hostHeader)
		req.Host = hostHeader
	}

	dialler, ok := rule.Service.(websocket.Dialler)
	if !ok {
		return nil, fmt.Errorf("Websockets aren't supported by the origin service '%s'", rule.Service)
	}
	conn, resp, err := websocket.ClientConnect(req, dialler)
	if err != nil {
		return nil, err
	}

	serveCtx, cancel := context.WithCancel(req.Context())
	connClosedChan := make(chan struct{})
	go func() {
		// serveCtx is done if req is cancelled, or streamWebsocket returns
		<-serveCtx.Done()
		_ = conn.Close()
		close(connClosedChan)
	}()

	// Copy to/from stream to the undelying connection. Use the underlying
	// connection because cloudflared doesn't operate on the message themselves
	err = c.streamWebsocket(w, conn.UnderlyingConn(), resp)
	cancel()

	// We need to make sure conn is closed before returning, otherwise we might write to conn after Proxy returns
	<-connClosedChan
	return resp, err
}

func (c *client) streamWebsocket(w connection.ResponseWriter, conn net.Conn, resp *http.Response) error {
	err := w.WriteRespHeaders(resp)
	if err != nil {
		return errors.Wrap(err, "Error writing websocket response header")
	}
	websocket.Stream(conn, w)
	return nil
}

func (c *client) writeEventStream(w connection.ResponseWriter, respBody io.ReadCloser) {
	reader := bufio.NewReader(respBody)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		_, _ = w.Write(line)
	}
}

func (c *client) appendTagHeaders(r *http.Request) {
	for _, tag := range c.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (c *client) logRequest(r *http.Request, cfRay string, lbProbe bool, ruleNum int) {
	if cfRay != "" {
		c.log.Debug().Msgf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		c.log.Debug().Msgf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		c.log.Debug().Msgf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", r.Method, r.URL, r.Proto)
	}
	c.log.Debug().Msgf("CF-RAY: %s Request Headers %+v", cfRay, r.Header)
	c.log.Debug().Msgf("CF-RAY: %s Serving with ingress rule %d", cfRay, ruleNum)

	if contentLen := r.ContentLength; contentLen == -1 {
		c.log.Debug().Msgf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		c.log.Debug().Msgf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (c *client) logOriginResponse(r *http.Response, cfRay string, lbProbe bool, ruleNum int) {
	responseByCode.WithLabelValues(strconv.Itoa(r.StatusCode)).Inc()
	if cfRay != "" {
		c.log.Debug().Msgf("CF-RAY: %s Status: %s served by ingress %d", cfRay, r.Status, ruleNum)
	} else if lbProbe {
		c.log.Debug().Msgf("Response to Load Balancer health check %s", r.Status)
	} else {
		c.log.Debug().Msgf("Status: %s served by ingress %d", r.Status, ruleNum)
	}
	c.log.Debug().Msgf("CF-RAY: %s Response Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		c.log.Debug().Msgf("CF-RAY: %s Response content length unknown", cfRay)
	} else {
		c.log.Debug().Msgf("CF-RAY: %s Response content length %d", cfRay, contentLen)
	}
}

func (c *client) logRequestError(err error, cfRay string, ruleNum int) {
	requestErrors.Inc()
	if cfRay != "" {
		c.log.Error().Msgf("CF-RAY: %s Proxying to ingress %d error: %v", cfRay, ruleNum, err)
	} else {
		c.log.Error().Msgf("Proxying to ingress %d error: %v", ruleNum, err)
	}

}

func findCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}
