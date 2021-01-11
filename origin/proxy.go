package origin

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

type proxy struct {
	ingressRules ingress.Ingress
	tags         []tunnelpogs.Tag
	log          *zerolog.Logger
	bufferPool   *buffer.Pool
}

func NewOriginProxy(ingressRules ingress.Ingress, tags []tunnelpogs.Tag, log *zerolog.Logger) connection.OriginProxy {
	return &proxy{
		ingressRules: ingressRules,
		tags:         tags,
		log:          log,
		bufferPool:   buffer.NewPool(512 * 1024),
	}
}

func (p *proxy) Proxy(w connection.ResponseWriter, req *http.Request, sourceConnectionType connection.Type) error {
	incrementRequests()
	defer decrementConcurrentRequests()

	cfRay := findCfRayHeader(req)
	lbProbe := isLBProbeRequest(req)

	p.appendTagHeaders(req)
	rule, ruleNum := p.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	p.logRequest(req, cfRay, lbProbe, ruleNum)

	if sourceConnectionType == connection.TypeHTTP {
		resp, err := p.proxyHTTP(w, req, rule)
		if err != nil {
			p.logErrorAndWriteResponse(w, err, cfRay, ruleNum)
			return err
		}

		p.logOriginResponse(resp, cfRay, lbProbe, ruleNum)
		return nil
	}

	respHeader := http.Header{}
	if sourceConnectionType == connection.TypeWebsocket {
		go websocket.NewConn(w, p.log).Pinger(req.Context())
		respHeader = websocket.NewResponseHeader(req)
	}

	connClosedChan := make(chan struct{})
	err := p.proxyConnection(connClosedChan, w, req, rule)
	if err != nil {
		p.logErrorAndWriteResponse(w, err, cfRay, ruleNum)
		return err
	}

	status := http.StatusSwitchingProtocols
	resp := &http.Response{
		Status:        http.StatusText(status),
		StatusCode:    status,
		Header:        respHeader,
		ContentLength: -1,
	}
	w.WriteRespHeaders(http.StatusSwitchingProtocols, nil)

	<-connClosedChan

	p.logOriginResponse(resp, cfRay, lbProbe, ruleNum)
	return nil
}

func (p *proxy) logErrorAndWriteResponse(w connection.ResponseWriter, err error, cfRay string, ruleNum int) {
	p.logRequestError(err, cfRay, ruleNum)
	w.WriteErrorResponse()
}

func (p *proxy) proxyHTTP(w connection.ResponseWriter, req *http.Request, rule *ingress.Rule) (*http.Response, error) {
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

	httpService, ok := rule.Service.(ingress.HTTPOriginProxy)
	if !ok {
		p.log.Error().Msgf("%s is not a http service", rule.Service)
		return nil, fmt.Errorf("Not a http service")
	}

	resp, err := httpService.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	defer resp.Body.Close()

	err = w.WriteRespHeaders(resp.StatusCode, resp.Header)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	if connection.IsServerSentEvent(resp.Header) {
		p.log.Debug().Msg("Detected Server-Side Events from Origin")
		p.writeEventStream(w, resp.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		_, _ = io.CopyBuffer(w, resp.Body, buf)
	}
	return resp, nil
}

func (p *proxy) proxyConnection(connClosedChan chan struct{},
	conn io.ReadWriter, req *http.Request, rule *ingress.Rule) error {
	if hostHeader := rule.Config.HTTPHostHeader; hostHeader != "" {
		req.Header.Set("Host", hostHeader)
		req.Host = hostHeader
	}

	connectionService, ok := rule.Service.(ingress.StreamBasedOriginProxy)
	if !ok {
		p.log.Error().Msgf("%s is not a connection-oriented service", rule.Service)
		return fmt.Errorf("Not a connection-oriented service")
	}
	originConn, err := connectionService.EstablishConnection(req)
	if err != nil {
		return err
	}

	serveCtx, cancel := context.WithCancel(req.Context())
	go func() {
		// serveCtx is done if req is cancelled, or streamWebsocket returns
		<-serveCtx.Done()
		originConn.Close()
		close(connClosedChan)
	}()

	go func() {
		originConn.Stream(conn)
		cancel()
	}()

	return nil
}

func (p *proxy) writeEventStream(w connection.ResponseWriter, respBody io.ReadCloser) {
	reader := bufio.NewReader(respBody)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		_, _ = w.Write(line)
	}
}

func (p *proxy) appendTagHeaders(r *http.Request) {
	for _, tag := range p.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (p *proxy) logRequest(r *http.Request, cfRay string, lbProbe bool, ruleNum int) {
	if cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		p.log.Debug().Msgf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		p.log.Debug().Msgf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", r.Method, r.URL, r.Proto)
	}
	p.log.Debug().Msgf("CF-RAY: %s Request Headers %+v", cfRay, r.Header)
	p.log.Debug().Msgf("CF-RAY: %s Serving with ingress rule %d", cfRay, ruleNum)

	if contentLen := r.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (p *proxy) logOriginResponse(r *http.Response, cfRay string, lbProbe bool, ruleNum int) {
	responseByCode.WithLabelValues(strconv.Itoa(r.StatusCode)).Inc()
	if cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s Status: %s served by ingress %d", cfRay, r.Status, ruleNum)
	} else if lbProbe {
		p.log.Debug().Msgf("Response to Load Balancer health check %s", r.Status)
	} else {
		p.log.Debug().Msgf("Status: %s served by ingress %d", r.Status, ruleNum)
	}
	p.log.Debug().Msgf("CF-RAY: %s Response Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Response content length unknown", cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Response content length %d", cfRay, contentLen)
	}
}

func (p *proxy) logRequestError(err error, cfRay string, ruleNum int) {
	requestErrors.Inc()
	if cfRay != "" {
		p.log.Error().Msgf("CF-RAY: %s Proxying to ingress %d error: %v", cfRay, ruleNum, err)
	} else {
		p.log.Error().Msgf("Proxying to ingress %d error: %v", ruleNum, err)
	}

}

func findCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}
