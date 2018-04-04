package origin

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/cloudflare/cloudflared/websocket"

	raven "github.com/getsentry/raven-go"
	"github.com/pkg/errors"
	_ "github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	rpc "zombiezen.com/go/capnproto2/rpc"
)

var Log *logrus.Logger

const (
	dialTimeout = 15 * time.Second

	TagHeaderNamePrefix      = "Cf-Warp-Tag-"
	DuplicateConnectionError = "EDUPCONN"
	CloudflaredPingHeader    = "Cloudflard-Ping"
)

type DNSValidationConfig struct {
	VerifyDNSPropagated bool
	DNSPingRetries      uint
	DNSInitWaitTime     time.Duration
	PingFreq            time.Duration
}

type TunnelConfig struct {
	EdgeAddrs         []string
	OriginUrl         string
	Hostname          string
	OriginCert        []byte
	TlsConfig         *tls.Config
	ClientTlsConfig   *tls.Config
	Retries           uint
	HeartbeatInterval time.Duration
	MaxHeartbeats     uint64
	ClientID          string
	ReportedVersion   string
	LBPool            string
	Tags              []tunnelpogs.Tag
	HAConnections     int
	HTTPTransport     http.RoundTripper
	Metrics           *tunnelMetrics
	MetricsUpdateFreq time.Duration
	ProtocolLogger    *logrus.Logger
	Logger            *logrus.Logger
	IsAutoupdated     bool
	GracePeriod       time.Duration
	*DNSValidationConfig
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

type printableRegisterTunnelError struct {
	cause     error
	permanent bool
}

func (e printableRegisterTunnelError) Error() string {
	return e.cause.Error()
}

func (c *TunnelConfig) RegistrationOptions(connectionID uint8, OriginLocalIP string) *tunnelpogs.RegistrationOptions {
	policy := tunnelrpc.ExistingTunnelPolicy_balance
	if c.HAConnections <= 1 && c.LBPool == "" {
		policy = tunnelrpc.ExistingTunnelPolicy_disconnect
	}
	return &tunnelpogs.RegistrationOptions{
		ClientID:             c.ClientID,
		Version:              c.ReportedVersion,
		OS:                   fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH),
		ExistingTunnelPolicy: policy,
		PoolName:             c.LBPool,
		Tags:                 c.Tags,
		ConnectionID:         connectionID,
		OriginLocalIP:        OriginLocalIP,
		IsAutoupdated:        c.IsAutoupdated,
	}
}

func StartTunnelDaemon(config *TunnelConfig, shutdownC <-chan struct{}, connectedSignal chan struct{}) error {
	Log = config.Logger
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-shutdownC
		cancel()
	}()
	// If a user specified negative HAConnections, we will treat it as requesting 1 connection
	if config.HAConnections > 1 {
		return NewSupervisor(config).Run(ctx, connectedSignal)
	} else {
		addrs, err := ResolveEdgeIPs(config.EdgeAddrs)
		if err != nil {
			return err
		}
		return ServeTunnelLoop(ctx, config, addrs[0], 0, connectedSignal)
	}
}

func ServeTunnelLoop(ctx context.Context, config *TunnelConfig, addr *net.TCPAddr, connectionID uint8, connectedSignal chan struct{}) error {
	config.Metrics.incrementHaConnections()
	defer config.Metrics.decrementHaConnections()
	backoff := BackoffHandler{MaxRetries: config.Retries}
	// Used to close connectedSignal no more than once
	connectedFuse := h2mux.NewBooleanFuse()
	go func() {
		if connectedFuse.Await() {
			close(connectedSignal)
		}
	}()
	// Ensure the above goroutine will terminate if we return without connecting
	defer connectedFuse.Fuse(false)
	for {
		err, recoverable := ServeTunnel(ctx, config, addr, connectionID, connectedFuse, &backoff)
		if recoverable {
			if duration, ok := backoff.GetBackoffDuration(ctx); ok {
				Log.Infof("Retrying in %s seconds", duration)
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
) (err error, recoverable bool) {
	var wg sync.WaitGroup
	wg.Add(2)
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
	// Returns error from parsing the origin URL or handshake errors
	handler, originLocalIP, err := NewTunnelHandler(ctx, config, addr.String(), connectionID)
	if err != nil {
		errLog := Log.WithError(err)
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
	serveCtx, serveCancel := context.WithCancel(ctx)
	registerErrC := make(chan error, 1)
	go func() {
		defer wg.Done()
		err := RegisterTunnel(serveCtx, handler.muxer, config, connectionID, originLocalIP)
		if err == nil {
			connectedFuse.Fuse(true)
			backoff.SetGracePeriod()
		} else {
			serveCancel()
		}
		registerErrC <- err
	}()
	updateMetricsTickC := time.Tick(config.MetricsUpdateFreq)
	go func() {
		defer wg.Done()
		connectionTag := uint8ToString(connectionID)
		for {
			select {
			case <-serveCtx.Done():
				// UnregisterTunnel blocks until the RPC call returns
				UnregisterTunnel(handler.muxer, config.GracePeriod)
				handler.muxer.Shutdown()
				return
			case <-updateMetricsTickC:
				handler.UpdateMetrics(connectionTag)
			}
		}
	}()

	err = handler.muxer.Serve()
	serveCancel()
	registerErr := <-registerErrC
	wg.Wait()
	if err != nil {
		Log.WithError(err).Error("Tunnel error")
		return err, true
	}
	if registerErr != nil {
		// Don't retry on errors like entitlement failure or version too old
		if e, ok := registerErr.(printableRegisterTunnelError); ok {
			Log.Error(e)
			return e.cause, !e.permanent
		} else if e, ok := registerErr.(dupConnRegisterTunnelError); ok {
			Log.Info("Already connected to this server, selecting a different one")
			return e, true
		}
		// Only log errors to Sentry that may have been caused by the client side, to reduce dupes
		raven.CaptureError(registerErr, nil)
		Log.Error("Cannot register")
		return err, true
	}
	return nil, false
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

func RegisterTunnel(ctx context.Context, muxer *h2mux.Muxer, config *TunnelConfig, connectionID uint8, originLocalIP string) error {
	logger := Log.WithField("subsystem", "rpc")
	logger.Debug("initiating RPC stream to register")
	stream, err := muxer.OpenStream([]h2mux.Header{
		{Name: ":method", Value: "RPC"},
		{Name: ":scheme", Value: "capnp"},
		{Name: ":path", Value: "*"},
	}, nil)
	if err != nil {
		// RPC stream open error
		raven.CaptureError(err, nil)
		return err
	}
	if !IsRPCStreamResponse(stream.Headers) {
		// stream response error
		raven.CaptureError(err, nil)
		return err
	}
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger.WithField("subsystem", "rpc-transport")),
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
		config.RegistrationOptions(connectionID, originLocalIP),
	)
	LogServerInfo(logger, serverInfoPromise.Result(), connectionID, config.Metrics)
	if err != nil {
		// RegisterTunnel RPC failure
		return err
	}
	for _, logLine := range registration.LogLines {
		logger.Info(logLine)
	}
	if registration.Err == DuplicateConnectionError {
		return dupConnRegisterTunnelError{}
	} else if registration.Err != "" {
		return printableRegisterTunnelError{
			cause:     fmt.Errorf("Server error: %s", registration.Err),
			permanent: registration.PermanentFailure,
		}
	}

	return nil
}

func UnregisterTunnel(muxer *h2mux.Muxer, gracePeriod time.Duration) error {
	logger := Log.WithField("subsystem", "rpc")
	logger.Debug("initiating RPC stream to unregister")
	stream, err := muxer.OpenStream([]h2mux.Header{
		{Name: ":method", Value: "RPC"},
		{Name: ":scheme", Value: "capnp"},
		{Name: ":path", Value: "*"},
	}, nil)
	if err != nil {
		// RPC stream open error
		raven.CaptureError(err, nil)
		return err
	}
	if !IsRPCStreamResponse(stream.Headers) {
		// stream response error
		raven.CaptureError(err, nil)
		return err
	}
	ctx := context.Background()
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger.WithField("subsystem", "rpc-transport")),
	)
	defer conn.Close()
	ts := tunnelpogs.TunnelServer_PogsClient{Client: conn.Bootstrap(ctx)}
	// gracePeriod is encoded in int64 using capnproto
	return ts.UnregisterTunnel(ctx, gracePeriod.Nanoseconds())
}

func LogServerInfo(logger *logrus.Entry,
	promise tunnelrpc.ServerInfo_Promise,
	connectionID uint8,
	metrics *tunnelMetrics,
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
	Log.Infof("Connected to %s", serverInfo.LocationName)
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
	metrics    *tunnelMetrics
	// connectionID is only used by metrics, and prometheus requires labels to be string
	connectionID string
	clientID     string
}

var dialer = net.Dialer{DualStack: true}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewTunnelHandler(ctx context.Context, config *TunnelConfig, addr string, connectionID uint8) (*TunnelHandler, string, error) {
	url, err := validation.ValidateUrl(config.OriginUrl)
	if err != nil {
		return nil, "", fmt.Errorf("Unable to parse origin url %#v", url)
	}
	h := &TunnelHandler{
		originUrl:    url,
		httpClient:   config.HTTPTransport,
		tlsConfig:    config.ClientTlsConfig,
		tags:         config.Tags,
		metrics:      config.Metrics,
		connectionID: uint8ToString(connectionID),
		clientID:     config.ClientID,
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
		Timeout:           5 * time.Second,
		Handler:           h,
		IsClient:          true,
		HeartbeatInterval: config.HeartbeatInterval,
		MaxHeartbeats:     config.MaxHeartbeats,
		Logger:            config.ProtocolLogger,
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
		Log.WithError(err).Panic("Unexpected error from http.NewRequest")
	}
	err = H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		Log.WithError(err).Error("invalid request received")
	}
	h.AppendTagHeaders(req)
	cfRay := FindCfRayHeader(req)
	h.logRequest(req, cfRay)
	if h.isCloudflaredPing(req) {
		stream.WriteHeaders([]h2mux.Header{{Name: ":status", Value: fmt.Sprintf("%d", http.StatusOK)}})
		return nil
	}
	if websocket.IsWebSocketUpgrade(req) {
		conn, response, err := websocket.ClientConnect(req, h.tlsConfig)
		if err != nil {
			h.logError(stream, err)
		} else {
			stream.WriteHeaders(H1ResponseToH2Response(response))
			defer conn.Close()
			websocket.Stream(conn.UnderlyingConn(), stream)
			h.metrics.incrementResponses(h.connectionID, "200")
			h.logResponse(response, cfRay)
		}
	} else {
		response, err := h.httpClient.RoundTrip(req)
		if err != nil {
			h.logError(stream, err)
		} else {
			defer response.Body.Close()
			stream.WriteHeaders(H1ResponseToH2Response(response))
			io.Copy(stream, response.Body)
			h.metrics.incrementResponses(h.connectionID, "200")
			h.logResponse(response, cfRay)
		}
	}
	h.metrics.decrementConcurrentRequests(h.connectionID)
	return nil
}

func (h *TunnelHandler) isCloudflaredPing(h1 *http.Request) bool {
	if h1.Header.Get(CloudflaredPingHeader) == h.clientID {
		return true
	}
	return false
}

func (h *TunnelHandler) logError(stream *h2mux.MuxedStream, err error) {
	Log.WithError(err).Error("HTTP request error")
	stream.WriteHeaders([]h2mux.Header{{Name: ":status", Value: "502"}})
	stream.Write([]byte("502 Bad Gateway"))
	h.metrics.incrementResponses(h.connectionID, "502")
}

func (h *TunnelHandler) logRequest(req *http.Request, cfRay string) {
	if cfRay != "" {
		Log.WithField("CF-RAY", cfRay).Infof("%s %s %s", req.Method, req.URL, req.Proto)
	} else {
		Log.Warnf("All requests should have a CF-RAY header. Please open a support ticket with Cloudflare. %s %s %s ", req.Method, req.URL, req.Proto)
	}
	Log.Debugf("Request Headers %+v", req.Header)
}

func (h *TunnelHandler) logResponse(r *http.Response, cfRay string) {
	if cfRay != "" {
		Log.WithField("CF-RAY", cfRay).Infof("%s", r.Status)
	} else {
		Log.Infof("%s", r.Status)
	}
	Log.Debugf("Response Headers %+v", r.Header)
}

func (h *TunnelHandler) UpdateMetrics(connectionID string) {
	h.metrics.updateMuxerMetrics(connectionID, h.muxer.Metrics())
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}
