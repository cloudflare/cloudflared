// +build fips

package main

import (
    _ "crypto/tls/fipsonly"
    "github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

func init () {
    tunnel.FipsEnabled = true
}
