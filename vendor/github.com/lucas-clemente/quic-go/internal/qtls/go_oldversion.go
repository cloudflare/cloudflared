//go:build (go1.9 || go1.10 || go1.11 || go1.12 || go1.13 || go1.14 || go1.15) && !go1.16
// +build go1.9 go1.10 go1.11 go1.12 go1.13 go1.14 go1.15
// +build !go1.16

package qtls

var _ int = "The version of quic-go you're using can't be built using outdated Go versions. For more details, please see https://github.com/lucas-clemente/quic-go/wiki/quic-go-and-Go-versions."
