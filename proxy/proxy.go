package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	cfdflow "github.com/cloudflare/cloudflared/flow"
	"github.com/cloudflare/cloudflared/management"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cfio"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/stream"
	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// TagHeaderNamePrefix indicates a Cloudflared Warp Tag prefix that gets appended for warp traffic stream headers.
	TagHeaderNamePrefix = "Cf-Warp-Tag-"
	trailerHeaderName   = "Trailer"
)

// Proxy represents a means to Proxy between cloudflared and the origin services.
type Proxy struct {
	ingressRules ingress.Ingress
	originDialer ingress.OriginTCPDialer
	tags         []pogs.Tag
	flowLimiter  cfdflow.Limiter
	log          *zerolog.Logger
}

// NewOriginProxy returns a new instance of the Proxy struct.
func NewOriginProxy(
	ingressRules ingress.Ingress,
	originDialer ingress.OriginDialer,
	tags []pogs.Tag,
	flowLimiter cfdflow.Limiter,
	log *zerolog.Logger,
) *Proxy {
	proxy := &Proxy{
		ingressRules: ingressRules,
		originDialer: originDialer,
		tags:         tags,
		flowLimiter:  flowLimiter,
		log:          log,
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
			_ = w.WriteRespHeaders(result.StatusCode, nil)
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
	p.appendTagHeaders(req)

	_, ruleSpan := tr.Tracer().Start(req.Context(), "ingress_match",
		trace.WithAttributes(attribute.String("req-host", req.Host)))
	rule, ruleNum := p.ingressRules.FindMatchingRule(req.Host, req.URL.Path)
	ruleSpan.SetAttributes(attribute.Int("rule-num", ruleNum))
	ruleSpan.End()
	logger := newHTTPLogger(p.log, tr.ConnIndex, req, ruleNum, rule.Service.String())
	logHTTPRequest(&logger, req)
	if err, applied := p.applyIngressMiddleware(rule, req, w); err != nil {
		if applied {
			logRequestError(&logger, err)
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
			&logger,
		); err != nil {
			logRequestError(&logger, err)
			return err
		}
		return nil
	case ingress.StreamBasedOriginProxy:
		dest, err := getDestFromRule(rule, req)
		if err != nil {
			return err
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			return fmt.Errorf("response writer is not a flusher")
		}
		rws := connection.NewHTTPResponseReadWriterAcker(w, flusher, req)
		logger := logger.With().Str(logFieldDestAddr, dest).Logger()
		if err := p.proxyStream(tr.ToTracedContext(), rws, dest, originProxy, &logger); err != nil {
			logRequestError(&logger, err)
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
	conn connection.ReadWriteAcker,
	req *connection.TCPRequest,
) error {
	incrementTCPRequests()
	defer decrementTCPConcurrentRequests()

	logger := newTCPLogger(p.log, req)

	// Try to start a new flow
	if err := p.flowLimiter.Acquire(management.TCP.String()); err != nil {
		logger.Warn().Msg("Too many concurrent flows being handled, rejecting tcp proxy")
		return errors.Wrap(err, "failed to start tcp flow due to rate limiting")
	}
	defer p.flowLimiter.Release()

	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tracedCtx := tracing.NewTracedContext(serveCtx, req.CfTraceID, &logger)
	logger.Debug().Msg("tcp proxy stream started")

	// Parse the destination into a netip.AddrPort
	dest, err := netip.ParseAddrPort(req.Dest)
	if err != nil {
		logRequestError(&logger, err)
		return err
	}

	if err := p.proxyTCPStream(tracedCtx, conn, dest, p.originDialer, &logger); err != nil {
		logRequestError(&logger, err)
		return err
	}

	logger.Debug().Msg("tcp proxy stream finished successfully")

	return nil
}

// ProxyHTTPRequest proxies requests of underlying type http and websocket to the origin service.
func (p *Proxy) proxyHTTPRequest(
	w connection.ResponseWriter,
	tr *tracing.TracedHTTPRequest,
	httpService ingress.HTTPOriginProxy,
	isWebsocket bool,
	disableChunkedEncoding bool,
	logger *zerolog.Logger,
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

		stream.Pipe(eyeballStream, rwc, logger)
		return nil
	}

	if _, err = cfio.Copy(w, resp.Body); err != nil {
		return err
	}

	// copy trailers
	copyTrailers(w, resp)

	logOriginHTTPResponse(logger, resp)
	return nil
}

// proxyStream proxies type TCP and other underlying types if the connection is defined as a stream oriented
// ingress rule.
// connectedLogger is used to log when the connection is acknowledged
func (p *Proxy) proxyStream(
	tr *tracing.TracedContext,
	rwa connection.ReadWriteAcker,
	dest string,
	originDialer ingress.StreamBasedOriginProxy,
	logger *zerolog.Logger,
) error {
	ctx := tr.Context
	_, connectSpan := tr.Tracer().Start(ctx, "stream-connect")

	start := time.Now()
	originConn, err := originDialer.EstablishConnection(ctx, dest, logger)
	if err != nil {
		connectStreamErrors.Inc()
		tracing.EndWithErrorStatus(connectSpan, err)
		return err
	}
	connectSpan.End()
	defer originConn.Close()
	logger.Debug().Msg("origin connection established")

	encodedSpans := tr.GetSpans()

	if err := rwa.AckConnection(encodedSpans); err != nil {
		connectStreamErrors.Inc()
		return err
	}

	connectLatency.Observe(float64(time.Since(start).Milliseconds()))
	logger.Debug().Msg("proxy stream acknowledged")

	originConn.Stream(ctx, rwa, logger)
	return nil
}

// proxyTCPStream proxies private network type TCP connections as a stream towards an available origin.
//
// This is different than proxyStream because it's not leveraged ingress rule services and uses the
// originDialer from OriginDialerService.
func (p *Proxy) proxyTCPStream(
	tr *tracing.TracedContext,
	tunnelConn connection.ReadWriteAcker,
	dest netip.AddrPort,
	originDialer ingress.OriginTCPDialer,
	logger *zerolog.Logger,
) error {
	ctx := tr.Context
	_, connectSpan := tr.Tracer().Start(ctx, "stream-connect")

	start := time.Now()
	originConn, err := originDialer.DialTCP(ctx, dest)
	if err != nil {
		connectStreamErrors.Inc()
		tracing.EndWithErrorStatus(connectSpan, err)
		return err
	}
	connectSpan.End()
	defer originConn.Close()
	logger.Debug().Msg("origin connection established")

	encodedSpans := tr.GetSpans()

	if err := tunnelConn.AckConnection(encodedSpans); err != nil {
		connectStreamErrors.Inc()
		return err
	}

	connectLatency.Observe(float64(time.Since(start).Milliseconds()))
	logger.Debug().Msg("proxy stream acknowledged")

	stream.Pipe(tunnelConn, originConn, logger)
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

func copyTrailers(w connection.ResponseWriter, response *http.Response) {
	for trailerHeader, trailerValues := range response.Trailer {
		for _, trailerValue := range trailerValues {
			w.AddTrailer(trailerHeader, trailerValue)
		}
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
