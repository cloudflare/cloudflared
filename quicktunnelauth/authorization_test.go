package quicktunnelauth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"net/http"
	"testing"
	"testing/iotest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testQuickTunnelAuthHandlerHarness struct {
	handler            *QuickTunnelAuthHandler
	stateManager       *QuickTunnelAuthStateManager
	assertionValidator *QuickTunnelAuthAssertionValidator
	sessionManager     *QuickTunnelAuthSessionManager
	privateKey         *ecdsa.PrivateKey
	verificationKey    jose.JSONWebKey
	now                time.Time
}

func TestNewQuickTunnelAuthHandlerWithAuthorization(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"visitor@example.com"})
	assert.Same(t, harness.stateManager, harness.handler.stateManager)
	assert.Same(t, harness.assertionValidator, harness.handler.assertionValidator)
	assert.Same(t, harness.sessionManager, harness.handler.sessionManager)
	require.NotNil(t, harness.handler.recipientPolicy)

	tests := []struct {
		name               string
		stateManager       *QuickTunnelAuthStateManager
		assertionValidator *QuickTunnelAuthAssertionValidator
		sessionManager     *QuickTunnelAuthSessionManager
		recipientPolicy    *QuickTunnelAuthRecipientPolicy
		expectedError      string
	}{
		{
			name:               "missing state manager",
			assertionValidator: harness.assertionValidator,
			sessionManager:     harness.sessionManager,
			recipientPolicy:    harness.handler.recipientPolicy,
			expectedError:      "state manager cannot be nil",
		},
		{
			name:            "missing assertion validator",
			stateManager:    harness.stateManager,
			sessionManager:  harness.sessionManager,
			recipientPolicy: harness.handler.recipientPolicy,
			expectedError:   "assertion validator cannot be nil",
		},
		{
			name:               "missing session manager",
			stateManager:       harness.stateManager,
			assertionValidator: harness.assertionValidator,
			recipientPolicy:    harness.handler.recipientPolicy,
			expectedError:      "session manager cannot be nil",
		},
		{
			name:               "missing recipient policy",
			stateManager:       harness.stateManager,
			assertionValidator: harness.assertionValidator,
			sessionManager:     harness.sessionManager,
			expectedError:      "recipient policy cannot be nil",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewQuickTunnelAuthHandlerWithAuthorization(
				test.stateManager,
				test.assertionValidator,
				test.sessionManager,
				test.recipientPolicy,
			)
			require.ErrorContains(t, err, test.expectedError)
			assert.Nil(t, handler)
		})
	}
}

func TestQuickTunnelAuthHandlerAuthorizeCallbackRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"visitor@example.com"})

	var ctx context.Context
	cookie, err := harness.handler.authorizeCallback(ctx, &QuickTunnelAuthCallback{})
	require.ErrorContains(t, err, "context is nil")
	assert.Nil(t, cookie)

	cookie, err = harness.handler.authorizeCallback(context.Background(), nil)
	require.ErrorContains(t, err, "callback is nil")
	assert.Nil(t, cookie)
}

func TestQuickTunnelAuthHandlerAuthorizeCallbackRejectsSessionFailure(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"visitor@example.com"})
	login := beginTestQuickTunnelLogin(t, harness.stateManager, "/dashboard")
	harness.sessionManager.random = iotest.ErrReader(assert.AnError)
	callback := &QuickTunnelAuthCallback{
		State:     login.State,
		Assertion: harness.signAssertion(t, login.State, "visitor@example.com"),
	}

	cookie, err := harness.handler.authorizeCallback(context.Background(), callback)
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, cookie)
}

func newTestQuickTunnelAuthHandlerHarness(
	t *testing.T,
	allowedMail []string,
) *testQuickTunnelAuthHandlerHarness {
	t.Helper()

	now := testQuickTunnelAuthNow
	stateManager := newTestQuickTunnelAuthStateManager(t)
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "callback-key")
	assertionValidator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
	t.Cleanup(assertionValidator.Close)
	sessionManager := newTestQuickTunnelAuthSessionManager(
		now,
		0x42,
		bytes.NewReader(make([]byte, quickTunnelAuthSessionNonceSize*4)),
	)
	recipientPolicy, err := NewQuickTunnelAuthRecipientPolicy(allowedMail)
	require.NoError(t, err)
	handler, err := NewQuickTunnelAuthHandlerWithAuthorization(
		stateManager,
		assertionValidator,
		sessionManager,
		recipientPolicy,
	)
	require.NoError(t, err)

	return &testQuickTunnelAuthHandlerHarness{
		handler:            handler,
		stateManager:       stateManager,
		assertionValidator: assertionValidator,
		sessionManager:     sessionManager,
		privateKey:         privateKey,
		verificationKey:    verificationKey,
		now:                now,
	}
}

func (h *testQuickTunnelAuthHandlerHarness) newCallbackRequest(
	t *testing.T,
	returnPath string,
	email string,
) (*http.Request, *QuickTunnelAuthLogin) {
	t.Helper()

	login := beginTestQuickTunnelLogin(t, h.stateManager, returnPath)
	assertion := h.signAssertion(t, login.State, email)
	return newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, assertion), login
}

func (h *testQuickTunnelAuthHandlerHarness) signAssertion(t *testing.T, state, email string) string {
	t.Helper()

	claims := newTestQuickTunnelAuthBrokerClaims(h.now, h.stateManager.hostname, state)
	claims.Email = email
	return signTestQuickTunnelAuthBrokerAssertion(
		t,
		jose.ES256,
		h.privateKey,
		h.verificationKey.KeyID,
		quickTunnelAuthBrokerJWTHeaderType,
		false,
		claims,
	)
}
