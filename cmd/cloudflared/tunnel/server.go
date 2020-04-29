package tunnel

import (
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunneldns"

	"gopkg.in/urfave/cli.v2"

	"github.com/pkg/errors"
)

func runDNSProxyServer(c *cli.Context, dnsReadySignal, shutdownC chan struct{}, logger logger.Service) error {
	port := c.Int("proxy-dns-port")
	if port <= 0 || port > 65535 {
		return errors.New("The 'proxy-dns-port' must be a valid port number in <1, 65535> range.")
	}
	listener, err := tunneldns.CreateListener(c.String("proxy-dns-address"), uint16(port), c.StringSlice("proxy-dns-upstream"), c.StringSlice("proxy-dns-bootstrap"), logger)
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
	listener.Stop()
	return nil
}
