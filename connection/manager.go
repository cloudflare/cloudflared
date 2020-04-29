package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/streamhandler"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	quickStartLink       = "https://developers.cloudflare.com/argo-tunnel/quickstart/"
	faqLink              = "https://developers.cloudflare.com/argo-tunnel/faq/"
	defaultRetryAfter    = time.Second * 5
	packageNamespace     = "connection"
	edgeManagerSubsystem = "edgemanager"
)

// EdgeManager manages connections with the edge
type EdgeManager struct {
	// streamHandler handles stream opened by the edge
	streamHandler *streamhandler.StreamHandler
	// TLSConfig is the TLS configuration to connect with edge
	tlsConfig *tls.Config
	// cloudflaredConfig is the cloudflared configuration that is determined when the process first starts
	cloudflaredConfig *CloudflaredConfig
	// serviceDiscoverer returns the next edge addr to connect to
	serviceDiscoverer *edgediscovery.Edge
	// state is attributes of ConnectionManager that can change during runtime.
	state *edgeManagerState

	logger logger.Service

	metrics *metrics
}

type metrics struct {
	// activeStreams is a gauge shared by all muxers of this process to expose the total number of active streams
	activeStreams prometheus.Gauge
}

func newMetrics(namespace, subsystem string) *metrics {
	return &metrics{
		activeStreams: h2mux.NewActiveStreamsMetrics(namespace, subsystem),
	}
}

// EdgeManagerConfigurable is the configurable attributes of a EdgeConnectionManager
type EdgeManagerConfigurable struct {
	TunnelHostnames []h2mux.TunnelHostname
	*tunnelpogs.EdgeConnectionConfig
}

type CloudflaredConfig struct {
	CloudflaredID uuid.UUID
	Tags          []tunnelpogs.Tag
	BuildInfo     *buildinfo.BuildInfo
	IntentLabel   string
}

func NewEdgeManager(
	streamHandler *streamhandler.StreamHandler,
	edgeConnMgrConfigurable *EdgeManagerConfigurable,
	userCredential []byte,
	tlsConfig *tls.Config,
	serviceDiscoverer *edgediscovery.Edge,
	cloudflaredConfig *CloudflaredConfig,
	logger logger.Service,
) *EdgeManager {
	return &EdgeManager{
		streamHandler:     streamHandler,
		tlsConfig:         tlsConfig,
		cloudflaredConfig: cloudflaredConfig,
		serviceDiscoverer: serviceDiscoverer,
		state:             newEdgeConnectionManagerState(edgeConnMgrConfigurable, userCredential),
		logger:            logger,
		metrics:           newMetrics(packageNamespace, edgeManagerSubsystem),
	}
}

func (em *EdgeManager) Run(ctx context.Context) error {
	defer em.shutdown()

	// Currently, declarative tunnels don't have any concept of a stable connection
	// Each edge connection is transient and when it dies, it is replaced by a different one,
	// not restarted.
	// So in the future we should really change this so that n connections are stored individually
	connIndex := 0
	for {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "EdgeConnectionManager terminated")
		default:
			time.Sleep(1 * time.Second)
		}
		// Create/delete connection one at a time, so we don't need to adjust for connections that are being created/deleted
		// in shouldCreateConnection or shouldReduceConnection calculation
		if em.state.shouldCreateConnection(em.serviceDiscoverer.AvailableAddrs()) {
			if connErr := em.newConnection(ctx, connIndex); connErr != nil {
				if !connErr.ShouldRetry {
					em.logger.Errorf("connectionManager: %s with error: %s", em.noRetryMessage(), connErr)
					return connErr
				}
				em.logger.Errorf("connectionManager: cannot create new connection: %s", connErr)
			} else {
				connIndex++
			}
		} else if em.state.shouldReduceConnection() {
			if err := em.closeConnection(ctx); err != nil {
				em.logger.Errorf("connectionManager: cannot close connection: %s", err)
			}
		}
	}
}

func (em *EdgeManager) UpdateConfigurable(newConfigurable *EdgeManagerConfigurable) {
	em.logger.Infof("New edge connection manager configuration %+v", newConfigurable)
	em.state.updateConfigurable(newConfigurable)
}

func (em *EdgeManager) newConnection(ctx context.Context, index int) *tunnelpogs.ConnectError {
	edgeTCPAddr, err := em.serviceDiscoverer.GetAddr(index)
	if err != nil {
		return retryConnection(fmt.Sprintf("edge address discovery error: %v", err))
	}
	configurable := em.state.getConfigurable()
	edgeConn, err := DialEdge(ctx, configurable.Timeout, em.tlsConfig, edgeTCPAddr)
	if err != nil {
		return retryConnection(fmt.Sprintf("dial edge error: %v", err))
	}
	// Establish a muxed connection with the edge
	// Client mux handshake with agent server
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, h2mux.MuxerConfig{
		Timeout:           configurable.Timeout,
		Handler:           em.streamHandler,
		IsClient:          true,
		HeartbeatInterval: configurable.HeartbeatInterval,
		MaxHeartbeats:     configurable.MaxFailedHeartbeats,
		Logger:            em.logger,
	}, em.metrics.activeStreams)
	if err != nil {
		retryConnection(fmt.Sprintf("couldn't perform handshake with edge: %v", err))
	}

	h2muxConn, err := newConnection(muxer, edgeTCPAddr)
	if err != nil {
		return retryConnection(fmt.Sprintf("couldn't create h2mux connection: %v", err))
	}

	go em.serveConn(ctx, h2muxConn)

	connResult, err := h2muxConn.Connect(ctx, &tunnelpogs.ConnectParameters{
		CloudflaredID:       em.cloudflaredConfig.CloudflaredID,
		CloudflaredVersion:  em.cloudflaredConfig.BuildInfo.CloudflaredVersion,
		NumPreviousAttempts: 0,
		OriginCert:          em.state.getUserCredential(),
		IntentLabel:         em.cloudflaredConfig.IntentLabel,
		Tags:                em.cloudflaredConfig.Tags,
	}, em.logger)
	if err != nil {
		h2muxConn.Shutdown()
		return retryConnection(fmt.Sprintf("couldn't connect to edge: %v", err))
	}

	if connErr := connResult.ConnectError(); connErr != nil {
		return connErr
	}

	em.state.newConnection(h2muxConn)
	em.logger.Infof("connectionManager: connected to %s", connResult.ConnectedTo())

	if connResult.ClientConfig() != nil {
		em.streamHandler.UseConfiguration(ctx, connResult.ClientConfig())
	}
	return nil
}

func (em *EdgeManager) closeConnection(ctx context.Context) error {
	conn := em.state.getFirstConnection()
	if conn == nil {
		return fmt.Errorf("no connection to close")
	}
	conn.Shutdown()
	// teardown will be handled by EdgeManager.serveConn in another goroutine
	return nil
}

func (em *EdgeManager) serveConn(ctx context.Context, conn *Connection) {
	err := conn.Serve(ctx)
	em.logger.Errorf("connectionManager: Connection closed: %s", err)
	em.state.closeConnection(conn)
	em.serviceDiscoverer.GiveBack(conn.addr)
}

func (em *EdgeManager) noRetryMessage() string {
	messageTemplate := "cloudflared could not register an Argo Tunnel on your account. Please confirm the following before trying again:" +
		"1. You have Argo Smart Routing enabled in your account, See Enable Argo section of %s." +
		"2. Your credential at %s is still valid. See %s."
	return fmt.Sprintf(messageTemplate, quickStartLink, em.state.getConfigurable().UserCredentialPath, faqLink)
}

func (em *EdgeManager) shutdown() {
	em.state.shutdown()
}

type edgeManagerState struct {
	sync.RWMutex
	configurable   *EdgeManagerConfigurable
	userCredential []byte
	conns          map[uuid.UUID]*Connection
}

func newEdgeConnectionManagerState(configurable *EdgeManagerConfigurable, userCredential []byte) *edgeManagerState {
	return &edgeManagerState{
		configurable:   configurable,
		userCredential: userCredential,
		conns:          make(map[uuid.UUID]*Connection),
	}
}

func (ems *edgeManagerState) shouldCreateConnection(availableEdgeAddrs int) bool {
	ems.RLock()
	defer ems.RUnlock()
	expectedHAConns := int(ems.configurable.NumHAConnections)
	if availableEdgeAddrs < expectedHAConns {
		expectedHAConns = availableEdgeAddrs
	}
	return len(ems.conns) < expectedHAConns
}

func (ems *edgeManagerState) shouldReduceConnection() bool {
	ems.RLock()
	defer ems.RUnlock()
	return uint8(len(ems.conns)) > ems.configurable.NumHAConnections
}

func (ems *edgeManagerState) newConnection(conn *Connection) {
	ems.Lock()
	defer ems.Unlock()
	ems.conns[conn.id] = conn
}

func (ems *edgeManagerState) closeConnection(conn *Connection) {
	ems.Lock()
	defer ems.Unlock()
	delete(ems.conns, conn.id)
}

func (ems *edgeManagerState) getFirstConnection() *Connection {
	ems.RLock()
	defer ems.RUnlock()

	for _, conn := range ems.conns {
		return conn
	}
	return nil
}

func (ems *edgeManagerState) shutdown() {
	ems.Lock()
	defer ems.Unlock()
	for _, conn := range ems.conns {
		conn.Shutdown()
	}
}

func (ems *edgeManagerState) getConfigurable() *EdgeManagerConfigurable {
	ems.Lock()
	defer ems.Unlock()
	return ems.configurable
}

func (ems *edgeManagerState) updateConfigurable(newConfigurable *EdgeManagerConfigurable) {
	ems.Lock()
	defer ems.Unlock()
	ems.configurable = newConfigurable
}

func (ems *edgeManagerState) getUserCredential() []byte {
	ems.RLock()
	defer ems.RUnlock()
	return ems.userCredential
}

func retryConnection(cause string) *tunnelpogs.ConnectError {
	return &tunnelpogs.ConnectError{
		Cause:       cause,
		RetryAfter:  defaultRetryAfter,
		ShouldRetry: true,
	}
}
