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

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/streamhandler"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	dialTimeout              = 15 * time.Second
	openStreamTimeout        = 30 * time.Second
	muxerTimeout             = 5 * time.Second
	lbProbeUserAgentPrefix   = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
	TagHeaderNamePrefix      = "Cf-Warp-Tag-"
	DuplicateConnectionError = "EDUPCONN"
)

type registerRPCName string

const (
	register  registerRPCName = "register"
	reconnect registerRPCName = "reconnect"
	unknown   registerRPCName = "unknown"
)

type TunnelConfig struct {
	BuildInfo            *buildinfo.BuildInfo
	ClientID             string
	ClientTlsConfig      *tls.Config
	CloseConnOnce        *sync.Once // Used to close connectedSignal no more than once
	CompressionQuality   uint64
	EdgeAddrs            []string
	GracePeriod          time.Duration
	HAConnections        int
	HTTPTransport        http.RoundTripper
	HeartbeatInterval    time.Duration
	Hostname             string
	HTTPHostHeader       string
	IncidentLookup       IncidentLookup
	IsAutoupdated        bool
	IsFreeTunnel         bool
	LBPool               string
	Logger               *log.Logger
	MaxHeartbeats        uint64
	Metrics              *TunnelMetrics
	MetricsUpdateFreq    time.Duration
	NoChunkedEncoding    bool
	OriginCert           []byte
	ReportedVersion      string
	Retries              uint
	RunFromTerminal      bool
	Tags                 []tunnelpogs.Tag
	TlsConfig            *tls.Config
	TransportLogger      *log.Logger
	UseDeclarativeTunnel bool
	WSGI                 bool
	// OriginUrl may not be used if a user specifies a unix socket.
	OriginUrl string

	// feature-flag to use new edge reconnect tokens
	UseReconnectToken bool
}

// ReconnectTunnelCredentialManager is invoked by functions in this file to
// get/set parameters for ReconnectTunnel RPC calls.
type ReconnectTunnelCredentialManager interface {
	ReconnectToken() ([]byte, error)
	EventDigest() ([]byte, error)
	SetEventDigest(eventDigest []byte)
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

func newClientRegisterTunnelError(cause error, counter *prometheus.CounterVec, name registerRPCName) clientRegisterTunnelError {
	counter.WithLabelValues(cause.Error(), string(name)).Inc()
	return clientRegisterTunnelError{cause: cause}
}

func (e clientRegisterTunnelError) Error() string {
	return e.cause.Error()
}

func (c *TunnelConfig) muxerConfig(handler h2mux.MuxedStreamHandler) h2mux.MuxerConfig {
	return h2mux.MuxerConfig{
		Timeout:            muxerTimeout,
		Handler:            handler,
		IsClient:           true,
		HeartbeatInterval:  c.HeartbeatInterval,
		MaxHeartbeats:      c.MaxHeartbeats,
		Logger:             c.TransportLogger.WithFields(log.Fields{}),
		CompressionQuality: h2mux.CompressionSetting(c.CompressionQuality),
	}
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

func StartTunnelDaemon(ctx context.Context, config *TunnelConfig, connectedSignal *signal.Signal, cloudflaredID uuid.UUID) error {
	s, err := NewSupervisor(config, cloudflaredID)
	if err != nil {
		return err
	}
	return s.Run(ctx, connectedSignal)
}

func ServeTunnelLoop(ctx context.Context,
	credentialManager ReconnectTunnelCredentialManager,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connectionID uint8,
	connectedSignal *signal.Signal,
	u uuid.UUID,
) error {
	connectionLogger := config.Logger.WithField("connectionID", connectionID)
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
		err, recoverable := ServeTunnel(
			ctx,
			credentialManager,
			config,
			connectionLogger,
			addr, connectionID,
			connectedFuse,
			&backoff,
			u,
		)
		if recoverable {
			if duration, ok := backoff.GetBackoffDuration(ctx); ok {
				connectionLogger.Infof("Retrying in %s seconds", duration)
				backoff.Backoff(ctx)
				continue
			}
		}
		return err
	}
}

func ServeTunnel(
	ctx context.Context,
	credentialManager ReconnectTunnelCredentialManager,
	config *TunnelConfig,
	logger *log.Entry,
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

	// additional tags to send other than hostname which is set in cloudflared main package
	tags := make(map[string]string)
	tags["ha"] = connectionTag

	// Returns error from parsing the origin URL or handshake errors
	handler, originLocalIP, err := NewTunnelHandler(ctx, config, addr, connectionID)
	if err != nil {
		errLog := logger.WithError(err)
		switch err.(type) {
		case connection.DialError:
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

	errGroup.Go(func() (err error) {
		defer func() {
			if err == nil {
				connectedFuse.Fuse(true)
				backoff.SetGracePeriod()
			}
		}()

		if config.UseReconnectToken && connectedFuse.Value() {
			token, tokenErr := credentialManager.ReconnectToken()
			eventDigest, eventDigestErr := credentialManager.EventDigest()
			// if we have both credentials, we can reconnect
			if tokenErr == nil && eventDigestErr == nil {
				return ReconnectTunnel(serveCtx, token, eventDigest, handler.muxer, config, logger, connectionID, originLocalIP, u)
			}
			// log errors and proceed to RegisterTunnel
			if tokenErr != nil {
				logger.WithError(tokenErr).Error("Couldn't get reconnect token")
			}
			if eventDigestErr != nil {
				logger.WithError(eventDigestErr).Error("Couldn't get event digest")
			}
		}
		return RegisterTunnel(serveCtx, credentialManager, handler.muxer, config, logger, connectionID, originLocalIP, u)
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
		_ = newClientRegisterTunnelError(err, config.Metrics.regFail, unknown)

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
			return err, true
		case muxerShutdownError:
			logger.Infof("Muxer shutdown")
			return err, true
		default:
			logger.WithError(err).Error("Serve tunnel error")
			return err, true
		}
	}
	return nil, true
}

func RegisterTunnel(
	ctx context.Context,
	credentialManager ReconnectTunnelCredentialManager,
	muxer *h2mux.Muxer,
	config *TunnelConfig,
	logger *log.Entry,
	connectionID uint8,
	originLocalIP string,
	uuid uuid.UUID,
) error {
	config.TransportLogger.Debug("initiating RPC stream to register")
	tunnelServer, err := connection.NewRPCClient(ctx, muxer, config.TransportLogger.WithField("subsystem", "rpc-register"), openStreamTimeout)
	if err != nil {
		// RPC stream open error
		return newClientRegisterTunnelError(err, config.Metrics.rpcFail, register)
	}
	defer tunnelServer.Close()
	// Request server info without blocking tunnel registration; must use capnp library directly.
	serverInfoPromise := tunnelrpc.TunnelServer{Client: tunnelServer.Client}.GetServerInfo(ctx, func(tunnelrpc.TunnelServer_getServerInfo_Params) error {
		return nil
	})
	LogServerInfo(serverInfoPromise.Result(), connectionID, config.Metrics, logger)
	registration := tunnelServer.RegisterTunnel(
		ctx,
		config.OriginCert,
		config.Hostname,
		config.RegistrationOptions(connectionID, originLocalIP, uuid),
	)
	if registrationErr := registration.DeserializeError(); registrationErr != nil {
		// RegisterTunnel RPC failure
		return processRegisterTunnelError(registrationErr, config.Metrics, register)
	}
	credentialManager.SetEventDigest(registration.EventDigest)
	return processRegistrationSuccess(config, logger, connectionID, registration, register)
}

func ReconnectTunnel(
	ctx context.Context,
	token []byte,
	eventDigest []byte,
	muxer *h2mux.Muxer,
	config *TunnelConfig,
	logger *log.Entry,
	connectionID uint8,
	originLocalIP string,
	uuid uuid.UUID,
) error {
	config.TransportLogger.Debug("initiating RPC stream to reconnect")
	tunnelServer, err := connection.NewRPCClient(ctx, muxer, config.TransportLogger.WithField("subsystem", "rpc-reconnect"), openStreamTimeout)
	if err != nil {
		// RPC stream open error
		return newClientRegisterTunnelError(err, config.Metrics.rpcFail, reconnect)
	}
	defer tunnelServer.Close()
	// Request server info without blocking tunnel registration; must use capnp library directly.
	serverInfoPromise := tunnelrpc.TunnelServer{Client: tunnelServer.Client}.GetServerInfo(ctx, func(tunnelrpc.TunnelServer_getServerInfo_Params) error {
		return nil
	})
	LogServerInfo(serverInfoPromise.Result(), connectionID, config.Metrics, logger)
	registration := tunnelServer.ReconnectTunnel(
		ctx,
		token,
		eventDigest,
		config.Hostname,
		config.RegistrationOptions(connectionID, originLocalIP, uuid),
	)
	if registrationErr := registration.DeserializeError(); registrationErr != nil {
		// ReconnectTunnel RPC failure
		return processRegisterTunnelError(registrationErr, config.Metrics, reconnect)
	}
	return processRegistrationSuccess(config, logger, connectionID, registration, reconnect)
}

func processRegistrationSuccess(config *TunnelConfig, logger *log.Entry, connectionID uint8, registration *tunnelpogs.TunnelRegistration, name registerRPCName) error {
	for _, logLine := range registration.LogLines {
		logger.Info(logLine)
	}

	if registration.TunnelID != "" {
		config.Metrics.tunnelsHA.AddTunnelID(connectionID, registration.TunnelID)
		logger.Infof("Each HA connection's tunnel IDs: %v", config.Metrics.tunnelsHA.String())
	}

	// Print out the user's trial zone URL in a nice box (if they requested and got one)
	if isTrialTunnel := config.Hostname == ""; isTrialTunnel {
		if url, err := url.Parse(registration.Url); err == nil {
			for _, line := range asciiBox(trialZoneMsg(url.String()), 2) {
				logger.Infoln(line)
			}
		} else {
			logger.Errorln("Failed to connect tunnel, please try again.")
			return fmt.Errorf("empty URL in response from Cloudflare edge")
		}
	}

	config.Metrics.userHostnamesCounts.WithLabelValues(registration.Url).Inc()

	logger.Infof("Route propagating, it may take up to 1 minute for your new route to become functional")
	config.Metrics.regSuccess.WithLabelValues(string(name)).Inc()
	return nil
}

func processRegisterTunnelError(err tunnelpogs.TunnelRegistrationError, metrics *TunnelMetrics, name registerRPCName) error {
	if err.Error() == DuplicateConnectionError {
		metrics.regFail.WithLabelValues("dup_edge_conn", string(name)).Inc()
		return dupConnRegisterTunnelError{}
	}
	metrics.regFail.WithLabelValues("server_error", string(name)).Inc()
	return serverRegisterTunnelError{
		cause:     fmt.Errorf("Server error: %s", err.Error()),
		permanent: err.IsPermanent(),
	}
}

func UnregisterTunnel(muxer *h2mux.Muxer, gracePeriod time.Duration, logger *log.Logger) error {
	logger.Debug("initiating RPC stream to unregister")
	ctx := context.Background()
	tunnelServer, err := connection.NewRPCClient(ctx, muxer, logger.WithField("subsystem", "rpc-unregister"), openStreamTimeout)
	if err != nil {
		// RPC stream open error
		return err
	}
	// gracePeriod is encoded in int64 using capnproto
	return tunnelServer.UnregisterTunnel(ctx, gracePeriod.Nanoseconds())
}

func LogServerInfo(
	promise tunnelrpc.ServerInfo_Promise,
	connectionID uint8,
	metrics *TunnelMetrics,
	logger *log.Entry,
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

type TunnelHandler struct {
	originUrl      string
	httpHostHeader string
	muxer          *h2mux.Muxer
	httpClient     http.RoundTripper
	tlsConfig      *tls.Config
	tags           []tunnelpogs.Tag
	metrics        *TunnelMetrics
	// connectionID is only used by metrics, and prometheus requires labels to be string
	connectionID      string
	logger            *log.Logger
	noChunkedEncoding bool
}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewTunnelHandler(ctx context.Context,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connectionID uint8,
) (*TunnelHandler, string, error) {
	originURL, err := validation.ValidateUrl(config.OriginUrl)
	if err != nil {
		return nil, "", fmt.Errorf("unable to parse origin URL %#v", originURL)
	}
	h := &TunnelHandler{
		originUrl:         originURL,
		httpHostHeader:    config.HTTPHostHeader,
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

	edgeConn, err := connection.DialEdge(ctx, dialTimeout, config.TlsConfig, addr)
	if err != nil {
		return nil, "", err
	}
	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	h.muxer, err = h2mux.Handshake(edgeConn, edgeConn, config.muxerConfig(h), h.metrics.activeStreams)
	if err != nil {
		return nil, "", errors.Wrap(err, "h2mux handshake with edge error")
	}
	return h, edgeConn.LocalAddr().String(), nil
}

func (h *TunnelHandler) AppendTagHeaders(r *http.Request) {
	for _, tag := range h.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (h *TunnelHandler) ServeStream(stream *h2mux.MuxedStream) error {
	h.metrics.incrementRequests(h.connectionID)
	defer h.metrics.decrementConcurrentRequests(h.connectionID)

	req, reqErr := h.createRequest(stream)
	if reqErr != nil {
		h.logError(stream, reqErr)
		return reqErr
	}

	cfRay := streamhandler.FindCfRayHeader(req)
	lbProbe := streamhandler.IsLBProbeRequest(req)
	h.logRequest(req, cfRay, lbProbe)

	var resp *http.Response
	var respErr error
	if websocket.IsWebSocketUpgrade(req) {
		resp, respErr = h.serveWebsocket(stream, req)
	} else {
		resp, respErr = h.serveHTTP(stream, req)
	}
	if respErr != nil {
		h.logError(stream, respErr)
		return respErr
	}
	h.logResponseOk(resp, cfRay, lbProbe)
	return nil
}

func (h *TunnelHandler) createRequest(stream *h2mux.MuxedStream) (*http.Request, error) {
	req, err := http.NewRequest("GET", h.originUrl, h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		return nil, errors.Wrap(err, "Unexpected error from http.NewRequest")
	}
	err = h2mux.OldH2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		return nil, errors.Wrap(err, "invalid request received")
	}
	h.AppendTagHeaders(req)
	return req, nil
}

func (h *TunnelHandler) serveWebsocket(stream *h2mux.MuxedStream, req *http.Request) (*http.Response, error) {
	if h.httpHostHeader != "" {
		req.Header.Set("Host", h.httpHostHeader)
		req.Host = h.httpHostHeader
	}

	conn, response, err := websocket.ClientConnect(req, h.tlsConfig)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = stream.WriteHeaders(h2mux.OldH1ResponseToH2ResponseHeaders(response))
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	// Copy to/from stream to the undelying connection. Use the underlying
	// connection because cloudflared doesn't operate on the message themselves
	websocket.Stream(conn.UnderlyingConn(), stream)
	return response, nil
}

func (h *TunnelHandler) serveHTTP(stream *h2mux.MuxedStream, req *http.Request) (*http.Response, error) {
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

	if h.httpHostHeader != "" {
		req.Header.Set("Host", h.httpHostHeader)
		req.Host = h.httpHostHeader
	}

	response, err := h.httpClient.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "Error proxying request to origin")
	}
	defer response.Body.Close()

	err = stream.WriteHeaders(h2mux.OldH1ResponseToH2ResponseHeaders(response))
	if err != nil {
		return nil, errors.Wrap(err, "Error writing response header")
	}
	if h.isEventStream(response) {
		h.writeEventStream(stream, response.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		io.CopyBuffer(stream, response.Body, make([]byte, 512*1024))
	}
	return response, nil
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

func (h *TunnelHandler) logResponseOk(r *http.Response, cfRay string, lbProbe bool) {
	h.metrics.incrementResponses(h.connectionID, "200")
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
