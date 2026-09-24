package quicktunnelauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuickTunnelAuthAssertionValidatorValidateRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	validState := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	validHostname := "test-tunnel.trycloudflare.com"

	tests := []struct {
		name             string
		validator        *QuickTunnelAuthAssertionValidator
		ctx              context.Context
		assertion        string
		expectedHostname string
		expectedState    string
		expectedError    string
	}{
		{name: "nil validator", ctx: context.Background(), assertion: "header.payload.signature", expectedHostname: validHostname, expectedState: validState, expectedError: "authentication assertion validator cannot be nil"},
		{name: "nil context", validator: validator, assertion: "header.payload.signature", expectedHostname: validHostname, expectedState: validState, expectedError: "authentication assertion validation context cannot be nil"},
		{name: "empty assertion", validator: validator, ctx: context.Background(), expectedHostname: validHostname, expectedState: validState, expectedError: "broker assertion cannot be empty"},
		{name: "invalid hostname", validator: validator, ctx: context.Background(), assertion: "header.payload.signature", expectedHostname: "example.com", expectedState: validState, expectedError: "expected callback hostname is not a valid Quick Tunnel hostname"},
		{name: "invalid state encoding", validator: validator, ctx: context.Background(), assertion: "header.payload.signature", expectedHostname: validHostname, expectedState: "not-base64!", expectedError: "expected authentication state is invalid"},
		{name: "invalid state size", validator: validator, ctx: context.Background(), assertion: "header.payload.signature", expectedHostname: validHostname, expectedState: base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize-1)), expectedError: "expected authentication state is invalid"},
		{name: "non-canonical state", validator: validator, ctx: context.Background(), assertion: "header.payload.signature", expectedHostname: validHostname, expectedState: validState + "=", expectedError: "expected authentication state is invalid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			identity, err := test.validator.Validate(test.ctx, test.assertion, test.expectedHostname, test.expectedState)
			require.EqualError(t, err, test.expectedError)
			assert.Nil(t, identity)
		})
	}
}

func TestQuickTunnelAuthAssertionValidatorValidateAcceptsValidAssertion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")
	validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
	claims := newTestQuickTunnelAuthBrokerClaims(now, hostname, state)
	claims.Email = "Visitor@Example.COM"
	assertion := signTestQuickTunnelAuthBrokerAssertion(t, jose.ES256, privateKey, verificationKey.KeyID, quickTunnelAuthBrokerJWTHeaderType, false, claims)

	identity, err := validator.Validate(context.Background(), assertion, hostname, state)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "visitor@example.com", identity.Email)
	assert.Equal(t, time.Unix(*claims.IdentityExpiresAt, 0).UTC(), identity.ExpiresAt)
}

func TestQuickTunnelAuthAssertionValidatorValidateAcceptsAllowedClockSkew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")
	validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
	claims := newTestQuickTunnelAuthBrokerClaims(now, hostname, state)
	claims.IssuedAt = jwt.NewNumericDate(now.Add(quickTunnelAuthBrokerClockSkew))
	assertion := signTestQuickTunnelAuthBrokerAssertion(t, jose.ES256, privateKey, verificationKey.KeyID, quickTunnelAuthBrokerJWTHeaderType, false, claims)

	identity, err := validator.Validate(context.Background(), assertion, hostname, state)
	require.NoError(t, err)
	assert.NotNil(t, identity)
}

func TestQuickTunnelAuthAssertionValidatorValidateAcceptsEqualIssuedAtAndExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")
	validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
	claims := newTestQuickTunnelAuthBrokerClaims(now, hostname, state)
	boundary := jwt.NewNumericDate(now.Add(quickTunnelAuthBrokerClockSkew / 2))
	claims.IssuedAt = boundary
	claims.Expiry = boundary
	assertion := signTestQuickTunnelAuthBrokerAssertion(t, jose.ES256, privateKey, verificationKey.KeyID, quickTunnelAuthBrokerJWTHeaderType, false, claims)

	identity, err := validator.Validate(context.Background(), assertion, hostname, state)
	require.NoError(t, err)
	assert.NotNil(t, identity)
}

func TestQuickTunnelAuthAssertionValidatorValidateRejectsInvalidHeader(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")
	claims := newTestQuickTunnelAuthBrokerClaims(now, hostname, state)

	tests := []struct {
		name          string
		algorithm     jose.SignatureAlgorithm
		signingKey    any
		keyID         string
		headerType    string
		embedJWK      bool
		expectedError string
	}{
		{name: "wrong algorithm", algorithm: jose.HS256, signingKey: make([]byte, sha256.Size), keyID: verificationKey.KeyID, headerType: quickTunnelAuthBrokerJWTHeaderType, expectedError: "parse broker assertion"},
		{name: "missing key ID", algorithm: jose.ES256, signingKey: privateKey, headerType: quickTunnelAuthBrokerJWTHeaderType, expectedError: "broker assertion key ID is invalid"},
		{name: "key ID with whitespace", algorithm: jose.ES256, signingKey: privateKey, keyID: " test-key", headerType: quickTunnelAuthBrokerJWTHeaderType, expectedError: "broker assertion key ID is invalid"},
		{name: "oversized key ID", algorithm: jose.ES256, signingKey: privateKey, keyID: strings.Repeat("k", quickTunnelAuthBrokerMaxKeyIDLength+1), headerType: quickTunnelAuthBrokerJWTHeaderType, expectedError: "broker assertion key ID is invalid"},
		{name: "missing type", algorithm: jose.ES256, signingKey: privateKey, keyID: verificationKey.KeyID, expectedError: "broker assertion type header must be JWT"},
		{name: "wrong type", algorithm: jose.ES256, signingKey: privateKey, keyID: verificationKey.KeyID, headerType: "JOSE", expectedError: "broker assertion type header must be JWT"},
		{name: "embedded key", algorithm: jose.ES256, signingKey: privateKey, headerType: quickTunnelAuthBrokerJWTHeaderType, embedJWK: true, expectedError: "broker assertion must not contain an embedded verification key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
			assertion := signTestQuickTunnelAuthBrokerAssertion(t, test.algorithm, test.signingKey, test.keyID, test.headerType, test.embedJWK, claims)
			identity, err := validator.Validate(context.Background(), assertion, hostname, state)
			require.ErrorContains(t, err, test.expectedError)
			assert.Nil(t, identity)
		})
	}
}

func TestQuickTunnelAuthAssertionValidatorValidateRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	_, trustedVerificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")
	untrustedPrivateKey, _ := newTestQuickTunnelAuthBrokerKeyPair(t, "other-key")
	validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, trustedVerificationKey)
	assertion := signTestQuickTunnelAuthBrokerAssertion(t, jose.ES256, untrustedPrivateKey, trustedVerificationKey.KeyID, quickTunnelAuthBrokerJWTHeaderType, false, newTestQuickTunnelAuthBrokerClaims(now, hostname, state))

	identity, err := validator.Validate(context.Background(), assertion, hostname, state)
	require.ErrorContains(t, err, "verify broker assertion")
	assert.Nil(t, identity)
}

func TestQuickTunnelAuthAssertionValidatorValidateRejectsInvalidClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)
	state := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthStateSize))
	otherState := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, quickTunnelAuthStateSize))
	hostname := "test-tunnel.trycloudflare.com"
	privateKey, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, "test-key")

	tests := []struct {
		name          string
		mutate        func(*quickTunnelAuthBrokerAssertionClaims)
		expectedError string
	}{
		{name: "missing issuer", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Issuer = "" }, expectedError: "broker assertion issuer is missing"},
		{name: "wrong issuer", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Issuer = "https://example.com" }, expectedError: "validate broker assertion standard claims"},
		{name: "missing audience", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Audience = nil }, expectedError: "broker assertion audience is missing"},
		{name: "wrong audience", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Audience = jwt.Audience{"other-audience"} }, expectedError: "validate broker assertion standard claims"},
		{name: "missing issued at", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.IssuedAt = nil }, expectedError: "broker assertion issued-at time is missing"},
		{name: "issued at beyond clock skew", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(quickTunnelAuthBrokerClockSkew + time.Second))
		}, expectedError: "validate broker assertion standard claims"},
		{name: "missing expiry", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Expiry = nil }, expectedError: "broker assertion expiry is missing"},
		{name: "expired", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Expiry = jwt.NewNumericDate(now) }, expectedError: "broker assertion has expired"},
		{name: "expiry before issued at", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(quickTunnelAuthBrokerClockSkew / 2))
			c.Expiry = jwt.NewNumericDate(now.Add(time.Second))
		}, expectedError: "broker assertion expiry must not be before its issued-at time"},
		{name: "lifetime too long", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) {
			c.Expiry = jwt.NewNumericDate(now.Add(quickTunnelAuthBrokerAssertionTTL + time.Second))
		}, expectedError: "broker assertion lifetime exceeds the maximum"},
		{name: "wrong assertion type", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Type = "other" }, expectedError: "broker assertion has an invalid type"},
		{name: "invalid callback hostname", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.CallbackHostname = "example.com" }, expectedError: "broker assertion callback hostname does not match"},
		{name: "mismatched callback hostname", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.CallbackHostname = "other.trycloudflare.com" }, expectedError: "broker assertion callback hostname does not match"},
		{name: "invalid state", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.State = "not-base64!" }, expectedError: "broker assertion state does not match"},
		{name: "mismatched state", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.State = otherState }, expectedError: "broker assertion state does not match"},
		{name: "invalid email", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.Email = "not-an-email" }, expectedError: "broker assertion contains an invalid email"},
		{name: "missing identity expiry", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.IdentityExpiresAt = nil }, expectedError: "broker assertion Access identity expiry is missing"},
		{name: "expired identity", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) { c.IdentityExpiresAt = int64Pointer(now.Unix()) }, expectedError: "broker assertion Access identity has expired"},
		{name: "assertion outlives identity", mutate: func(c *quickTunnelAuthBrokerAssertionClaims) {
			c.IdentityExpiresAt = int64Pointer(now.Add(30 * time.Second).Unix())
		}, expectedError: "broker assertion outlives the Access identity"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			claims := newTestQuickTunnelAuthBrokerClaims(now, hostname, state)
			test.mutate(&claims)
			validator := newTestQuickTunnelAuthAssertionValidatorWithKey(t, now, verificationKey)
			assertion := signTestQuickTunnelAuthBrokerAssertion(t, jose.ES256, privateKey, verificationKey.KeyID, quickTunnelAuthBrokerJWTHeaderType, false, claims)
			identity, err := validator.Validate(context.Background(), assertion, hostname, state)
			require.ErrorContains(t, err, test.expectedError)
			assert.Nil(t, identity)
		})
	}
}

func newTestQuickTunnelAuthAssertionValidatorWithKey(
	t *testing.T,
	now time.Time,
	key jose.JSONWebKey,
) *QuickTunnelAuthAssertionValidator {
	t.Helper()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	validator.now = func() time.Time { return now }
	validator.jwks.keySet = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}}
	validator.jwks.expiresAt = now.Add(quickTunnelAuthBrokerJWKSCacheTTL)
	return validator
}

func int64Pointer(value int64) *int64 {
	return &value
}

func newTestQuickTunnelAuthBrokerClaims(
	now time.Time,
	hostname string,
	state string,
) quickTunnelAuthBrokerAssertionClaims {
	return quickTunnelAuthBrokerAssertionClaims{
		Type:              quickTunnelAuthBrokerAssertionType,
		Email:             "visitor@example.com",
		CallbackHostname:  hostname,
		State:             state,
		IdentityExpiresAt: int64Pointer(now.Add(time.Hour).Unix()),
		Claims: jwt.Claims{
			Issuer:   quickTunnelAuthBrokerIssuer,
			Audience: jwt.Audience{quickTunnelAuthBrokerAudience},
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
}

func signTestQuickTunnelAuthBrokerAssertion(
	t *testing.T,
	algorithm jose.SignatureAlgorithm,
	signingKey any,
	keyID string,
	headerType string,
	embedJWK bool,
	claims quickTunnelAuthBrokerAssertionClaims,
) string {
	t.Helper()

	options := &jose.SignerOptions{EmbedJWK: embedJWK}
	if keyID != "" {
		options.WithHeader("kid", keyID)
	}
	if headerType != "" {
		options.WithType(jose.ContentType(headerType))
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: signingKey}, options)
	require.NoError(t, err)
	assertion, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return assertion
}
