package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cloudflare/cloudflared/credentials"
)

const (
	headerKeyAccessJWTAssertion = "Cf-Access-Jwt-Assertion"
)

var (
	cloudflareAccessCertsURL    = "https://%s.cloudflareaccess.com"
	cloudflareAccessFedCertsURL = "https://%s.fed.cloudflareaccess.com"
)

// JWTValidator is an implementation of Verifier that validates access based JWT tokens.
type JWTValidator struct {
	*oidc.IDTokenVerifier
	audTags []string
}

func NewJWTValidator(teamName string, environment string, audTags []string) *JWTValidator {
	var certsURL string
	if environment == credentials.FedEndpoint {
		certsURL = fmt.Sprintf(cloudflareAccessFedCertsURL, teamName)
	} else {
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
		audTags:         audTags,
	}
}

func (v *JWTValidator) Name() string {
	return "AccessJWTValidator"
}

func (v *JWTValidator) Handle(ctx context.Context, r *http.Request) (*HandleResult, error) {
	accessJWT := r.Header.Get(headerKeyAccessJWTAssertion)
	if accessJWT == "" {
		// log the exact error message here. the message is specific to the handler implementation logic, we don't gain anything
		// in passing it upstream. and each handler impl know what logging level to use for each.
		return &HandleResult{
			ShouldFilterRequest: true,
			StatusCode:          http.StatusForbidden,
			Reason:              "no access token in request",
		}, nil
	}

	token, err := v.IDTokenVerifier.Verify(ctx, accessJWT)
	if err != nil {
		return nil, err
	}

	// We want at least one audTag to match
	for _, jwtAudTag := range token.Audience {
		for _, acceptedAudTag := range v.audTags {
			if acceptedAudTag == jwtAudTag {
				return &HandleResult{ShouldFilterRequest: false}, nil
			}
		}
	}

	return &HandleResult{
		ShouldFilterRequest: true,
		StatusCode:          http.StatusForbidden,
		Reason:              fmt.Sprintf("Invalid token in jwt: %v", token.Audience),
	}, nil
}
