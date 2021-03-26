package origin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/retry"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
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
	backoff *retry.BackoffHandler,
	authenticate func(ctx context.Context, numPreviousAttempts int) (tunnelpogs.AuthOutcome, error),
) (retryTimer <-chan time.Time, err error) {
	authOutcome, err := authenticate(ctx, backoff.Retries())
	if err != nil {
		cm.authFail.WithLabelValues(err.Error()).Inc()
		if _, ok := backoff.GetMaxBackoffDuration(ctx); ok {
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
		return retry.Clock.After(outcome.RefreshAfter()), nil
	case tunnelpogs.AuthUnknown:
		duration := outcome.RefreshAfter()
		cm.authFail.WithLabelValues(outcome.Error()).Inc()
		return retry.Clock.After(duration), nil
	case tunnelpogs.AuthFail:
		cm.authFail.WithLabelValues(outcome.Error()).Inc()
		return nil, outcome
	default:
		err := fmt.Errorf("refresh_auth: Unexpected outcome type %T", authOutcome)
		cm.authFail.WithLabelValues(err.Error()).Inc()
		return nil, err
	}
}
