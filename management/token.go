package management

import (
	"fmt"

	"github.com/go-jose/go-jose/v3/jwt"
)

type managementTokenClaims struct {
	Tunnel tunnel `json:"tun"`
	Actor  actor  `json:"actor"`
}

// VerifyTunnel compares the tun claim isn't empty
func (c *managementTokenClaims) verify() bool {
	return c.Tunnel.verify() && c.Actor.verify()
}

type tunnel struct {
	ID         string `json:"id"`
	AccountTag string `json:"account_tag"`
}

// verify compares the tun claim isn't empty
func (t *tunnel) verify() bool {
	return t.AccountTag != "" && t.ID != ""
}

type actor struct {
	ID      string `json:"id"`
	Support bool   `json:"support"`
}

// verify checks the ID claim isn't empty
func (t *actor) verify() bool {
	return t.ID != ""
}

func parseToken(token string) (*managementTokenClaims, error) {
	jwt, err := jwt.ParseSigned(token)
	if err != nil {
		return nil, fmt.Errorf("malformed jwt: %v", err)
	}

	var claims managementTokenClaims
	// This is actually safe because we verify the token in the edge before it reaches cloudflared
	err = jwt.UnsafeClaimsWithoutVerification(&claims)
	if err != nil {
		return nil, fmt.Errorf("malformed jwt: %v", err)
	}
	if !claims.verify() {
		return nil, fmt.Errorf("invalid management token format provided")
	}
	return &claims, nil
}
