//go:build !fips

package fips

func IsFipsEnabled() bool {
	return false
}
