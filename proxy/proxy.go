package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cfio"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	// TagHeaderNamePrefix indicates a Cloudflared Warp Tag prefix that gets appended for warp traffic stream headers.
	TagHeaderNamePrefix   = "Cf-Warp-Tag-"
	LogFieldCFRay         = "cfRay"
	LogFieldRule          = "ingressRule"
	LogFieldOriginService = "originService"
	LogFieldFlowID        = "flowID"
)

// Proxy represents a means to Proxy between cloudflared and the origin services.
type Proxy struct {
	ingressRules ingress.Ingress
	warpRouting  *ingress.WarpRoutingService
	tags         []tunnelpogs.Tag
	log          *zerolog.Logger
}

// NewOriginProxy returns a new instance of the Proxy struct.
func NewOriginProxy(
	ingressRules ingress.Ingress,
	warpRouting ingress.WarpRoutingConfig,
	tags []tunnelpogs.Tag,
	log *zerolog.Logger,
) *Proxy {
	proxy := &Proxy{
		ingressRules: ingressRules,
		tags:         tags,
		log:          log,
	}
	if warpRouting.Enabled {
		proxy.warpRouting = ingress.NewWarpRoutingService(warpRouting)
		log.Info().Msgf("Warp-routing is enabled")
	}

	return proxy
}

// ProxyHTTP further depends on ingress rules to establish a connection with the origin service. This may be
// a simple roundtrip or a tcp/websocket dial depending on ingres rule setup.
func (p *Proxy) ProxyHTTP(
	w connection.ResponseWriter,
	tr *tracing.TracedRequest,
	isWebsocket bool,
) error {
	incrementRequests()
	defer decrementConcurrentRequests()

	req := tr.Request
	cfRay := connection.FindCfRayHeader(req)
	lbProbe := connection.IsLBProbeRequest(req)
	p.appendTagHeaders(req)

	_, ruleSpan := tr.Tracer().Start(req.Context(), "ingress_match",
		trace.WithAttributes(attribute.String("req-host", req.Host)))
	rule, ruleNum := p.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	logFields := logFields{
		cfRay:   cfRay,
		lbProbe: lbProbe,
		rule:    ruleNum,
	}
	p.logRequest(req, logFields)
	ruleSpan.SetAttributes(attribute.Int("rule-num", ruleNum))
	ruleSpan.End()

	switch originProxy := rule.Service.(type) {
	case ingress.HTTPOriginProxy:
		if err := p.proxyHTTPRequest(
			w,
			tr,
			originProxy,
			isWebsocket,
			rule.Config.DisableChunkedEncoding,
			logFields,
		); err != nil {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, "", rule, srv)
			return err
		}
		return nil
	case ingress.StreamBasedOriginProxy:
		dest, err := getDestFromRule(rule, req)
		if err != nil {
			return err
		}

		rws := connection.NewHTTPResponseReadWriterAcker(w, req)
		if err := p.proxyStream(req.Context(), rws, dest, originProxy); err != nil {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, "", rule, srv)
			return err
		}
		return nil
	default:
		return fmt.Errorf("Unrecognized service: %s, %t", rule.Service, originProxy)
	}
}

// ProxyTCP proxies to a TCP connection between the origin service and cloudflared.
func (p *Proxy) ProxyTCP(
	ctx context.Context,
	rwa connection.ReadWriteAcker,
	req *connection.TCPRequest,
) error {
	incrementRequests()
	defer decrementConcurrentRequests()

	if p.warpRouting == nil {
		err := errors.New(`cloudflared received a request from WARP client, but your configuration has disabled ingress from WARP clients. To enable this, set "warp-routing:\n\t enabled: true" in your config.yaml`)
		p.log.Error().Msg(err.Error())
		return err
	}

	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	p.log.Debug().Str(LogFieldFlowID, req.FlowID).Msg("tcp proxy stream started")

	if err := p.proxyStream(serveCtx, rwa, req.Dest, p.warpRouting.Proxy); err != nil {
		p.logRequestError(err, req.CFRay, req.FlowID, "", ingress.ServiceWarpRouting)
		return err
	}

	p.log.Debug().Str(LogFieldFlowID, req.FlowID).Msg("tcp proxy stream finished successfully")

	return nil
}

func ruleField(ing ingress.Ingress, ruleNum int) (ruleID string, srv string) {
	srv = ing.Rules[ruleNum].Service.String()
	if ing.IsSingleRule() {
		return "", srv
	}
	return fmt.Sprintf("%d", ruleNum), srv
}

// ProxyHTTPRequest proxies requests of underlying type http and websocket to the origin service.
func (p *Proxy) proxyHTTPRequest(
	w connection.ResponseWriter,
	tr *tracing.TracedRequest,
	httpService ingress.HTTPOriginProxy,
	isWebsocket bool,
	disableChunkedEncoding bool,
	fields logFields,
) error {
	roundTripReq := tr.Request
	if isWebsocket {
		roundTripReq = tr.Clone(tr.Request.Context())
		roundTripReq.Header.Set("Connection", "Upgrade")
		roundTripReq.Header.Set("Upgrade", "websocket")
		roundTripReq.Header.Set("Sec-Websocket-Version", "13")
		roundTripReq.ContentLength = 0
		roundTripReq.Body = nil
	} else {
		// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
		if disableChunkedEncoding {
			roundTripReq.TransferEncoding = []string{"gzip", "deflate"}
			cLength, err := strconv.Atoi(tr.Request.Header.Get("Content-Length"))
			if err == nil {
				roundTripReq.ContentLength = int64(cLength)
			}
		}
		// Request origin to keep connection alive to improve performance
		roundTripReq.Header.Set("Connection", "keep-alive")
	}

	// Set the User-Agent as an empty string if not provided to avoid inserting golang default UA
	if roundTripReq.Header.Get("User-Agent") == "" {
		roundTripReq.Header.Set("User-Agent", "")
	}

	_, ttfbSpan := tr.Tracer().Start(tr.Context(), "ttfb_origin")
	resp, err := httpService.RoundTrip(roundTripReq)
	if err != nil {
		tracing.EndWithErrorStatus(ttfbSpan, err)
		if err := roundTripReq.Context().Err(); err != nil {
			return errors.Wrap(err, "Incoming request ended abruptly")
		}
		return errors.Wrap(err, "Unable to reach the origin service. The service may be down or it may not be responding to traffic from cloudflared")
	}

	tracing.EndWithStatusCode(ttfbSpan, resp.StatusCode)
	defer resp.Body.Close()

	// resp headers can be nil
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}

	// Add spans to response header (if available)
	tr.AddSpans(resp.Header, p.log)

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
			reader: tr.Request.Body,
		}

		websocket.Stream(eyeballStream, rwc, p.log)
		return nil
	}

	if connection.IsServerSentEvent(resp.Header) {
		p.log.Debug().Msg("Detected Server-Side Events from Origin")
		p.writeEventStream(w, resp.Body)
	} else {
		_, _ = cfio.Copy(w, resp.Body)
	}

	p.logOriginResponse(resp, fields)
	return nil
}

// proxyStream proxies type TCP and other underlying types if the connection is defined as a stream oriented
// ingress rule.
func (p *Proxy) proxyStream(
	ctx context.Context,
	rwa connection.ReadWriteAcker,
	dest string,
	connectionProxy ingress.StreamBasedOriginProxy,
) error {
	originConn, err := connectionProxy.EstablishConnection(ctx, dest)
	if err != nil {
		return err
	}

	if err := rwa.AckConnection(); err != nil {
		return err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		// streamCtx is done if req is cancelled or if Stream returns
		<-streamCtx.Done()
		originConn.Close()
	}()

	originConn.Stream(ctx, rwa, p.log)
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

func (p *Proxy) writeEventStream(w connection.ResponseWriter, respBody io.ReadCloser) {
	reader := bufio.NewReader(respBody)
	for {
		line, readErr := reader.ReadBytes('\n')

		// We first try to write whatever we read even if an error occurred
		// The reason for doing it is to guarantee we really push everything to the eyeball side
		// before returning
		if len(line) > 0 {
			if _, writeErr := w.Write(line); writeErr != nil {
				return
			}
		}

		if readErr != nil {
			return
		}
	}
}

func (p *Proxy) appendTagHeaders(r *http.Request) {
	for _, tag := range p.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

type logFields struct {
	cfRay   string
	lbProbe bool
	rule    interface{}
	flowID  string
}

func (p *Proxy) logRequest(r *http.Request, fields logFields) {
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

func (p *Proxy) logOriginResponse(resp *http.Response, fields logFields) {
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

func (p *Proxy) logRequestError(err error, cfRay string, flowID string, rule, service string) {
	requestErrors.Inc()
	log := p.log.Error().Err(err)
	if cfRay != "" {
		log = log.Str(LogFieldCFRay, cfRay)
	}
	if flowID != "" {
		log = log.Str(LogFieldFlowID, flowID)
	}
	if rule != "" {
		log = log.Str(LogFieldRule, rule)
	}
	if service != "" {
		log = log.Str(LogFieldOriginService, service)
	}
	log.Msg("")
}

func getDestFromRule(rule *ingress.Rule, req *http.Request) (string, error) {
	switch rule.Service.String() {
	case ingress.ServiceBastion:
		return carrier.ResolveBastionDest(req)
	default:
		return rule.Service.String(), nil
	}
}
