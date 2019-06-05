package connection

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/streamhandler"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	rpc "zombiezen.com/go/capnproto2/rpc"
)

const (
	dialTimeout       = 5 * time.Second
	openStreamTimeout = 30 * time.Second
)

type dialError struct {
	cause error
}

func (e dialError) Error() string {
	return e.cause.Error()
}

type muxerShutdownError struct{}

func (e muxerShutdownError) Error() string {
	return "muxer shutdown"
}

type ConnectionConfig struct {
	TLSConfig         *tls.Config
	HeartbeatInterval time.Duration
	MaxHeartbeats     uint64
	Logger            *logrus.Entry
}

type connectionHandler interface {
	serve(ctx context.Context) error
	connect(ctx context.Context, parameters *tunnelpogs.ConnectParameters) (*tunnelpogs.ConnectResult, error)
	shutdown()
}

type h2muxHandler struct {
	muxer  *h2mux.Muxer
	logger *logrus.Entry
}

func (h *h2muxHandler) serve(ctx context.Context) error {
	// Serve doesn't return until h2mux is shutdown
	if err := h.muxer.Serve(ctx); err != nil {
		return err
	}
	return muxerShutdownError{}
}

// Connect is used to establish connections with cloudflare's edge network
func (h *h2muxHandler) connect(ctx context.Context, parameters *tunnelpogs.ConnectParameters) (*tunnelpogs.ConnectResult, error) {
	openStreamCtx, cancel := context.WithTimeout(ctx, openStreamTimeout)
	defer cancel()
	conn, err := h.newRPConn(openStreamCtx)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create new RPC connection")
	}
	defer conn.Close()
	tsClient := tunnelpogs.TunnelServer_PogsClient{Client: conn.Bootstrap(ctx)}
	return tsClient.Connect(ctx, parameters)
}

func (h *h2muxHandler) shutdown() {
	h.muxer.Shutdown()
}

func (h *h2muxHandler) newRPConn(ctx context.Context) (*rpc.Conn, error) {
	stream, err := h.muxer.OpenRPCStream(ctx)
	if err != nil {
		return nil, err
	}
	return rpc.NewConn(
		tunnelrpc.NewTransportLogger(h.logger.WithField("subsystem", "rpc-register"), rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(h.logger.WithField("subsystem", "rpc-transport")),
	), nil
}

// NewConnectionHandler returns a connectionHandler, wrapping h2mux to make RPC calls
func newH2MuxHandler(ctx context.Context,
	streamHandler *streamhandler.StreamHandler,
	config *ConnectionConfig,
	edgeIP *net.TCPAddr,
) (connectionHandler, error) {
	// Inherit from parent context so we can cancel (Ctrl-C) while dialing
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	defer dialCancel()
	dialer := net.Dialer{DualStack: true}
	plaintextEdgeConn, err := dialer.DialContext(dialCtx, "tcp", edgeIP.String())
	if err != nil {
		return nil, dialError{cause: errors.Wrap(err, "DialContext error")}
	}
	edgeConn := tls.Client(plaintextEdgeConn, config.TLSConfig)
	edgeConn.SetDeadline(time.Now().Add(dialTimeout))
	err = edgeConn.Handshake()
	if err != nil {
		return nil, dialError{cause: errors.Wrap(err, "Handshake with edge error")}
	}
	// clear the deadline on the conn; h2mux has its own timeouts
	edgeConn.SetDeadline(time.Time{})
	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, h2mux.MuxerConfig{
		Timeout:           dialTimeout,
		Handler:           streamHandler,
		IsClient:          true,
		HeartbeatInterval: config.HeartbeatInterval,
		MaxHeartbeats:     config.MaxHeartbeats,
		Logger:            config.Logger,
	})
	if err != nil {
		return nil, err
	}
	return &h2muxHandler{
		muxer:  muxer,
		logger: config.Logger,
	}, nil
}

// connectionPool is a pool of connection handlers
type connectionPool struct {
	sync.Mutex
	connectionHandlers []connectionHandler
}

func (cp *connectionPool) put(h connectionHandler) {
	cp.Lock()
	defer cp.Unlock()
	cp.connectionHandlers = append(cp.connectionHandlers, h)
}

func (cp *connectionPool) close() {
	cp.Lock()
	defer cp.Unlock()
	for _, h := range cp.connectionHandlers {
		h.shutdown()
	}
}
