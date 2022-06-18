package connection

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/rs/zerolog"
	"zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type tunnelServerClient struct {
	client    tunnelpogs.TunnelServer_PogsClient
	transport rpc.Transport
}

// NewTunnelRPCClient creates and returns a new RPC client, which will communicate using a stream on the given muxer.
// This method is exported for supervisor to call Authenticate RPC
func NewTunnelServerClient(
	ctx context.Context,
	stream io.ReadWriteCloser,
	log *zerolog.Logger,
) *tunnelServerClient {
	transport := tunnelrpc.NewTransportLogger(log, rpc.StreamTransport(stream))
	conn := rpc.NewConn(
		transport,
		tunnelrpc.ConnLog(log),
	)
	registrationClient := tunnelpogs.RegistrationServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn}
	return &tunnelServerClient{
		client:    tunnelpogs.TunnelServer_PogsClient{RegistrationServer_PogsClient: registrationClient, Client: conn.Bootstrap(ctx), Conn: conn},
		transport: transport,
	}
}

func (tsc *tunnelServerClient) Authenticate(ctx context.Context, classicTunnel *ClassicTunnelProperties, registrationOptions *tunnelpogs.RegistrationOptions) (tunnelpogs.AuthOutcome, error) {
	authResp, err := tsc.client.Authenticate(ctx, classicTunnel.OriginCert, classicTunnel.Hostname, registrationOptions)
	if err != nil {
		return nil, err
	}
	return authResp.Outcome(), nil
}

func (tsc *tunnelServerClient) Close() {
	// Closing the client will also close the connection
	_ = tsc.client.Close()
	_ = tsc.transport.Close()
}

type NamedTunnelRPCClient interface {
	RegisterConnection(
		c context.Context,
		config *NamedTunnelProperties,
		options *tunnelpogs.ConnectionOptions,
		connIndex uint8,
		edgeAddress net.IP,
		observer *Observer,
	) (*tunnelpogs.ConnectionDetails, error)
	SendLocalConfiguration(
		c context.Context,
		config []byte,
		observer *Observer,
	) error
	GracefulShutdown(ctx context.Context, gracePeriod time.Duration)
	Close()
}

type registrationServerClient struct {
	client    tunnelpogs.RegistrationServer_PogsClient
	transport rpc.Transport
}

func newRegistrationRPCClient(
	ctx context.Context,
	stream io.ReadWriteCloser,
	log *zerolog.Logger,
) NamedTunnelRPCClient {
	transport := tunnelrpc.NewTransportLogger(log, rpc.StreamTransport(stream))
	conn := rpc.NewConn(
		transport,
		tunnelrpc.ConnLog(log),
	)
	return &registrationServerClient{
		client:    tunnelpogs.RegistrationServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn},
		transport: transport,
	}
}

func (rsc *registrationServerClient) RegisterConnection(
	ctx context.Context,
	properties *NamedTunnelProperties,
	options *tunnelpogs.ConnectionOptions,
	connIndex uint8,
	edgeAddress net.IP,
	observer *Observer,
) (*tunnelpogs.ConnectionDetails, error) {
	conn, err := rsc.client.RegisterConnection(
		ctx,
		properties.Credentials.Auth(),
		properties.Credentials.TunnelID,
		connIndex,
		options,
	)
	if err != nil {
		if err.Error() == DuplicateConnectionError {
			observer.metrics.regFail.WithLabelValues("dup_edge_conn", "registerConnection").Inc()
			return nil, errDuplicationConnection
		}
		observer.metrics.regFail.WithLabelValues("server_error", "registerConnection").Inc()
		return nil, serverRegistrationErrorFromRPC(err)
	}

	observer.metrics.regSuccess.WithLabelValues("registerConnection").Inc()

	observer.logServerInfo(connIndex, conn.Location, edgeAddress, fmt.Sprintf("Connection %s registered", conn.UUID))
	observer.sendConnectedEvent(connIndex, conn.Location)

	return conn, nil
}

func (rsc *registrationServerClient) SendLocalConfiguration(ctx context.Context, config []byte, observer *Observer) (err error) {
	observer.metrics.localConfigMetrics.pushes.Inc()
	defer func() {
		if err != nil {
			observer.metrics.localConfigMetrics.pushesErrors.Inc()
		}
	}()

	return rsc.client.SendLocalConfiguration(ctx, config)
}

func (rsc *registrationServerClient) GracefulShutdown(ctx context.Context, gracePeriod time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()
	_ = rsc.client.UnregisterConnection(ctx)
}

func (rsc *registrationServerClient) Close() {
	// Closing the client will also close the connection
	_ = rsc.client.Close()
	// Closing the transport also closes the stream
	_ = rsc.transport.Close()
}

type rpcName string

const (
	register     rpcName = "register"
	reconnect    rpcName = "reconnect"
	unregister   rpcName = "unregister"
	authenticate rpcName = " authenticate"
)

func (h *h2muxConnection) registerTunnel(ctx context.Context, credentialSetter CredentialManager, classicTunnel *ClassicTunnelProperties, registrationOptions *tunnelpogs.RegistrationOptions) error {
	h.observer.sendRegisteringEvent(registrationOptions.ConnectionID)

	stream, err := h.newRPCStream(ctx, register)
	if err != nil {
		return err
	}
	rpcClient := NewTunnelServerClient(ctx, stream, h.observer.log)
	defer rpcClient.Close()

	_ = h.logServerInfo(ctx, rpcClient)
	registration := rpcClient.client.RegisterTunnel(
		ctx,
		classicTunnel.OriginCert,
		classicTunnel.Hostname,
		registrationOptions,
	)
	if registrationErr := registration.DeserializeError(); registrationErr != nil {
		// RegisterTunnel RPC failure
		return h.processRegisterTunnelError(registrationErr, register)
	}

	credentialSetter.SetEventDigest(h.connIndex, registration.EventDigest)
	return h.processRegistrationSuccess(registration, register, credentialSetter, classicTunnel)
}

type CredentialManager interface {
	ReconnectToken() ([]byte, error)
	EventDigest(connID uint8) ([]byte, error)
	SetEventDigest(connID uint8, digest []byte)
	ConnDigest(connID uint8) ([]byte, error)
	SetConnDigest(connID uint8, digest []byte)
}

func (h *h2muxConnection) processRegistrationSuccess(
	registration *tunnelpogs.TunnelRegistration,
	name rpcName,
	credentialManager CredentialManager, classicTunnel *ClassicTunnelProperties,
) error {
	for _, logLine := range registration.LogLines {
		h.observer.log.Info().Msg(logLine)
	}

	if registration.TunnelID != "" {
		h.observer.metrics.tunnelsHA.AddTunnelID(h.connIndex, registration.TunnelID)
		h.observer.log.Info().Msgf("Each HA connection's tunnel IDs: %v", h.observer.metrics.tunnelsHA.String())
	}

	credentialManager.SetConnDigest(h.connIndex, registration.ConnDigest)
	h.observer.metrics.userHostnamesCounts.WithLabelValues(registration.Url).Inc()

	h.observer.log.Info().Msgf("Route propagating, it may take up to 1 minute for your new route to become functional")
	h.observer.metrics.regSuccess.WithLabelValues(string(name)).Inc()
	return nil
}

func (h *h2muxConnection) processRegisterTunnelError(err tunnelpogs.TunnelRegistrationError, name rpcName) error {
	if err.Error() == DuplicateConnectionError {
		h.observer.metrics.regFail.WithLabelValues("dup_edge_conn", string(name)).Inc()
		return errDuplicationConnection
	}
	h.observer.metrics.regFail.WithLabelValues("server_error", string(name)).Inc()
	return ServerRegisterTunnelError{
		Cause:     err,
		Permanent: err.IsPermanent(),
	}
}

func (h *h2muxConnection) reconnectTunnel(ctx context.Context, credentialManager CredentialManager, classicTunnel *ClassicTunnelProperties, registrationOptions *tunnelpogs.RegistrationOptions) error {
	token, err := credentialManager.ReconnectToken()
	if err != nil {
		return err
	}
	eventDigest, err := credentialManager.EventDigest(h.connIndex)
	if err != nil {
		return err
	}
	connDigest, err := credentialManager.ConnDigest(h.connIndex)
	if err != nil {
		return err
	}

	h.observer.log.Debug().Msg("initiating RPC stream to reconnect")
	stream, err := h.newRPCStream(ctx, register)
	if err != nil {
		return err
	}
	rpcClient := NewTunnelServerClient(ctx, stream, h.observer.log)
	defer rpcClient.Close()

	_ = h.logServerInfo(ctx, rpcClient)
	registration := rpcClient.client.ReconnectTunnel(
		ctx,
		token,
		eventDigest,
		connDigest,
		classicTunnel.Hostname,
		registrationOptions,
	)
	if registrationErr := registration.DeserializeError(); registrationErr != nil {
		// ReconnectTunnel RPC failure
		return h.processRegisterTunnelError(registrationErr, reconnect)
	}
	return h.processRegistrationSuccess(registration, reconnect, credentialManager, classicTunnel)
}

func (h *h2muxConnection) logServerInfo(ctx context.Context, rpcClient *tunnelServerClient) error {
	// Request server info without blocking tunnel registration; must use capnp library directly.
	serverInfoPromise := tunnelrpc.TunnelServer{Client: rpcClient.client.Client}.GetServerInfo(ctx, func(tunnelrpc.TunnelServer_getServerInfo_Params) error {
		return nil
	})
	serverInfoMessage, err := serverInfoPromise.Result().Struct()
	if err != nil {
		h.observer.log.Err(err).Msg("Failed to retrieve server information")
		return err
	}
	serverInfo, err := tunnelpogs.UnmarshalServerInfo(serverInfoMessage)
	if err != nil {
		h.observer.log.Err(err).Msg("Failed to retrieve server information")
		return err
	}
	h.observer.logServerInfo(h.connIndex, serverInfo.LocationName, net.IP{}, "Connection established")
	return nil
}

func (h *h2muxConnection) registerNamedTunnel(
	ctx context.Context,
	namedTunnel *NamedTunnelProperties,
	connOptions *tunnelpogs.ConnectionOptions,
) error {
	stream, err := h.newRPCStream(ctx, register)
	if err != nil {
		return err
	}
	rpcClient := h.newRPCClientFunc(ctx, stream, h.observer.log)
	defer rpcClient.Close()

	if _, err = rpcClient.RegisterConnection(ctx, namedTunnel, connOptions, h.connIndex, nil, h.observer); err != nil {
		return err
	}
	return nil
}

func (h *h2muxConnection) unregister(isNamedTunnel bool) {
	h.observer.sendUnregisteringEvent(h.connIndex)

	unregisterCtx, cancel := context.WithTimeout(context.Background(), h.gracePeriod)
	defer cancel()

	stream, err := h.newRPCStream(unregisterCtx, unregister)
	if err != nil {
		return
	}
	defer stream.Close()

	if isNamedTunnel {
		rpcClient := h.newRPCClientFunc(unregisterCtx, stream, h.observer.log)
		defer rpcClient.Close()

		rpcClient.GracefulShutdown(unregisterCtx, h.gracePeriod)
	} else {
		rpcClient := NewTunnelServerClient(unregisterCtx, stream, h.observer.log)
		defer rpcClient.Close()

		// gracePeriod is encoded in int64 using capnproto
		_ = rpcClient.client.UnregisterTunnel(unregisterCtx, h.gracePeriod.Nanoseconds())
	}

	h.observer.log.Info().Uint8(LogFieldConnIndex, h.connIndex).Msg("Unregistered tunnel connection")
}
