//go:build fips

package fips

import (
	_ "crypto/tls/fipsonly"
)

func IsFipsEnabled() bool {
	return true
}
