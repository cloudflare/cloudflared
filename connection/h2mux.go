package connection

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/h2mux"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	muxerTimeout      = 5 * time.Second
	openStreamTimeout = 30 * time.Second
)

type h2muxConnection struct {
	config      *Config
	muxerConfig *MuxerConfig
	muxer       *h2mux.Muxer
	// connectionID is only used by metrics, and prometheus requires labels to be string
	connIndexStr string
	connIndex    uint8

	observer          *Observer
	gracefulShutdownC <-chan struct{}
	stoppedGracefully bool

	// newRPCClientFunc allows us to mock RPCs during testing
	newRPCClientFunc func(context.Context, io.ReadWriteCloser, *zerolog.Logger) NamedTunnelRPCClient
}

type MuxerConfig struct {
	HeartbeatInterval  time.Duration
	MaxHeartbeats      uint64
	CompressionSetting h2mux.CompressionSetting
	MetricsUpdateFreq  time.Duration
}

func (mc *MuxerConfig) H2MuxerConfig(h h2mux.MuxedStreamHandler, log *zerolog.Logger) *h2mux.MuxerConfig {
	return &h2mux.MuxerConfig{
		Timeout:            muxerTimeout,
		Handler:            h,
		IsClient:           true,
		HeartbeatInterval:  mc.HeartbeatInterval,
		MaxHeartbeats:      mc.MaxHeartbeats,
		Log:                log,
		CompressionQuality: mc.CompressionSetting,
	}
}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewH2muxConnection(
	config *Config,
	muxerConfig *MuxerConfig,
	edgeConn net.Conn,
	connIndex uint8,
	observer *Observer,
	gracefulShutdownC <-chan struct{},
) (*h2muxConnection, error, bool) {
	h := &h2muxConnection{
		config:            config,
		muxerConfig:       muxerConfig,
		connIndexStr:      uint8ToString(connIndex),
		connIndex:         connIndex,
		observer:          observer,
		gracefulShutdownC: gracefulShutdownC,
		newRPCClientFunc:  newRegistrationRPCClient,
	}

	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, *muxerConfig.H2MuxerConfig(h, observer.logTransport), h2mux.ActiveStreams)
	if err != nil {
		recoverable := isHandshakeErrRecoverable(err, connIndex, observer)
		return nil, err, recoverable
	}
	h.muxer = muxer
	return h, nil, false
}

func (h *h2muxConnection) ServeNamedTunnel(ctx context.Context, namedTunnel *NamedTunnelConfig, connOptions *tunnelpogs.ConnectionOptions, connectedFuse ConnectedFuse) error {
	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return h.serveMuxer(serveCtx)
	})

	errGroup.Go(func() error {
		if err := h.registerNamedTunnel(serveCtx, namedTunnel, connOptions); err != nil {
			return err
		}
		connectedFuse.Connected()
		return nil
	})

	errGroup.Go(func() error {
		h.controlLoop(serveCtx, connectedFuse, true)
		return nil
	})

	err := errGroup.Wait()
	if err == errMuxerStopped {
		if h.stoppedGracefully {
			return nil
		}
		h.observer.log.Info().Uint8(LogFieldConnIndex, h.connIndex).Msg("Unexpected muxer shutdown")
	}
	return err
}

func (h *h2muxConnection) ServeClassicTunnel(ctx context.Context, classicTunnel *ClassicTunnelConfig, credentialManager CredentialManager, registrationOptions *tunnelpogs.RegistrationOptions, connectedFuse ConnectedFuse) error {
	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return h.serveMuxer(serveCtx)
	})

	errGroup.Go(func() (err error) {
		defer func() {
			if err == nil {
				connectedFuse.Connected()
			}
		}()
		if classicTunnel.UseReconnectToken && connectedFuse.IsConnected() {
			err := h.reconnectTunnel(ctx, credentialManager, classicTunnel, registrationOptions)
			if err == nil {
				return nil
			}
			// log errors and proceed to RegisterTunnel
			h.observer.log.Err(err).
				Uint8(LogFieldConnIndex, h.connIndex).
				Msg("Couldn't reconnect connection. Re-registering it instead.")
		}
		return h.registerTunnel(ctx, credentialManager, classicTunnel, registrationOptions)
	})

	errGroup.Go(func() error {
		h.controlLoop(serveCtx, connectedFuse, false)
		return nil
	})

	err := errGroup.Wait()
	if err == errMuxerStopped {
		if h.stoppedGracefully {
			return nil
		}
		h.observer.log.Info().Uint8(LogFieldConnIndex, h.connIndex).Msg("Unexpected muxer shutdown")
	}
	return err
}

func (h *h2muxConnection) serveMuxer(ctx context.Context) error {
	// All routines should stop when muxer finish serving. When muxer is shutdown
	// gracefully, it doesn't return an error, so we need to return errMuxerShutdown
	// here to notify other routines to stop
	err := h.muxer.Serve(ctx)
	if err == nil {
		return errMuxerStopped
	}
	return err
}

func (h *h2muxConnection) controlLoop(ctx context.Context, connectedFuse ConnectedFuse, isNamedTunnel bool) {
	updateMetricsTicker := time.NewTicker(h.muxerConfig.MetricsUpdateFreq)
	defer updateMetricsTicker.Stop()
	var shutdownCompleted <-chan struct{}
	for {
		select {
		case <-h.gracefulShutdownC:
			if connectedFuse.IsConnected() {
				h.unregister(isNamedTunnel)
			}
			h.stoppedGracefully = true
			h.gracefulShutdownC = nil
			shutdownCompleted = h.muxer.Shutdown()

		case <-shutdownCompleted:
			return

		case <-ctx.Done():
			// UnregisterTunnel blocks until the RPC call returns
			if !h.stoppedGracefully && connectedFuse.IsConnected() {
				h.unregister(isNamedTunnel)
			}
			h.muxer.Shutdown()
			// don't wait for shutdown to finish when context is closed, this is the hard termination path
			return

		case <-updateMetricsTicker.C:
			h.observer.metrics.updateMuxerMetrics(h.connIndexStr, h.muxer.Metrics())
		}
	}
}

func (h *h2muxConnection) newRPCStream(ctx context.Context, rpcName rpcName) (*h2mux.MuxedStream, error) {
	openStreamCtx, openStreamCancel := context.WithTimeout(ctx, openStreamTimeout)
	defer openStreamCancel()
	stream, err := h.muxer.OpenRPCStream(openStreamCtx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (h *h2muxConnection) ServeStream(stream *h2mux.MuxedStream) error {
	respWriter := &h2muxRespWriter{stream}

	req, reqErr := h.newRequest(stream)
	if reqErr != nil {
		respWriter.WriteErrorResponse()
		return reqErr
	}

	var sourceConnectionType = TypeHTTP
	if websocket.IsWebSocketUpgrade(req) {
		sourceConnectionType = TypeWebsocket
	}

	err := h.config.OriginProxy.Proxy(respWriter, req, sourceConnectionType)
	if err != nil {
		respWriter.WriteErrorResponse()
		return err
	}
	return nil
}

func (h *h2muxConnection) newRequest(stream *h2mux.MuxedStream) (*http.Request, error) {
	req, err := http.NewRequest("GET", "http://localhost:8080", h2mux.MuxedStreamReader{MuxedStream: stream})
	if err != nil {
		return nil, errors.Wrap(err, "Unexpected error from http.NewRequest")
	}
	err = H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		return nil, errors.Wrap(err, "invalid request received")
	}
	return req, nil
}

type h2muxRespWriter struct {
	*h2mux.MuxedStream
}

func (rp *h2muxRespWriter) WriteRespHeaders(status int, header http.Header) error {
	headers := H1ResponseToH2ResponseHeaders(status, header)
	headers = append(headers, h2mux.Header{Name: ResponseMetaHeader, Value: responseMetaHeaderOrigin})
	return rp.WriteHeaders(headers)
}

func (rp *h2muxRespWriter) WriteErrorResponse() {
	_ = rp.WriteHeaders([]h2mux.Header{
		{Name: ":status", Value: "502"},
		{Name: ResponseMetaHeader, Value: responseMetaHeaderCfd},
	})
	_, _ = rp.Write([]byte("502 Bad Gateway"))
}
