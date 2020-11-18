package connection

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
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

	observer *Observer
}

type MuxerConfig struct {
	HeartbeatInterval  time.Duration
	MaxHeartbeats      uint64
	CompressionSetting h2mux.CompressionSetting
	MetricsUpdateFreq  time.Duration
}

func (mc *MuxerConfig) H2MuxerConfig(h h2mux.MuxedStreamHandler, logger logger.Service) *h2mux.MuxerConfig {
	return &h2mux.MuxerConfig{
		Timeout:            muxerTimeout,
		Handler:            h,
		IsClient:           true,
		HeartbeatInterval:  mc.HeartbeatInterval,
		MaxHeartbeats:      mc.MaxHeartbeats,
		Logger:             logger,
		CompressionQuality: mc.CompressionSetting,
	}
}

// NewTunnelHandler returns a TunnelHandler, origin LAN IP and error
func NewH2muxConnection(ctx context.Context,
	config *Config,
	muxerConfig *MuxerConfig,
	edgeConn net.Conn,
	connIndex uint8,
	observer *Observer,
) (*h2muxConnection, error, bool) {
	h := &h2muxConnection{
		config:       config,
		muxerConfig:  muxerConfig,
		connIndexStr: uint8ToString(connIndex),
		connIndex:    connIndex,
		observer:     observer,
	}

	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, *muxerConfig.H2MuxerConfig(h, observer), h2mux.ActiveStreams)
	if err != nil {
		recoverable := isHandshakeErrRecoverable(err, connIndex, observer)
		return nil, err, recoverable
	}
	h.muxer = muxer
	return h, nil, false
}

func (h *h2muxConnection) ServeNamedTunnel(ctx context.Context, namedTunnel *NamedTunnelConfig, credentialManager CredentialManager, connOptions *tunnelpogs.ConnectionOptions, connectedFuse ConnectedFuse) error {
	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return h.serveMuxer(serveCtx)
	})

	errGroup.Go(func() error {
		stream, err := h.newRPCStream(serveCtx, register)
		if err != nil {
			return err
		}
		rpcClient := newRegistrationRPCClient(ctx, stream, h.observer)
		defer rpcClient.Close()

		if err = rpcClient.RegisterConnection(serveCtx, namedTunnel, connOptions, h.connIndex, h.observer); err != nil {
			return err
		}
		connectedFuse.Connected()
		return nil
	})

	errGroup.Go(func() error {
		h.controlLoop(serveCtx, connectedFuse, true)
		return nil
	})
	return errGroup.Wait()
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
			h.observer.Errorf("Couldn't reconnect connection %d. Reregistering it instead. Error was: %v", h.connIndex, err)
		}
		return h.registerTunnel(ctx, credentialManager, classicTunnel, registrationOptions)
	})

	errGroup.Go(func() error {
		h.controlLoop(serveCtx, connectedFuse, false)
		return nil
	})
	return errGroup.Wait()
}

func (h *h2muxConnection) serveMuxer(ctx context.Context) error {
	// All routines should stop when muxer finish serving. When muxer is shutdown
	// gracefully, it doesn't return an error, so we need to return errMuxerShutdown
	// here to notify other routines to stop
	err := h.muxer.Serve(ctx)
	if err == nil {
		return muxerShutdownError{}
	}
	return err
}

func (h *h2muxConnection) controlLoop(ctx context.Context, connectedFuse ConnectedFuse, isNamedTunnel bool) {
	updateMetricsTickC := time.Tick(h.muxerConfig.MetricsUpdateFreq)
	for {
		select {
		case <-ctx.Done():
			// UnregisterTunnel blocks until the RPC call returns
			if connectedFuse.IsConnected() {
				h.unregister(isNamedTunnel)
			}
			h.muxer.Shutdown()
			return
		case <-updateMetricsTickC:
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

	err := h.config.OriginClient.Proxy(respWriter, req, websocket.IsWebSocketUpgrade(req))
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
	err = h2mux.H2RequestHeadersToH1Request(stream.Headers, req)
	if err != nil {
		return nil, errors.Wrap(err, "invalid request received")
	}
	return req, nil
}

type h2muxRespWriter struct {
	*h2mux.MuxedStream
}

func (rp *h2muxRespWriter) WriteRespHeaders(resp *http.Response) error {
	headers := h2mux.H1ResponseToH2ResponseHeaders(resp)
	headers = append(headers, h2mux.Header{Name: ResponseMetaHeaderField, Value: responseMetaHeaderOrigin})
	return rp.WriteHeaders(headers)
}

func (rp *h2muxRespWriter) WriteErrorResponse() {
	rp.WriteHeaders([]h2mux.Header{
		{Name: ":status", Value: "502"},
		{Name: ResponseMetaHeaderField, Value: responseMetaHeaderCfd},
	})
	rp.Write([]byte("502 Bad Gateway"))
}
