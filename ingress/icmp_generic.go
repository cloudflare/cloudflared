//go:build !darwin

package ingress

import (
	"fmt"
	"net"
	"runtime"

	"github.com/rs/zerolog"
)

func newICMPProxy(listenIP net.IP, logger *zerolog.Logger) (ICMPProxy, error) {
	return nil, fmt.Errorf("ICMP proxy is not implemented on %s", runtime.GOOS)
}
