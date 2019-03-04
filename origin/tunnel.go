package origin

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/cloudflare/cloudflared/websocket"

	raven "github.com/getsentry/raven-go"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	_ "github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	rpc "zombiezen.com/go/capnproto2/rpc"
)

const (
	dialTimeout              = 15 * time.Second
	lbProbeUserAgentPrefix   = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
	TagHeaderNamePrefix      = "Cf-Warp-Tag-"
	DuplicateConnectionError = "EDUPCONN"
)

type TunnelConfig struct {
	EdgeAddrs          []string
	OriginUrl          string
	Hostname           string
	OriginCert         []byte
	TlsConfig          *tls.Config
	ClientTlsConfig    *tls.Config
	Retries            uint
	HeartbeatInterval  time.Duration
	MaxHeartbeats      uint64
	ClientID           string
	BuildInfo          *BuildInfo
	ReportedVersion    string
	LBPool             string
	Tags               []tunnelpogs.Tag
	HAConnections      int
	HTTPTransport      http.RoundTripper
	Metrics            *TunnelMetrics
	MetricsUpdateFreq  time.Duration
	TransportLogger    *log.Logger
	Logger             *log.Logger
	IsAutoupdated      bool
	GracePeriod        time.Duration
	RunFromTerminal    bool
	NoChunkedEncoding  bool
	WSGI               bool
	CompressionQuality uint64
	IncidentLookup     IncidentLookup
	CloseConnOnce      *sync.Once // Used to close connectedSignal no more than once
}

type dialError struct {
	cause error
}

func (e dialError) Error() string {
	return e.cause.Error()
}

type dupConnRegisterTunnelError struct{}

func (e dupConnRegisterTunnelError) Error() string {
	return "already connected to this server"
}

type muxerShutdownError struct{}

func (e muxerShutdownError) Error() string {
	return "muxer shutdown"
}

// RegisterTunnel error from server
type serverRegisterTunnelError struct {
	cause     error
	permanent bool
}

func (e serverRegisterTunnelError) Error() string {
	return e.cause.Error()
}

// RegisterTunnel error from client
type clientRegisterTunnelError struct {
	cause error
}

func (e clientRegisterTunnelError) Error() string {
	return e.cause.Error()
}

func (c *TunnelConfig) RegistrationOptions(connectionID uint8, OriginLocalIP string, uuid uuid.UUID) *tunnelpogs.RegistrationOptions {
	policy := tunnelrpc.ExistingTunnelPolicy_balance
	if c.HAConnections <= 1 && c.LBPool == "" {
		policy = tunnelrpc.ExistingTunnelPolicy_disconnect
	}
	return &tunnelpogs.RegistrationOptions{
		ClientID:             c.ClientID,
		Version:              c.ReportedVersion,
		OS:                   fmt.Sprintf("%s_%s", c.BuildInfo.GoOS, c.BuildInfo.GoArch),
		ExistingTunnelPolicy: policy,
		PoolName:             c.LBPool,
		Tags:                 c.Tags,
		ConnectionID:         connectionID,
		OriginLocalIP:        OriginLocalIP,
		IsAutoupdated:        c.IsAutoupdated,
		RunFromTerminal:      c.RunFromTerminal,
		CompressionQuality:   c.CompressionQuality,
		UUID:                 uuid.String(),
	}
}

func StartTunnelDaemon(config *TunnelConfig, shutdownC <-chan struct{}, connectedSignal *signal.Signal) error {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-shutdownC
		cancel()
	}()

	u, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	// If a user specified negative HAConnections, we will treat it as requesting 1 connection
	if config.HAConnections > 1 {
		return NewSupervisor(config).Run(ctx, connectedSignal, u)
	} else {
		addrs, err := ResolveEdgeIPs(config.EdgeAddrs)
		if err != nil {
			return err
		}
		return ServeTunnelLoop(ctx, config, addrs[0], 0, connectedSignal, u)
	}
}

func ServeTunnelLoop(ctx context.Context,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connectionID uint8,
	connectedSignal *signal.Signal,
	u uuid.UUID,
) error {
	logger := config.Logger
	config.Metrics.incrementHaConnections()
	defer config.Metrics.decrementHaConnections()
	backoff := BackoffHandler{MaxRetries: config.Retries}
	connectedFuse := h2mux.NewBooleanFuse()
	go func() {
		if connectedFuse.Await() {
			connectedSignal.Notify()
		}
	}()
	// Ensure the above goroutine will terminate if we return without connecting
	defer connectedFuse.Fuse(false)
	for {
		err, recoverable := ServeTunnel(ctx, config, addr, connectionID, connectedFuse, &backoff, u)
		if recoverable {
			if duration, ok := backoff.GetBackoffDuration(ctx); ok {
				logger.Infof("Retrying in %s seconds", duration)
				backoff.Backoff(ctx)
				continue
			}
		}
		return err
	}
}

func ServeTunnel(
	ctx context.Context,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connectionID uint8,
	connectedFuse *h2mux.BooleanFuse,
	backoff *BackoffHandler,
	u uuid.UUID,
) (err error, recoverable bool) {
	// Treat panics as recoverable errors
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("ServeTunnel: %v", r)
			}
			recoverable = true
		}
	}()

	connectionTag := uint8ToString(connectionID)
	logger := config.Logger.WithField("connectionID", connectionTag)

	// additional tags to send other than hostname which is set in cloudflared main package
	tags := make(map[string]string)
	tags["ha"] = connectionTag

	// Returns error from parsing the origin URL or handshake errors
	handler, originLocalIP, err := NewTunnelHandler(ctx, config, addr.String(), connectionID)
	if err != nil {
		errLog := config.Logger.WithError(err)
		switch err.(type) {
		case dialError:
			errLog.Error("Unable to dial edge")
		case h2mux.MuxerHandshakeError:
			errLog.Error("Handshake failed with edge server")
		default:
			errLog.Error("Tunnel creation failure")
			return err, false
		}
		return err, true
	}

	errGroup, serveCtx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		err := RegisterTunnel(serveCtx, handler.muxer, config, connectionID, originLocalIP, u)
		if err == nil {
			connectedFuse.Fuse(true)
			backoff.SetGracePeriod()
		}
		return err
	})

	errGroup.Go(func() error {
		updateMetricsTickC := time.Tick(config.MetricsUpdateFreq)
		for {
			select {
			case <-serveCtx.Done():
				// UnregisterTunnel blocks until the RPC call returns
				err := UnregisterTunnel(handler.muxer, config.GracePeriod, config.TransportLogger)
				handler.muxer.Shutdown()
				return err
			case <-updateMetricsTickC:
				handler.UpdateMetrics(connectionTag)
			}
		}
	})

	errGroup.Go(func() error {
		// All routines should stop when muxer finish serving. When muxer is shutdown
		// gracefully, it doesn't return an error, so we need to return errMuxerShutdown
		// here to notify other routines to stop
		err := handler.muxer.Serve(serveCtx)
		if err == nil {
			return muxerShutdownError{}
		}
		return err
	})

	err = errGroup.Wait()
	if err != nil {
		switch castedErr := err.(type) {
		case dupConnRegisterTunnelError:
			logger.Info("Already connected to this server, selecting a different one")
			return err, true
		case serverRegisterTunnelError:
			logger.WithError(castedErr.cause).Error("Register tunnel error from server side")
			// Don't send registration error return from server to Sentry. They are
			// logged on server side
			if incidents := config.IncidentLookup.ActiveIncidents(); len(incidents) > 0 {
				logger.Error(activeIncidentsMsg(incidents))
			}
			return castedErr.cause, !castedErr.permanent
		case clientRegisterTunnelError:
			logger.WithError(castedErr.cause).Error("Register tunnel error on client side")
			raven.CaptureError(castedErr.cause, tags)
			return err, true
		case muxerShutdownError:
			logger.Infof("Muxer shutdown")
			return err, true
		default:
			logger.WithError(err).Error("Serve tunnel error")
			raven.CaptureError(err, tags)
			return err, true
		}
	}
	return nil, true
}

func IsRPCStreamResponse(headers []h2mux.Header) bool {
	if len(headers) != 1 {
		return false
	}
	if headers[0].Name != ":status" || headers[0].Value != "200" {
		return false
	}
	return true
}

func RegisterTunnel(
	ctx context.Context,
	muxer *h2mux.Muxer,
	config *TunnelConfig,
	connectionID uint8,
	originLocalIP string,
	uuid uuid.UUID,
) error {
	config.TransportLogger.Debug("initiating RPC stream to register")
	stream, err := muxer.OpenStream([]h2mux.Header{
		{Name: ":method", Value: "RPC"},
		{Name: ":scheme", Value: "capnp"},
		{Name: ":path", Value: "*"},
	}, nil)
	if err != nil {
		// RPC stream open error
		return clientRegisterTunnelError{cause: err}
	}
	if !IsRPCStreamResponse(stream.Headers) {
		// stream response error
		return clientRegisterTunnelError{cause: err}
	}
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(config.TransportLogger.WithField("subsystem", "rpc-register"), rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(config.TransportLogger.WithField("subsystem", "rpc-transport")),
	)
	defer conn.Close()
	ts := tunnelpogs.TunnelServer_PogsClient{Client: conn.Bootstrap(ctx)}
	// Request server info without blocking tunnel registration; must use capnp library directly.
	tsClient := tunnelrpc.TunnelServer{Client: ts.Client}
	serverInfoPromise := tsClient.GetServerInfo(ctx, func(tunnelrpc.TunnelServer_getServerInfo_Params) error {
		return nil
	})
	registration, err := ts.RegisterTunnel(
		ctx,
		config.OriginCert,
		config.Hostname,
		config.RegistrationOptions(connectionID, originLocalIP, uuid),
	)
	LogServerInfo(serverInfoPromise.Result(), connectionID, config.Metrics, config.Logger)
	if err != nil {
		// RegisterTunnel RPC failure
		return clientRegisterTunnelError{cause: err}
	}
	for _, logLine := range registration.LogLines {
		config.Logger.Info(logLine)
	}

	if regErr := processRegisterTunnelError(registration.Err, registration.PermanentFailure, config.Metrics); regErr != nil {
		return regErr
	}

	if registration.TunnelID != "" {
		config.Metrics.tunnelsHA.AddTunnelID(connectionID, registration.TunnelID)
		config.Logger.Infof("Each HA connection's tunnel IDs: %v", config.Metrics.tunnelsHA.String())
	}

	// Print out the user's trial zone URL in a nice box (if they requested and got one)
	if isTrialTunnel := config.Hostname == ""; isTrialTunnel {
		if url, err := url.Parse(registration.Url); err == nil {
			for _, line := range asciiBox(trialZoneMsg(url.String()), 2) {
				config.Logger.Infoln(line)
			}
		} else {
			config.Logger.Errorln("Failed to connect tunnel, please try again.")
			return fmt.Errorf("empty URL in response from Cloudflare edge")
		}
	}

	config.Logger.Infof("Route propagating, it may take up to 1 minute for your new route to become functional")
	return nil
}

func processRegisterTunnelError(err string, permanentFailure bool, metrics *TunnelMetrics) error {
	if err == "" {
		metrics.regSuccess.Inc()
		return nil
	}

	metrics.regFail.WithLabelValues(err).Inc()
	if err == DuplicateConnectionError {
		return dupConnRegisterTunnelError{}
	}
	return serverRegisterTunnelError{
		cause:     fmt.Errorf("Server error: %s", err),
		permanent: permanentFailure,
	}
}

func UnregisterTunnel(muxer *h2mux.Muxer, gracePeriod time.Duration, logger *log.Logger) error {
	logger.Debug("initiating RPC stream to unregister")
	stream, err := muxer.OpenStream([]h2mux.Header{
		{Name: ":method", Value: "RPC"},
		{Name: ":scheme", Value: "capnp"},
		{Name: ":path", Value: "*"},
	}, nil)
	if err != nil {
		// RPC stream open error
		return err
	}
	if !IsRPCStreamResponse(stream.Headers) {
		// stream response error
		return err
	}
	ctx := context.Background()
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger.WithField("subsystem", "rpc-unregister"), rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger.WithField("subsystem", "rpc-transport")),
	)
	defer conn.Close()
	ts := tunnelpogs.TunnelServer_PogsClient{Client: conn.Bootstrap(ctx)}
	// gracePeriod is encoded in int64 using capnproto
	return ts.UnregisterTunnel(ctx, gracePeriod.Nanoseconds())
}

func LogServerInfo(
	promise tunnelrpc.ServerInfo_Promise,
	connectionID uint8,
	metrics *TunnelMetrics,
	logger *log.Logger,
) {
	serverInfoMessage, err := promise.Struct()
	if err != nil {
		logger.WithError(err).Warn("Failed to retrieve server information")
		return
	}
	serverInfo, err := tunnelpogs.UnmarshalServerInfo(serverInfoMessage)
	if err != nil {
		logger.WithError(err).Warn("Failed to retrieve server information")
		return
	}
	logger.Infof("Connected to %s", serverInfo.LocationName)
	metrics.registerServerLocation(uint8ToString(connectionID), serverInfo.LocationName)
}

func H2RequestHeadersToH1Request(h2 []h2mux.Header, h1 *http.Request) error {
	for _, header := range h2 {
		switch header.Name {
		case ":method":
			h1.Method = header.Value
		case ":scheme":
		case ":authority":
			// Otherwise the host header will be based on the origin URL
			h1.Host = header.Value
		case ":path":
			u, err := url.Parse(header.Value)
			if err != nil {
				return fmt.Errorf("unparseable path")
			}
			resolved := h1.URL.ResolveReference(u)
			// prevent escaping base URL
			if !strings.HasPrefix(resolved.String(), h1.URL.String()) {
				return fmt.Errorf("invalid path")
			}
			h1.URL = resolved
		case "content-length":
			contentLength, err := strconv.ParseInt(header.Value, 10, 64)
			if err != nil {
				return fmt.Errorf("unparseable content length")
			}
			h1.ContentLength = contentLength
		default:
			h1.Header.Add(http.CanonicalHeaderKey(header.Name), header.Value)
		}
	}
	return nil
}

func H1ResponseToH2Response(h1 *http.Response) (h2 []h2mux.Header) {
	h2 = []h2mux.Header{{Name: ":status", Value: fmt.Sprintf("%d", h1.StatusCode)}}
	for headerName, headerValues := range h1.Header {
		for _, headerValue := range headerValues {
			h2 = append(h2, h2mux.Header{Name: strings.ToLower(headerName), Value: headerValue})
		}
	}
	return
}

func FindCfRayHeader(h1 *http.Request) string {
	return h1.Header.Get("Cf-Ray")
}

type TunnelHandler struct {
	originUrl  string
	muxer      *h2mux.Muxer
	httpClient http.RoundTripper
	tlsConfig  *tls.Config
	tags       []tunnelpogs.Tag
	metrics    *TunnelMetrics
	// connectionID is only used by metrics, and prometheus requires labels to be string
	connectionID      string
	logger            *log.Logger
	noChunkedEncoding bool
}

var dialer = net.Dialer{DualStack: true}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewTunnelHandler(ctx context.Context,
	config *TunnelConfig,
	addr string,
	connectionID uint8,
) (*TunnelHandler, string, error) {
	originURL, err := validation.ValidateUrl(config.OriginUrl)
	if err != nil {
		return nil, "", fmt.Errorf("unable to parse origin URL %#v", originURL)
	}
	h := &TunnelHandler{
		originUrl:         originURL,
		httpClient:        config.HTTPTransport,
		tlsConfig:         config.ClientTlsConfig,
		tags:              config.Tags,
		metrics:           config.Metrics,
		connectionID:      uint8ToString(connectionID),
		logger:            config.Logger,
		noChunkedEncoding: config.NoChunkedEncoding,
	}
	if h.httpClient == nil {
		h.httpClient = http.DefaultTransport
	}
	// Inherit from parent context so we can cancel (Ctrl-C) while dialing
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	// TUN-92: enforce a timeout on dial and handshake (as tls.Dial does not support one)
	plaintextEdgeConn, err := dialer.DialContext(dialCtx, "tcp", addr)
	dialCancel()
	if err != nil {
		return nil, "", dialError{cause: errors.Wrap(err, "DialContext error")}
	}
	edgeConn := tls.Client(plaintextEdgeConn, config.TlsConfig)
	edgeConn.SetDeadline(time.Now().Add(dialTimeout))
	err = edgeConn.Handshake()
	if err != nil {
		return nil, "", dialError{cause: errors.Wrap(err, "Handshake with edge error")}
	}
	// clear the deadline on the conn; h2mux has its own timeouts
	edgeConn.SetDeadline(time.Time{})
	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	h.muxer, err = h2mux.Handshake(edgeConn, edgeConn, h2mux.MuxerConfig{
		Timeout:            5 * time.Second,
		Handler:            h,
		IsClient:           true,
		HeartbeatInterval:  config.HeartbeatInterval,
		MaxHeartbeats:      config.MaxHeartbeats,
		Logger:             config.TransportLogger.WithFields(log.Fields{}),
		CompressionQuality: h2mux.CompressionSetting(config.CompressionQuality),
	})
	if err != nil {
		return h, "", errors.New("TLS handshake error")
	}
	return h, edgeConn.LocalAddr().String(), err
}

func (h *TunnelHandler) AppendTagHeaders(r *http.Request) {
	for _, tag := range h.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (h *TunnelHandler) ServeStream(stream *h2mux.MuxedStream) error {
	h.metrics.incrementRequests(h.connectionID)
	req, err := http.NewRequest("GET", h.originUrl, h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		h.logger.WithError(err).Panic("Unexpected error from http.NewRequest")
	}
	err = H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		h.logger.WithError(err).Error("invalid request received")
	}
	h.AppendTagHeaders(req)
	cfRay := FindCfRayHeader(req)
	lbProbe := isLBProbeRequest(req)
	h.logRequest(req, cfRay, lbProbe)
	if websocket.IsWebSocketUpgrade(req) {
		conn, response, err := websocket.ClientConnect(req, h.tlsConfig)
		if err != nil {
			h.logError(stream, err)
		} else {
			stream.WriteHeaders(H1ResponseToH2Response(response))
			defer conn.Close()
			// Copy to/from stream to the undelying connection. Use the underlying
			// connection because cloudflared doesn't operate on the message themselves
			websocket.Stream(conn.UnderlyingConn(), stream)
			h.metrics.incrementResponses(h.connectionID, "200")
			h.logResponse(response, cfRay, lbProbe)
		}
	} else {
		// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
		if h.noChunkedEncoding {
			req.TransferEncoding = []string{"gzip", "deflate"}
			cLength, err := strconv.Atoi(req.Header.Get("Content-Length"))
			if err == nil {
				req.ContentLength = int64(cLength)
			}
		}

		// Request origin to keep connection alive to improve performance
		req.Header.Set("Connection", "keep-alive")

		response, err := h.httpClient.RoundTrip(req)

		if err != nil {
			h.logError(stream, err)
		} else {
			defer response.Body.Close()
			stream.WriteHeaders(H1ResponseToH2Response(response))
			if h.isEventStream(response) {
				h.writeEventStream(stream, response.Body)
			} else {
				// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
				// compression generates dictionary on first write
				io.CopyBuffer(stream, response.Body, make([]byte, 512*1024))
			}

			h.metrics.incrementResponses(h.connectionID, "200")
			h.logResponse(response, cfRay, lbProbe)
		}
	}
	h.metrics.decrementConcurrentRequests(h.connectionID)
	return nil
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

func (h *TunnelHandler) logError(stream *h2mux.MuxedStream, err error) {
	h.logger.WithError(err).Error("HTTP request error")
	stream.WriteHeaders([]h2mux.Header{{Name: ":status", Value: "502"}})
	stream.Write([]byte("502 Bad Gateway"))
	h.metrics.incrementResponses(h.connectionID, "502")
}

func (h *TunnelHandler) logRequest(req *http.Request, cfRay string, lbProbe bool) {
	logger := log.NewEntry(h.logger)
	if cfRay != "" {
		logger = logger.WithField("CF-RAY", cfRay)
		logger.Debugf("%s %s %s", req.Method, req.URL, req.Proto)
	} else if lbProbe {
		logger.Debugf("Load Balancer health check %s %s %s", req.Method, req.URL, req.Proto)
	} else {
		logger.Warnf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", req.Method, req.URL, req.Proto)
	}
	logger.Debugf("Request Headers %+v", req.Header)

	if contentLen := req.ContentLength; contentLen == -1 {
		logger.Debugf("Request Content length unknown")
	} else {
		logger.Debugf("Request content length %d", contentLen)
	}
}

func (h *TunnelHandler) logResponse(r *http.Response, cfRay string, lbProbe bool) {
	logger := log.NewEntry(h.logger)
	if cfRay != "" {
		logger = logger.WithField("CF-RAY", cfRay)
		logger.Debugf("%s", r.Status)
	} else if lbProbe {
		logger.Debugf("Response to Load Balancer health check %s", r.Status)
	} else {
		logger.Infof("%s", r.Status)
	}
	logger.Debugf("Response Headers %+v", r.Header)

	if contentLen := r.ContentLength; contentLen == -1 {
		logger.Debugf("Response content length unknown")
	} else {
		logger.Debugf("Response content length %d", contentLen)
	}
}

func (h *TunnelHandler) UpdateMetrics(connectionID string) {
	h.metrics.updateMuxerMetrics(connectionID, h.muxer.Metrics())
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}

func isLBProbeRequest(req *http.Request) bool {
	return strings.HasPrefix(req.UserAgent(), lbProbeUserAgentPrefix)
}

// Print out the given lines in a nice ASCII box.
func asciiBox(lines []string, padding int) (box []string) {
	maxLen := maxLen(lines)
	spacer := strings.Repeat(" ", padding)

	border := "+" + strings.Repeat("-", maxLen+(padding*2)) + "+"

	box = append(box, border)
	for _, line := range lines {
		box = append(box, "|"+spacer+line+strings.Repeat(" ", maxLen-len(line))+spacer+"|")
	}
	box = append(box, border)
	return
}

func maxLen(lines []string) int {
	max := 0
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max
}

func trialZoneMsg(url string) []string {
	return []string{
		"Your free tunnel has started! Visit it:",
		"  " + url,
	}
}

func activeIncidentsMsg(incidents []Incident) string {
	preamble := "There is an active Cloudflare incident that may be related:"
	if len(incidents) > 1 {
		preamble = "There are active Cloudflare incidents that may be related:"
	}
	incidentStrings := []string{}
	for _, incident := range incidents {
		incidentString := fmt.Sprintf("%s (%s)", incident.Name, incident.URL())
		incidentStrings = append(incidentStrings, incidentString)
	}
	return preamble + " " + strings.Join(incidentStrings, "; ")

}
