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
	warpRouting  *ingress.WarpRoutingService
	tags         []tunnelpogs.Tag
	log          *zerolog.Logger
	bufferPool   *buffer.Pool
}

func NewOriginProxy(
	ingressRules ingress.Ingress,
	warpRouting *ingress.WarpRoutingService,
	tags []tunnelpogs.Tag,
	log *zerolog.Logger) connection.OriginProxy {

	return &proxy{
		ingressRules: ingressRules,
		warpRouting:  warpRouting,
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

	serveCtx, cancel := context.WithCancel(req.Context())
	defer cancel()

	p.appendTagHeaders(req)
	if sourceConnectionType == connection.TypeTCP {
		if p.warpRouting == nil {
			err := errors.New(`cloudflared received a request from Warp client, but your configuration has disabled ingress from Warp clients. To enable this, set "warp-routing:\n\t enabled: true" in your config.yaml`)
			p.log.Error().Msg(err.Error())
			return err
		}
		resp, err := p.proxyConnection(serveCtx, w, req, sourceConnectionType, p.warpRouting.Proxy)
		if err != nil {
			p.logRequestError(err, cfRay, ingress.ServiceWarpRouting)
			w.WriteErrorResponse()
			return err
		}
		p.logOriginResponse(resp, cfRay, lbProbe, ingress.ServiceWarpRouting)
		return nil
	}

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

	if hostHeader := rule.Config.HTTPHostHeader; hostHeader != "" {
		req.Header.Set("Host", hostHeader)
		req.Host = hostHeader
	}

	connectionProxy, ok := rule.Service.(ingress.StreamBasedOriginProxy)
	if !ok {
		p.log.Error().Msgf("%s is not a connection-oriented service", rule.Service)
		return fmt.Errorf("Not a connection-oriented service")
	}

	resp, err := p.proxyConnection(serveCtx, w, req, sourceConnectionType, connectionProxy)
	if err != nil {
		p.logErrorAndWriteResponse(w, err, cfRay, ruleNum)
		return err
	}

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

func (p *proxy) proxyConnection(
	serveCtx context.Context,
	w connection.ResponseWriter,
	req *http.Request,
	sourceConnectionType connection.Type,
	connectionProxy ingress.StreamBasedOriginProxy,
) (*http.Response, error) {
	originConn, err := connectionProxy.EstablishConnection(req)
	if err != nil {
		return nil, err
	}

	var eyeballConn io.ReadWriter = w
	respHeader := http.Header{}
	if sourceConnectionType == connection.TypeWebsocket {
		wsReadWriter := websocket.NewConn(serveCtx, w, p.log)
		// If cloudflared <-> origin is not websocket, we need to decode TCP data out of WS frames
		if originConn.Type() != sourceConnectionType {
			eyeballConn = wsReadWriter
		}
		respHeader = websocket.NewResponseHeader(req)
	}
	status := http.StatusSwitchingProtocols
	resp := &http.Response{
		Status:        http.StatusText(status),
		StatusCode:    status,
		Header:        respHeader,
		ContentLength: -1,
	}
	w.WriteRespHeaders(http.StatusSwitchingProtocols, respHeader)
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}

	streamCtx, cancel := context.WithCancel(serveCtx)
	defer cancel()

	go func() {
		// streamCtx is done if req is cancelled or if Stream returns
		<-streamCtx.Done()
		originConn.Close()
	}()

	originConn.Stream(eyeballConn)
	return resp, nil
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

func (p *proxy) logRequest(r *http.Request, cfRay string, lbProbe bool, rule interface{}) {
	if cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else if lbProbe {
		p.log.Debug().Msgf("CF-RAY: %s Load Balancer health check %s %s %s", cfRay, r.Method, r.URL, r.Proto)
	} else {
		p.log.Debug().Msgf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", r.Method, r.URL, r.Proto)
	}
	p.log.Debug().Msgf("CF-RAY: %s Request Headers %+v", cfRay, r.Header)
	p.log.Debug().Msgf("CF-RAY: %s Serving with ingress rule %v", cfRay, rule)

	if contentLen := r.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Request Content length unknown", cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Request content length %d", cfRay, contentLen)
	}
}

func (p *proxy) logOriginResponse(r *http.Response, cfRay string, lbProbe bool, rule interface{}) {
	responseByCode.WithLabelValues(strconv.Itoa(r.StatusCode)).Inc()
	if cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s Status: %s served by ingress %d", cfRay, r.Status, rule)
	} else if lbProbe {
		p.log.Debug().Msgf("Response to Load Balancer health check %s", r.Status)
	} else {
		p.log.Debug().Msgf("Status: %s served by ingress %v", r.Status, rule)
	}
	p.log.Debug().Msgf("CF-RAY: %s Response Headers %+v", cfRay, r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Response content length unknown", cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Response content length %d", cfRay, contentLen)
	}
}

func (p *proxy) logRequestError(err error, cfRay string, rule interface{}) {
	requestErrors.Inc()
	if cfRay != "" {
		p.log.Error().Msgf("CF-RAY: %s Proxying to ingress %v error: %v", cfRay, rule, err)
	} else {
		p.log.Error().Msgf("Proxying to ingress %v error: %v", rule, err)
	}
}

func findCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}
