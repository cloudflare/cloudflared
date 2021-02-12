package tunnel

import (
	"fmt"

	"github.com/cloudflare/cloudflared/tunneldns"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

func runDNSProxyServer(c *cli.Context, dnsReadySignal chan struct{}, shutdownC <-chan struct{}, log *zerolog.Logger) error {
	port := c.Int("proxy-dns-port")
	if port <= 0 || port > 65535 {
		return errors.New("The 'proxy-dns-port' must be a valid port number in <1, 65535> range.")
	}
	maxUpstreamConnections := c.Int("proxy-dns-max-upstream-conns")
	if maxUpstreamConnections < 0 {
		return fmt.Errorf("'%s' must be 0 or higher", "proxy-dns-max-upstream-conns")
	}
	listener, err := tunneldns.CreateListener(c.String("proxy-dns-address"), uint16(port), c.StringSlice("proxy-dns-upstream"), c.StringSlice("proxy-dns-bootstrap"), maxUpstreamConnections, log)
	if err != nil {
		close(dnsReadySignal)
		listener.Stop()
		return errors.Wrap(err, "Cannot create the DNS over HTTPS proxy server")
	}

	err = listener.Start(dnsReadySignal)
	if err != nil {
		return errors.Wrap(err, "Cannot start the DNS over HTTPS proxy server")
	}
	<-shutdownC
	_ = listener.Stop()
	log.Info().Msg("DNS server stopped")
	return nil
}
