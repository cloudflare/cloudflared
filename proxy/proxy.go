package proxy

import (
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
	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/stream"
	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// TagHeaderNamePrefix indicates a Cloudflared Warp Tag prefix that gets appended for warp traffic stream headers.
	TagHeaderNamePrefix   = "Cf-Warp-Tag-"
	LogFieldCFRay         = "cfRay"
	LogFieldLBProbe       = "lbProbe"
	LogFieldRule          = "ingressRule"
	LogFieldOriginService = "originService"
	LogFieldFlowID        = "flowID"
	LogFieldConnIndex     = "connIndex"
	LogFieldDestAddr      = "destAddr"

	trailerHeaderName = "Trailer"
)

// Proxy represents a means to Proxy between cloudflared and the origin services.
type Proxy struct {
	ingressRules ingress.Ingress
	warpRouting  *ingress.WarpRoutingService
	management   *ingress.ManagementService
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

func (p *Proxy) applyIngressMiddleware(rule *ingress.Rule, r *http.Request, w connection.ResponseWriter) (error, bool) {
	for _, handler := range rule.Handlers {
		result, err := handler.Handle(r.Context(), r)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("error while processing middleware handler %s", handler.Name())), false
		}

		if result.ShouldFilterRequest {
			w.WriteRespHeaders(result.StatusCode, nil)
			return fmt.Errorf("request filtered by middleware handler (%s) due to: %s", handler.Name(), result.Reason), true
		}
	}
	return nil, true
}

// ProxyHTTP further depends on ingress rules to establish a connection with the origin service. This may be
// a simple roundtrip or a tcp/websocket dial depending on ingres rule setup.
func (p *Proxy) ProxyHTTP(
	w connection.ResponseWriter,
	tr *tracing.TracedHTTPRequest,
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
		cfRay:     cfRay,
		lbProbe:   lbProbe,
		rule:      ruleNum,
		connIndex: tr.ConnIndex,
	}
	p.logRequest(req, logFields)
	ruleSpan.SetAttributes(attribute.Int("rule-num", ruleNum))
	ruleSpan.End()
	if err, applied := p.applyIngressMiddleware(rule, req, w); err != nil {
		if applied {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, "", rule, srv)
			return nil
		}
		return err
	}

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
		if err := p.proxyStream(tr.ToTracedContext(), rws, dest, originProxy); err != nil {
			rule, srv := ruleField(p.ingressRules, ruleNum)
			p.logRequestError(err, cfRay, "", rule, srv)
			return err
		}
		return nil
	case ingress.HTTPLocalProxy:
		p.proxyLocalRequest(originProxy, w, req, isWebsocket)
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

	tracedCtx := tracing.NewTracedContext(serveCtx, req.CfTraceID, p.log)

	p.log.Debug().
		Str(LogFieldFlowID, req.FlowID).
		Str(LogFieldDestAddr, req.Dest).
		Uint8(LogFieldConnIndex, req.ConnIndex).
		Msg("tcp proxy stream started")

	if err := p.proxyStream(tracedCtx, rwa, req.Dest, p.warpRouting.Proxy); err != nil {
		p.logRequestError(err, req.CFRay, req.FlowID, "", ingress.ServiceWarpRouting)
		return err
	}

	p.log.Debug().
		Str(LogFieldFlowID, req.FlowID).
		Str(LogFieldDestAddr, req.Dest).
		Uint8(LogFieldConnIndex, req.ConnIndex).
		Msg("tcp proxy stream finished successfully")

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
	tr *tracing.TracedHTTPRequest,
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

	headers := make(http.Header, len(resp.Header))
	// copy headers
	for k, v := range resp.Header {
		headers[k] = v
	}

	// Add spans to response header (if available)
	tr.AddSpans(headers)

	err = w.WriteRespHeaders(resp.StatusCode, headers)
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

		stream.Pipe(eyeballStream, rwc, p.log)
		return nil
	}

	if _, err = cfio.Copy(w, resp.Body); err != nil {
		return err
	}

	// copy trailers
	copyTrailers(w, resp)

	p.logOriginResponse(resp, fields)
	return nil
}

// proxyStream proxies type TCP and other underlying types if the connection is defined as a stream oriented
// ingress rule.
func (p *Proxy) proxyStream(
	tr *tracing.TracedContext,
	rwa connection.ReadWriteAcker,
	dest string,
	connectionProxy ingress.StreamBasedOriginProxy,
) error {
	ctx := tr.Context
	_, connectSpan := tr.Tracer().Start(ctx, "stream-connect")
	originConn, err := connectionProxy.EstablishConnection(ctx, dest)
	if err != nil {
		tracing.EndWithErrorStatus(connectSpan, err)
		return err
	}
	connectSpan.End()
	defer originConn.Close()

	encodedSpans := tr.GetSpans()

	if err := rwa.AckConnection(encodedSpans); err != nil {
		return err
	}

	originConn.Stream(ctx, rwa, p.log)
	return nil
}

func (p *Proxy) proxyLocalRequest(proxy ingress.HTTPLocalProxy, w connection.ResponseWriter, req *http.Request, isWebsocket bool) {
	if isWebsocket {
		// These headers are added since they are stripped off during an eyeball request to origintunneld, but they
		// are required during the Handshake process of a WebSocket request.
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-Websocket-Version", "13")
	}
	proxy.ServeHTTP(w, req)
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

func (p *Proxy) appendTagHeaders(r *http.Request) {
	for _, tag := range p.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

type logFields struct {
	cfRay     string
	lbProbe   bool
	rule      int
	flowID    string
	connIndex uint8
}

func copyTrailers(w connection.ResponseWriter, response *http.Response) {
	for trailerHeader, trailerValues := range response.Trailer {
		for _, trailerValue := range trailerValues {
			w.AddTrailer(trailerHeader, trailerValue)
		}
	}
}

func (p *Proxy) logRequest(r *http.Request, fields logFields) {
	log := p.log.With().Int(management.EventTypeKey, int(management.HTTP)).Logger()
	event := log.Debug()
	if fields.cfRay != "" {
		event = event.Str(LogFieldCFRay, fields.cfRay)
	}
	if fields.lbProbe {
		event = event.Bool(LogFieldLBProbe, fields.lbProbe)
	}
	if fields.cfRay == "" && !fields.lbProbe {
		log.Debug().Msgf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", r.Method, r.URL, r.Proto)
	}
	event.
		Uint8(LogFieldConnIndex, fields.connIndex).
		Str("host", r.Host).
		Str("path", r.URL.Path).
		Interface(LogFieldRule, fields.rule).
		Interface("headers", r.Header).
		Int64("content-length", r.ContentLength).
		Msgf("%s %s %s", r.Method, r.URL, r.Proto)
}

func (p *Proxy) logOriginResponse(resp *http.Response, fields logFields) {
	responseByCode.WithLabelValues(strconv.Itoa(resp.StatusCode)).Inc()
	event := p.log.Debug()
	if fields.cfRay != "" {
		event = event.Str(LogFieldCFRay, fields.cfRay)
	}
	if fields.lbProbe {
		event = event.Bool(LogFieldLBProbe, fields.lbProbe)
	}
	event.
		Int(management.EventTypeKey, int(management.HTTP)).
		Uint8(LogFieldConnIndex, fields.connIndex).
		Int64("content-length", resp.ContentLength).
		Msgf("%s", resp.Status)
}

func (p *Proxy) logRequestError(err error, cfRay string, flowID string, rule, service string) {
	requestErrors.Inc()
	log := p.log.Error().Err(err)
	if cfRay != "" {
		log = log.Str(LogFieldCFRay, cfRay)
	}
	if flowID != "" {
		log = log.Str(LogFieldFlowID, flowID).Int(management.EventTypeKey, int(management.TCP))
	} else {
		log = log.Int(management.EventTypeKey, int(management.HTTP))
	}
	if rule != "" {
		log = log.Str(LogFieldRule, rule)
	}
	if service != "" {
		log = log.Str(LogFieldOriginService, service)
	}
	log.Send()
}

func getDestFromRule(rule *ingress.Rule, req *http.Request) (string, error) {
	switch rule.Service.String() {
	case ingress.ServiceBastion:
		return carrier.ResolveBastionDest(req)
	default:
		return rule.Service.String(), nil
	}
}
