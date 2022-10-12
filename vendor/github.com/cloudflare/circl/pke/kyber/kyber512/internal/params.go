// Code generated from params.templ.go. DO NOT EDIT.

package internal

import (
	"github.com/cloudflare/circl/pke/kyber/internal/common"
)

const (
	K             = 2
	Eta1          = 3
	DU            = 10
	DV            = 4
	PublicKeySize = 32 + K*common.PolySize

	PrivateKeySize = K * common.PolySize

	PlaintextSize  = common.PlaintextSize
	SeedSize       = 32
	CiphertextSize = 768
)
