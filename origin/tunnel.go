package origin

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudflare/cloudflare-warp/h2mux"
	"github.com/cloudflare/cloudflare-warp/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflare-warp/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflare-warp/validation"

	log "github.com/Sirupsen/logrus"
	raven "github.com/getsentry/raven-go"
	"github.com/pkg/errors"
	rpc "zombiezen.com/go/capnproto2/rpc"
)

const (
	dialTimeout = 15 * time.Second

	TagHeaderNamePrefix = "Cf-Warp-Tag-"
)

type TunnelConfig struct {
	EdgeAddr          string
	OriginUrl         string
	Hostname          string
	APIKey            string
	APIEmail          string
	APICAKey          string
	TlsConfig         *tls.Config
	Retries           uint
	HeartbeatInterval time.Duration
	MaxHeartbeats     uint64
	ClientID          string
	ReportedVersion   string
	LBPool            string
	Tags              []tunnelpogs.Tag
	AccessInternalIP  bool
	ConnectedSignal   h2mux.Signal
}

type dialError struct {
	cause error
}

func (e dialError) Error() string {
	return e.cause.Error()
}

type printableRegisterTunnelError struct {
	cause     error
	permanent bool
}

func (e printableRegisterTunnelError) Error() string {
	return e.cause.Error()
}

func (c *TunnelConfig) RegistrationOptions() *tunnelpogs.RegistrationOptions {
	policy := tunnelrpc.ExistingTunnelPolicy_disconnect
	if c.LBPool != "" {
		policy = tunnelrpc.ExistingTunnelPolicy_balance
	}
	return &tunnelpogs.RegistrationOptions{
		ClientID:             c.ClientID,
		Version:              c.ReportedVersion,
		OS:                   fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH),
		ExistingTunnelPolicy: policy,
		PoolID:               c.LBPool,
		Tags:                 c.Tags,
		ExposeInternalHostname: c.AccessInternalIP,
	}
}

func StartTunnelDaemon(config *TunnelConfig, shutdownC <-chan struct{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-shutdownC
		cancel()
	}()
	backoff := BackoffHandler{MaxRetries: config.Retries}
	for {
		err, recoverable := ServeTunnel(ctx, config, &backoff)
		if recoverable {
			if duration, ok := backoff.GetBackoffDuration(ctx); ok {
				log.Infof("Retrying in %s seconds", duration)
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
	backoff *BackoffHandler,
) (err error, recoverable bool) {
	// Returns error from parsing the origin URL or handshake errors
	handler, err := NewTunnelHandler(ctx, config)
	if err != nil {
		errLog := log.WithError(err)
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
		err := RegisterTunnel(serveCtx, handler.muxer, config)
		if err == nil {
			config.ConnectedSignal.Signal()
			backoff.SetGracePeriod()
		} else {
			serveCancel()
		}
		registerErrC <- err
	}()
	go func() {
		<-serveCtx.Done()
		handler.muxer.Shutdown()
	}()
	err = handler.muxer.Serve()
	serveCancel()
	registerErr := <-registerErrC
	if err != nil {
		log.WithError(err).Error("Tunnel error")
		return err, true
	}
	if registerErr != nil {
		raven.CaptureError(registerErr, nil)
		// Don't retry on errors like entitlement failure or version too old
		if e, ok := registerErr.(printableRegisterTunnelError); ok {
			log.WithError(e).Error("Cannot register")
			if e.permanent {
				return nil, false
			}
			return e.cause, true
		}
		log.Error("Cannot register")
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

func RegisterTunnel(ctx context.Context, muxer *h2mux.Muxer, config *TunnelConfig) error {
	logger := log.WithField("subsystem", "rpc")
	logger.Debug("initiating RPC stream")
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
		&tunnelpogs.Authentication{Key: config.APIKey, Email: config.APIEmail, OriginCAKey: config.APICAKey},
		config.Hostname,
		config.RegistrationOptions(),
	)
	LogServerInfo(logger, serverInfoPromise.Result())
	if err != nil {
		// RegisterTunnel RPC failure
		return err
	}
	for _, logLine := range registration.LogLines {
		logger.Info(logLine)
	}
	if registration.Err != "" {
		return printableRegisterTunnelError{
			cause:     fmt.Errorf("Server error: %s", registration.Err),
			permanent: registration.PermanentFailure,
		}
	}
	for _, url := range registration.Urls {
		log.Infof("Registered at %s", url)
	}
	for _, logLine := range registration.LogLines {
		log.Infof(logLine)
	}
	return nil
}

func LogServerInfo(logger *log.Entry, promise tunnelrpc.ServerInfo_Promise) {
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
	log.Infof("Connected to %s", serverInfo.LocationName)
}

type TunnelHandler struct {
	originUrl  string
	muxer      *h2mux.Muxer
	httpClient *http.Client
	tags       []tunnelpogs.Tag
}

var dialer = net.Dialer{DualStack: true}

func NewTunnelHandler(ctx context.Context, config *TunnelConfig) (*TunnelHandler, error) {
	url, err := validation.ValidateUrl(config.OriginUrl)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse origin url %#v", url)
	}
	h := &TunnelHandler{
		originUrl:  url,
		httpClient: &http.Client{Timeout: time.Minute},
		tags:       config.Tags,
	}
	// Inherit from parent context so we can cancel (Ctrl-C) while dialing
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	// TUN-92: enforce a timeout on dial and handshake (as tls.Dial does not support one)
	plaintextEdgeConn, err := dialer.DialContext(dialCtx, "tcp", config.EdgeAddr)
	dialCancel()
	if err != nil {
		return nil, dialError{cause: err}
	}
	edgeConn := tls.Client(plaintextEdgeConn, config.TlsConfig)
	edgeConn.SetDeadline(time.Now().Add(dialTimeout))
	err = edgeConn.Handshake()
	if err != nil {
		return nil, dialError{cause: err}
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
	})
	if err != nil {
		return h, errors.New("TLS handshake error")
	}
	return h, err
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

func (h *TunnelHandler) AppendTagHeaders(r *http.Request) {
	for _, tag := range h.tags {
		r.Header.Add(TagHeaderNamePrefix+tag.Name, tag.Value)
	}
}

func (h *TunnelHandler) ServeStream(stream *h2mux.MuxedStream) error {
	req, err := http.NewRequest("GET", h.originUrl, h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		log.WithError(err).Panic("Unexpected error from http.NewRequest")
	}
	err = H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		log.WithError(err).Error("invalid request received")
	}
	h.AppendTagHeaders(req)
	response, err := h.httpClient.Do(req)
	if err != nil {
		log.WithError(err).Error("HTTP request error")
		stream.WriteHeaders([]h2mux.Header{{Name: ":status", Value: "502"}})
		stream.Write([]byte("502 Bad Gateway"))
	} else {
		defer response.Body.Close()
		stream.WriteHeaders(H1ResponseToH2Response(response))
		io.Copy(stream, response.Body)
	}
	return nil
}
