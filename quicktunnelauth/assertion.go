package quicktunnelauth

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	quickTunnelAuthBrokerIssuer        = "https://login.trycloudflare.com"
	quickTunnelAuthBrokerAudience      = "cloudflared-quick-tunnel"
	quickTunnelAuthBrokerAssertionType = "quick_tunnel_auth"

	quickTunnelAuthBrokerAssertionTTL  = 2 * time.Minute
	quickTunnelAuthBrokerClockSkew     = 30 * time.Second
	quickTunnelAuthBrokerJWTHeaderType = "JWT"
)

// QuickTunnelAuthIdentity contains the broker-verified identity required by
// local Quick Tunnel recipient authorization.
type QuickTunnelAuthIdentity struct {
	Email     string
	ExpiresAt time.Time
}

type quickTunnelAuthBrokerAssertionClaims struct {
	Type             string `json:"type"`
	Email            string `json:"email"`
	CallbackHostname string `json:"callback_host"`
	State            string `json:"state"`
	// IdentityExpiresAt is the Access identity expiry, distinct from the assertion's short-lived expiry.
	IdentityExpiresAt *int64 `json:"identity_exp"`
	jwt.Claims
}

// Validate verifies a broker assertion and binds it to the expected Quick
// Tunnel hostname and browser authentication state.
func (v *QuickTunnelAuthAssertionValidator) Validate(
	ctx context.Context,
	assertion string,
	expectedHostname string,
	expectedState string,
) (*QuickTunnelAuthIdentity, error) {
	if v == nil {
		return nil, errors.New("authentication assertion validator cannot be nil")
	}
	if ctx == nil {
		return nil, errors.New("authentication assertion validation context cannot be nil")
	}
	if assertion == "" {
		return nil, errors.New("broker assertion cannot be empty")
	}
	if !isQuickTunnelHostname(expectedHostname) {
		return nil, errors.New("expected callback hostname is not a valid Quick Tunnel hostname")
	}
	if !isCanonicalQuickTunnelAuthAssertionState(expectedState) {
		return nil, errors.New("expected authentication state is invalid")
	}

	parsedToken, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, fmt.Errorf("parse broker assertion: %w", err)
	}
	if len(parsedToken.Headers) != 1 {
		return nil, errors.New("broker assertion must contain exactly one protected header")
	}

	header := parsedToken.Headers[0]
	if err := validateQuickTunnelAuthBrokerHeader(header); err != nil {
		return nil, err
	}

	verificationKey, err := v.verificationKey(ctx, header.KeyID)
	if err != nil {
		return nil, err
	}

	var claims quickTunnelAuthBrokerAssertionClaims
	if err := parsedToken.Claims(verificationKey.Key, &claims); err != nil {
		return nil, fmt.Errorf("verify broker assertion: %w", err)
	}

	return v.validateClaims(&claims, expectedHostname, expectedState)
}

func validateQuickTunnelAuthBrokerHeader(header jose.Header) error {
	if header.JSONWebKey != nil {
		return errors.New("broker assertion must not contain an embedded verification key")
	}
	if header.KeyID == "" ||
		len(header.KeyID) > quickTunnelAuthBrokerMaxKeyIDLength ||
		strings.TrimSpace(header.KeyID) != header.KeyID {
		return errors.New("broker assertion key ID is invalid")
	}

	headerType, ok := header.ExtraHeaders[jose.HeaderType].(string)
	if !ok || headerType != quickTunnelAuthBrokerJWTHeaderType {
		return errors.New("broker assertion type header must be JWT")
	}
	return nil
}

func (v *QuickTunnelAuthAssertionValidator) validateClaims(
	claims *quickTunnelAuthBrokerAssertionClaims,
	expectedHostname string,
	expectedState string,
) (*QuickTunnelAuthIdentity, error) {
	if claims == nil {
		return nil, errors.New("broker assertion claims cannot be nil")
	}
	if claims.Issuer == "" {
		return nil, errors.New("broker assertion issuer is missing")
	}
	if len(claims.Audience) == 0 {
		return nil, errors.New("broker assertion audience is missing")
	}
	if claims.IssuedAt == nil {
		return nil, errors.New("broker assertion issued-at time is missing")
	}
	if claims.Expiry == nil {
		return nil, errors.New("broker assertion expiry is missing")
	}
	if claims.IdentityExpiresAt == nil {
		return nil, errors.New("broker assertion Access identity expiry is missing")
	}

	now := v.now().UTC()
	if err := claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      quickTunnelAuthBrokerIssuer,
		AnyAudience: jwt.Audience{quickTunnelAuthBrokerAudience},
		Time:        now,
	}, quickTunnelAuthBrokerClockSkew); err != nil {
		return nil, fmt.Errorf("validate broker assertion standard claims: %w", err)
	}

	issuedAt := claims.IssuedAt.Time().UTC()
	expiresAt := claims.Expiry.Time().UTC()
	if !now.Before(expiresAt) {
		return nil, errors.New("broker assertion has expired")
	}
	if issuedAt.After(expiresAt) {
		return nil, errors.New("broker assertion expiry must not be before its issued-at time")
	}
	if expiresAt.After(issuedAt.Add(quickTunnelAuthBrokerAssertionTTL)) {
		return nil, errors.New("broker assertion lifetime exceeds the maximum")
	}
	if claims.Type != quickTunnelAuthBrokerAssertionType {
		return nil, errors.New("broker assertion has an invalid type")
	}
	if !isQuickTunnelHostname(claims.CallbackHostname) || claims.CallbackHostname != expectedHostname {
		return nil, errors.New("broker assertion callback hostname does not match the expected Quick Tunnel")
	}
	if !isCanonicalQuickTunnelAuthAssertionState(claims.State) ||
		!hmac.Equal([]byte(claims.State), []byte(expectedState)) {
		return nil, errors.New("broker assertion state does not match the expected authentication state")
	}

	normalizedEmail := normalizeQuickTunnelEmail(claims.Email)
	if !isValidQuickTunnelEmail(normalizedEmail) {
		return nil, errors.New("broker assertion contains an invalid email")
	}

	identityExpiresAt := time.Unix(*claims.IdentityExpiresAt, 0).UTC()
	if !now.Before(identityExpiresAt) {
		return nil, errors.New("broker assertion Access identity has expired")
	}
	if expiresAt.After(identityExpiresAt) {
		return nil, errors.New("broker assertion outlives the Access identity")
	}

	return &QuickTunnelAuthIdentity{
		Email:     normalizedEmail,
		ExpiresAt: identityExpiresAt,
	}, nil
}

func isCanonicalQuickTunnelAuthAssertionState(state string) bool {
	decodedState, err := base64.RawURLEncoding.Strict().DecodeString(state)
	return err == nil && len(decodedState) == quickTunnelAuthStateSize
}
