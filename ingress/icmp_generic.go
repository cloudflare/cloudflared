//go:build !darwin && !linux && !windows

package ingress

import (
	"fmt"
	"net/netip"
	"runtime"

	"github.com/rs/zerolog"
)

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (ICMPProxy, error) {
	return nil, fmt.Errorf("ICMP proxy is not implemented on %s", runtime.GOOS)
}
