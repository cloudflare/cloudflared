package middleware

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	issuer = fmt.Sprintf(cloudflareAccessCertsURL, "testteam")
)

type accessTokenClaims struct {
	Email string `json:"email"`
	Type  string `json:"type"`
	jwt.Claims
}

func TestJWTValidator(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com", nil)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issued := time.Now()
	claims := accessTokenClaims{
		Email: "test@example.com",
		Type:  "app",
		Claims: jwt.Claims{
			Issuer:   issuer,
			Subject:  "ee239b7a-e3e6-4173-972a-8fbe9d99c04f",
			Audience: []string{""},
			Expiry:   jwt.NewNumericDate(issued.Add(time.Hour)),
			IssuedAt: jwt.NewNumericDate(issued),
		},
	}
	token := signToken(t, claims, key)
	req.Header.Add(headerKeyAccessJWTAssertion, token)

	keySet := oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{key.Public()}}
	config := &oidc.Config{
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: []string{string(jose.ES256)},
	}
	verifier := oidc.NewVerifier(issuer, &keySet, config)

	tests := []struct {
		name    string
		audTags []string
		aud     jwt.Audience
		error   bool
	}{
		{
			name: "valid",
			audTags: []string{
				"0bc545634b1732494b3f9472794a549c883fabd48de9dfe0e0413e59c3f96c38",
				"d7ec5b7fda23ffa8f8c8559fb37c66a2278208a78dbe376a3394b5ffec6911ba",
			},
			aud:   jwt.Audience{"d7ec5b7fda23ffa8f8c8559fb37c66a2278208a78dbe376a3394b5ffec6911ba"},
			error: false,
		},
		{
			name: "invalid no match",
			audTags: []string{
				"0bc545634b1732494b3f9472794a549c883fabd48de9dfe0e0413e59c3f96c38",
				"d7ec5b7fda23ffa8f8c8559fb37c66a2278208a78dbe376a3394b5ffec6911ba",
			},
			aud:   jwt.Audience{"09dc377143841843ecca28b196bdb1ec1675af38c8b7b60c7def5876c8877157"},
			error: true,
		},
		{
			name:    "invalid empty check",
			audTags: []string{},
			aud:     jwt.Audience{"09dc377143841843ecca28b196bdb1ec1675af38c8b7b60c7def5876c8877157"},
			error:   true,
		},
		{
			name: "invalid absent aud",
			audTags: []string{
				"0bc545634b1732494b3f9472794a549c883fabd48de9dfe0e0413e59c3f96c38",
				"d7ec5b7fda23ffa8f8c8559fb37c66a2278208a78dbe376a3394b5ffec6911ba",
			},
			aud:   jwt.Audience{""},
			error: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := JWTValidator{
				IDTokenVerifier: verifier,
				audTags:         test.audTags,
			}
			claims.Audience = test.aud
			token := signToken(t, claims, key)
			req.Header.Set(headerKeyAccessJWTAssertion, token)

			result, err := validator.Handle(context.Background(), req)
			assert.NoError(t, err)
			assert.Equal(t, test.error, result.ShouldFilterRequest)
		})
	}
}

func signToken(t *testing.T, token accessTokenClaims, key *ecdsa.PrivateKey) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, &jose.SignerOptions{})
	require.NoError(t, err)
	payload, err := json.Marshal(token)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	jwt, err := jws.CompactSerialize()
	require.NoError(t, err)
	return jwt
}
