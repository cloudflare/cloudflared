package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pkg/errors"
)

const (
	headerKeyAccessJWTAssertion = "Cf-Access-Jwt-Assertion"
)

var (
	ErrNoAccessToken         = errors.New("no access token provided in request")
	cloudflareAccessCertsURL = "https://%s.cloudflareaccess.com"
)

// JWTValidator is an implementation of Verifier that validates access based JWT tokens.
type JWTValidator struct {
	*oidc.IDTokenVerifier
	audTags []string
}

func NewJWTValidator(teamName string, certsURL string, audTags []string) *JWTValidator {
	if certsURL == "" {
		certsURL = fmt.Sprintf(cloudflareAccessCertsURL, teamName)
	}
	certsEndpoint := fmt.Sprintf("%s/cdn-cgi/access/certs", certsURL)

	config := &oidc.Config{
		SkipClientIDCheck: true,
	}

	ctx := context.Background()
	keySet := oidc.NewRemoteKeySet(ctx, certsEndpoint)
	verifier := oidc.NewVerifier(certsURL, keySet, config)
	return &JWTValidator{
		IDTokenVerifier: verifier,
	}
}

func (v *JWTValidator) Handle(ctx context.Context, r *http.Request) error {
	accessJWT := r.Header.Get(headerKeyAccessJWTAssertion)
	if accessJWT == "" {
		return ErrNoAccessToken
	}

	token, err := v.IDTokenVerifier.Verify(ctx, accessJWT)
	if err != nil {
		return fmt.Errorf("Invalid token: %w", err)
	}

	// We want atleast one audTag to match
	for _, jwtAudTag := range token.Audience {
		for _, acceptedAudTag := range v.audTags {
			if acceptedAudTag == jwtAudTag {
				return nil
			}
		}
	}

	return fmt.Errorf("Invalid token: %w", err)
}
