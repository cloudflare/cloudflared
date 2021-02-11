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
			err := errors.New(`cloudflared received a request from Warp client, but your configuration has disabled ingress from Warp clients. To enable this, set "warp-routing:\n\t enabled: true" in your config.yaml`)
			p.log.Error().Msg(err.Error())
			return err
		}
		logFields := logFields{
			cfRay:   cfRay,
			lbProbe: lbProbe,
			rule:    ingress.ServiceWarpRouting,
		}
		if err := p.proxyStreamRequest(serveCtx, w, req, sourceConnectionType, p.warpRouting.Proxy, logFields); err != nil {
			p.logRequestError(err, cfRay, ingress.ServiceWarpRouting)
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

	if sourceConnectionType == connection.TypeHTTP {
		if err := p.proxyHTTPRequest(w, req, rule, logFields); err != nil {
			p.logRequestError(err, cfRay, ruleNum)
			return err
		}
		return nil
	}

	connectionProxy, ok := rule.Service.(ingress.StreamBasedOriginProxy)
	if !ok {
		p.log.Error().Msgf("%s is not a connection-oriented service", rule.Service)
		return fmt.Errorf("Not a connection-oriented service")
	}

	if err := p.proxyStreamRequest(serveCtx, w, req, sourceConnectionType, connectionProxy, logFields); err != nil {
		p.logRequestError(err, cfRay, ruleNum)
		return err
	}
	return nil
}

func (p *proxy) proxyHTTPRequest(w connection.ResponseWriter, req *http.Request, rule *ingress.Rule, fields logFields) error {
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

	httpService, ok := rule.Service.(ingress.HTTPOriginProxy)
	if !ok {
		p.log.Error().Msgf("%s is not a http service", rule.Service)
		return fmt.Errorf("Not a http service")
	}

	resp, err := httpService.RoundTrip(req)
	if err != nil {
		return errors.Wrap(err, "Error proxying request to origin")
	}
	defer resp.Body.Close()

	err = w.WriteRespHeaders(resp.StatusCode, resp.Header)
	if err != nil {
		return errors.Wrap(err, "Error writing response header")
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
	req *http.Request,
	sourceConnectionType connection.Type,
	connectionProxy ingress.StreamBasedOriginProxy,
	fields logFields,
) error {
	originConn, resp, err := connectionProxy.EstablishConnection(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
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
	p.log.Debug().Msgf("CF-RAY: %s Request Headers %+v", fields.cfRay, r.Header)
	p.log.Debug().Msgf("CF-RAY: %s Serving with ingress rule %v", fields.cfRay, fields.rule)

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
