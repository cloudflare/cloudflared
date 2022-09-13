//go:build !darwin && !linux && !windows

package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
)

var errICMPProxyNotImplemented = fmt.Errorf("ICMP proxy is not implemented on %s", runtime.GOOS)

type icmpProxy struct{}

func (ip icmpProxy) Request(pk *packet.ICMP, responder packet.FunnelUniPipe) error {
	return errICMPProxyNotImplemented
}

func (ip *icmpProxy) Serve(ctx context.Context) error {
	return errICMPProxyNotImplemented
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	return nil, errICMPProxyNotImplemented
}
