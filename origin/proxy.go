package origin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	TagHeaderNamePrefix   = "Cf-Warp-Tag-"
	LogFieldCFRay         = "cfRay"
	LogFieldRule          = "ingressRule"
	LogFieldOriginService = "originService"
)

type proxy struct {
	ingressRules ingress.Ingress
	warpRouting  *ingress.WarpRoutingService
	tags         []tunnelpogs.Tag
	log          *zerolog.Logger
	bufferPool   *bufferPool
}

var switchingProtocolText = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))

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
		bufferPool:   newBufferPool(512 * 1024),
	}
}

// Caller is responsible for writing any error to ResponseWriter
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
			err := errors.New(`cloudflared received a request from WARP client, but your configuration has disabled ingress from WARP clients. To enable this, set "warp-routing:\n\t enabled: true" in your config.yaml`)
			p.log.Error().Msg(err.Error())
			return err
		}
		logFields := logFields{
			cfRay:   cfRay,
			lbProbe: lbProbe,
			rule:    ingress.ServiceWarpRouting,
		}

		host, err := getRequestHost(req)
		if err != nil {
			err = fmt.Errorf(`cloudflared recieved a warp-routing request with an empty host value: %v`, err)
			return err
		}
		if err := p.proxyStreamRequest(serveCtx, w, host, req, p.warpRouting.Proxy, logFields); err != nil {
			p.logRequestError(err, cfRay, "", ingress.ServiceWarpRouting)
			return err
		}
		return nil
	}

	rule, ruleNum := p.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	logFields := logFields{
		cfRay:   cfRay,
		lbProbe: lbProbe,
		rule:    ruleNum,
	}
	p.logRequest(req, logFields)

	switch originProxy := rule.Service.(type) {
	case ingress.HTTPOriginProxy:
		if err := p.proxyHTTPRequest(w, req, originProxy, sourceConnectionType == connection.TypeWebsocket,
			rule.Config.DisableChunkedEncoding, logFields); err != nil {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, rule, srv)
			return err
		}
		return nil

	case ingress.StreamBasedOriginProxy:
		dest, err := getDestFromRule(rule, req)
		if err != nil {
			return err
		}
		if err := p.proxyStreamRequest(serveCtx, w, dest, req, originProxy, logFields); err != nil {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, rule, srv)
			return err
		}
		return nil
	default:
		return fmt.Errorf("Unrecognized service: %s, %t", rule.Service, originProxy)
	}
}

func getDestFromRule(rule *ingress.Rule, req *http.Request) (string, error) {
	switch rule.Service.String() {
	case ingress.ServiceBastion:
		return carrier.ResolveBastionDest(req)
	default:
		return rule.Service.String(), nil
	}
}

// getRequestHost returns the host of the http.Request.
func getRequestHost(r *http.Request) (string, error) {
	if r.Host != "" {
		return r.Host, nil
	}
	if r.URL != nil {
		return r.URL.Host, nil
	}
	return "", errors.New("host not set in incoming request")
}

func ruleField(ing ingress.Ingress, ruleNum int) (ruleID string, srv string) {
	srv = ing.Rules[ruleNum].Service.String()
	if ing.IsSingleRule() {
		return "", srv
	}
	return fmt.Sprintf("%d", ruleNum), srv
}

func (p *proxy) proxyHTTPRequest(
	w connection.ResponseWriter,
	req *http.Request,
	httpService ingress.HTTPOriginProxy,
	isWebsocket bool,
	disableChunkedEncoding bool,
	fields logFields) error {
	roundTripReq := req
	if isWebsocket {
		roundTripReq = req.Clone(req.Context())
		roundTripReq.Header.Set("Connection", "Upgrade")
		roundTripReq.Header.Set("Upgrade", "websocket")
		roundTripReq.Header.Set("Sec-Websocket-Version", "13")
		roundTripReq.ContentLength = 0
		roundTripReq.Body = nil
	} else {
		// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
		if disableChunkedEncoding {
			roundTripReq.TransferEncoding = []string{"gzip", "deflate"}
			cLength, err := strconv.Atoi(req.Header.Get("Content-Length"))
			if err == nil {
				roundTripReq.ContentLength = int64(cLength)
			}
		}
		// Request origin to keep connection alive to improve performance
		roundTripReq.Header.Set("Connection", "keep-alive")
	}

	resp, err := httpService.RoundTrip(roundTripReq)
	if err != nil {
		return errors.Wrap(err, "Unable to reach the origin service. The service may be down or it may not be responding to traffic from cloudflared")
	}
	defer resp.Body.Close()

	err = w.WriteRespHeaders(resp.StatusCode, resp.Header)
	if err != nil {
		return errors.Wrap(err, "Error writing response header")
	}

	if resp.StatusCode == http.StatusSwitchingProtocols {
		rwc, ok := resp.Body.(io.ReadWriteCloser)
		if !ok {
			return errors.New("internal error: unsupported connection type")
		}
		defer rwc.Close()

		eyeballStream := &bidirectionalStream{
			writer: w,
			reader: req.Body,
		}

		websocket.Stream(eyeballStream, rwc, p.log)
		return nil
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
	p.logOriginResponse(resp, fields)
	return nil
}

// proxyStreamRequest first establish a connection with origin, then it writes the status code and headers, and finally it streams data between
// eyeball and origin.
func (p *proxy) proxyStreamRequest(
	serveCtx context.Context,
	w connection.ResponseWriter,
	dest string,
	req *http.Request,
	connectionProxy ingress.StreamBasedOriginProxy,
	fields logFields,
) error {
	originConn, err := connectionProxy.EstablishConnection(dest)
	if err != nil {
		return err
	}

	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		ContentLength: -1,
	}

	if secWebsocketKey := req.Header.Get("Sec-WebSocket-Key"); secWebsocketKey != "" {
		resp.Header = websocket.NewResponseHeader(req)
	}

	if err = w.WriteRespHeaders(resp.StatusCode, resp.Header); err != nil {
		return err
	}

	streamCtx, cancel := context.WithCancel(serveCtx)
	defer cancel()

	go func() {
		// streamCtx is done if req is cancelled or if Stream returns
		<-streamCtx.Done()
		originConn.Close()
	}()

	eyeballStream := &bidirectionalStream{
		writer: w,
		reader: req.Body,
	}
	originConn.Stream(serveCtx, eyeballStream, p.log)
	p.logOriginResponse(resp, fields)
	return nil
}

type bidirectionalStream struct {
	reader io.Reader
	writer io.Writer
}

func (wr *bidirectionalStream) Read(p []byte) (n int, err error) {
	return wr.reader.Read(p)
}

func (wr *bidirectionalStream) Write(p []byte) (n int, err error) {
	return wr.writer.Write(p)
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

type logFields struct {
	cfRay   string
	lbProbe bool
	rule    interface{}
}

func (p *proxy) logRequest(r *http.Request, fields logFields) {
	if fields.cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s %s %s %s", fields.cfRay, r.Method, r.URL, r.Proto)
	} else if fields.lbProbe {
		p.log.Debug().Msgf("CF-RAY: %s Load Balancer health check %s %s %s", fields.cfRay, r.Method, r.URL, r.Proto)
	} else {
		p.log.Debug().Msgf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", r.Method, r.URL, r.Proto)
	}
	p.log.Debug().
		Str("CF-RAY", fields.cfRay).
		Str("Header", fmt.Sprintf("%+v", r.Header)).
		Str("host", r.Host).
		Str("path", r.URL.Path).
		Interface("rule", fields.rule).
		Msg("Inbound request")

	if contentLen := r.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Request Content length unknown", fields.cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Request content length %d", fields.cfRay, contentLen)
	}
}

func (p *proxy) logOriginResponse(resp *http.Response, fields logFields) {
	responseByCode.WithLabelValues(strconv.Itoa(resp.StatusCode)).Inc()
	if fields.cfRay != "" {
		p.log.Debug().Msgf("CF-RAY: %s Status: %s served by ingress %d", fields.cfRay, resp.Status, fields.rule)
	} else if fields.lbProbe {
		p.log.Debug().Msgf("Response to Load Balancer health check %s", resp.Status)
	} else {
		p.log.Debug().Msgf("Status: %s served by ingress %v", resp.Status, fields.rule)
	}
	p.log.Debug().Msgf("CF-RAY: %s Response Headers %+v", fields.cfRay, resp.Header)

	if contentLen := resp.ContentLength; contentLen == -1 {
		p.log.Debug().Msgf("CF-RAY: %s Response content length unknown", fields.cfRay)
	} else {
		p.log.Debug().Msgf("CF-RAY: %s Response content length %d", fields.cfRay, contentLen)
	}
}

func (p *proxy) logRequestError(err error, cfRay string, rule, service string) {
	requestErrors.Inc()
	log := p.log.Error().Err(err)
	if cfRay != "" {
		log = log.Str(LogFieldCFRay, cfRay)
	}
	if rule != "" {
		log = log.Str(LogFieldRule, rule)
	}
	if service != "" {
		log = log.Str(LogFieldOriginService, service)
	}
	log.Msg("")
}

func findCfRayHeader(req *http.Request) string {
	return req.Header.Get("Cf-Ray")
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}
