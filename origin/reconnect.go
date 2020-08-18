package origin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errJWTUnset = errors.New("JWT unset")
)

// reconnectTunnelCredentialManager is invoked by functions in tunnel.go to
// get/set parameters for ReconnectTunnel RPC calls.
type reconnectCredentialManager struct {
	mu          sync.RWMutex
	jwt         []byte
	eventDigest map[uint8][]byte
	connDigest  map[uint8][]byte
	authSuccess prometheus.Counter
	authFail    *prometheus.CounterVec
}

func newReconnectCredentialManager(namespace, subsystem string, haConnections int) *reconnectCredentialManager {
	authSuccess := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tunnel_authenticate_success",
			Help:      "Count of successful tunnel authenticate",
		},
	)
	authFail := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tunnel_authenticate_fail",
			Help:      "Count of tunnel authenticate errors by type",
		},
		[]string{"error"},
	)
	prometheus.MustRegister(authSuccess, authFail)
	return &reconnectCredentialManager{
		eventDigest: make(map[uint8][]byte, haConnections),
		connDigest:  make(map[uint8][]byte, haConnections),
		authSuccess: authSuccess,
		authFail:    authFail,
	}
}

func (cm *reconnectCredentialManager) ReconnectToken() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.jwt == nil {
		return nil, errJWTUnset
	}
	return cm.jwt, nil
}

func (cm *reconnectCredentialManager) SetReconnectToken(jwt []byte) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.jwt = jwt
}

func (cm *reconnectCredentialManager) EventDigest(connID uint8) ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	digest, ok := cm.eventDigest[connID]
	if !ok {
		return nil, fmt.Errorf("no event digest for connection %v", connID)
	}
	return digest, nil
}

func (cm *reconnectCredentialManager) SetEventDigest(connID uint8, digest []byte) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.eventDigest[connID] = digest
}

func (cm *reconnectCredentialManager) ConnDigest(connID uint8) ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	digest, ok := cm.connDigest[connID]
	if !ok {
		return nil, fmt.Errorf("no conneciton digest for connection %v", connID)
	}
	return digest, nil
}

func (cm *reconnectCredentialManager) SetConnDigest(connID uint8, digest []byte) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.connDigest[connID] = digest
}

func (cm *reconnectCredentialManager) RefreshAuth(
	ctx context.Context,
	backoff *BackoffHandler,
	authenticate func(ctx context.Context, numPreviousAttempts int) (tunnelpogs.AuthOutcome, error),
) (retryTimer <-chan time.Time, err error) {
	authOutcome, err := authenticate(ctx, backoff.Retries())
	if err != nil {
		cm.authFail.WithLabelValues(err.Error()).Inc()
		if _, ok := backoff.GetBackoffDuration(ctx); ok {
			return backoff.BackoffTimer(), nil
		}
		return nil, err
	}
	// clear backoff timer
	backoff.SetGracePeriod()

	switch outcome := authOutcome.(type) {
	case tunnelpogs.AuthSuccess:
		cm.SetReconnectToken(outcome.JWT())
		cm.authSuccess.Inc()
		return timeAfter(outcome.RefreshAfter()), nil
	case tunnelpogs.AuthUnknown:
		duration := outcome.RefreshAfter()
		cm.authFail.WithLabelValues(outcome.Error()).Inc()
		return timeAfter(duration), nil
	case tunnelpogs.AuthFail:
		cm.authFail.WithLabelValues(outcome.Error()).Inc()
		return nil, outcome
	default:
		err := fmt.Errorf("refresh_auth: Unexpected outcome type %T", authOutcome)
		cm.authFail.WithLabelValues(err.Error()).Inc()
		return nil, err
	}
}

func ReconnectTunnel(
	ctx context.Context,
	muxer *h2mux.Muxer,
	config *TunnelConfig,
	logger logger.Service,
	connectionID uint8,
	originLocalAddr string,
	uuid uuid.UUID,
	credentialManager *reconnectCredentialManager,
) error {
	token, err := credentialManager.ReconnectToken()
	if err != nil {
		return err
	}
	eventDigest, err := credentialManager.EventDigest(connectionID)
	if err != nil {
		return err
	}
	connDigest, err := credentialManager.ConnDigest(connectionID)
	if err != nil {
		return err
	}

	config.TransportLogger.Debug("initiating RPC stream to reconnect")
	tunnelServer, err := connection.NewRPCClient(ctx, muxer, config.TransportLogger, openStreamTimeout)
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
		connDigest,
		config.Hostname,
		config.RegistrationOptions(connectionID, originLocalAddr, uuid),
	)
	if registrationErr := registration.DeserializeError(); registrationErr != nil {
		// ReconnectTunnel RPC failure
		return processRegisterTunnelError(registrationErr, config.Metrics, reconnect)
	}
	return processRegistrationSuccess(config, logger, connectionID, registration, reconnect, credentialManager)
}
