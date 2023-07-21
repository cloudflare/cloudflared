package management

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/go-jose/go-jose/v3"
	"github.com/stretchr/testify/require"
)

const (
	validToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiIsImtpZCI6IjEifQ.eyJ0dW4iOnsiaWQiOiI3YjA5ODE0OS01MWZlLTRlZTUtYTY4Ny0zZTM3NDQ2NmVmYzciLCJhY2NvdW50X3RhZyI6ImNkMzkxZTljMDYyNmE4Zjc2Y2IxZjY3MGY2NTkxYjA1In0sImFjdG9yIjp7ImlkIjoiZGNhcnJAY2xvdWRmbGFyZS5jb20iLCJzdXBwb3J0IjpmYWxzZX0sInJlcyI6WyJsb2dzIl0sImV4cCI6MTY3NzExNzY5NiwiaWF0IjoxNjc3MTE0MDk2LCJpc3MiOiJ0dW5uZWxzdG9yZSJ9.mKenOdOy3Xi4O-grldFnAAemdlE9WajEpTDC_FwezXQTstWiRTLwU65P5jt4vNsIiZA4OJRq7bH-QYID9wf9NA"

	accountTag = "cd391e9c0626a8f76cb1f670f6591b05"
	tunnelID   = "7b098149-51fe-4ee5-a687-3e374466efc7"
	actorID    = "45d2751e-6b59-45a9-814d-f630786bd0cd"
)

type invalidManagementTokenClaims struct {
	Invalid string `json:"invalid"`
}

func TestParseToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	for _, test := range []struct {
		name   string
		claims any
		err    error
	}{
		{
			name: "Valid",
			claims: managementTokenClaims{
				Tunnel: tunnel{
					ID:         tunnelID,
					AccountTag: accountTag,
				},
				Actor: actor{
					ID: actorID,
				},
			},
		},
		{
			name:   "Invalid claims",
			claims: invalidManagementTokenClaims{Invalid: "invalid"},
			err:    errors.New("invalid management token format provided"),
		},
		{
			name: "Missing Tunnel",
			claims: managementTokenClaims{
				Actor: actor{
					ID: actorID,
				},
			},
			err: errors.New("invalid management token format provided"),
		},
		{
			name: "Missing Tunnel ID",
			claims: managementTokenClaims{
				Tunnel: tunnel{
					AccountTag: accountTag,
				},
				Actor: actor{
					ID: actorID,
				},
			},
			err: errors.New("invalid management token format provided"),
		},
		{
			name: "Missing Account Tag",
			claims: managementTokenClaims{
				Tunnel: tunnel{
					ID: tunnelID,
				},
				Actor: actor{
					ID: actorID,
				},
			},
			err: errors.New("invalid management token format provided"),
		},
		{
			name: "Missing Actor",
			claims: managementTokenClaims{
				Tunnel: tunnel{
					ID:         tunnelID,
					AccountTag: accountTag,
				},
			},
			err: errors.New("invalid management token format provided"),
		},
		{
			name: "Missing Actor ID",
			claims: managementTokenClaims{
				Tunnel: tunnel{
					ID: tunnelID,
				},
				Actor: actor{},
			},
			err: errors.New("invalid management token format provided"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			jwt := signToken(t, test.claims, key)
			claims, err := parseToken(jwt)
			if test.err != nil {
				require.EqualError(t, err, test.err.Error())
				return
			}
			require.Nil(t, err)
			require.Equal(t, test.claims, *claims)
		})
	}
}

func signToken(t *testing.T, token any, key *ecdsa.PrivateKey) string {
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "1")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, opts)
	require.NoError(t, err)
	payload, err := json.Marshal(token)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	jwt, err := jws.CompactSerialize()
	require.NoError(t, err)
	return jwt
}
