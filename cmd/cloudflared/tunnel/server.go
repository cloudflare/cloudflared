package tunnel

import (
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunneldns"

	"github.com/urfave/cli/v2"

	"github.com/pkg/errors"
)

func runDNSProxyServer(c *cli.Context, dnsReadySignal, shutdownC chan struct{}, logger logger.Service, odoh bool) error {
	port := c.Int("proxy-dns-port")
	if port <= 0 || port > 65535 {
		return errors.New("The 'proxy-dns-port' must be a valid port number in <1, 65535> range.")
	}
	var listener *tunneldns.Listener
	var err error
	if odoh {
		listener, err = tunneldns.CreateObliviousDNSListener(
			c.String("proxy-dns-address"),
			uint16(port),
			c.String("proxy-dns-odoh-target"),
			c.String("proxy-dns-odoh-proxy"),
			c.Bool("proxy-dns-odoh-useproxy"),
			logger,
		)
	} else {
		listener, err = tunneldns.CreateListener(
			c.String("proxy-dns-address"),
			uint16(port),
			c.StringSlice("proxy-dns-upstream"),
			c.StringSlice("proxy-dns-bootstrap"),
			logger,
		)
	}

	// Update odohconfig
	go listener.UpdateOdohConfig()

	if err != nil {
		close(dnsReadySignal)
		listener.Stop()
		if odoh {
			return errors.Wrap(err, "Cannot create the Oblivious DNS over HTTPS proxy server")
		} else {
			return errors.Wrap(err, "Cannot create the DNS over HTTPS proxy server")
		}
	}

	err = listener.Start(dnsReadySignal)
	if odoh {
		if err != nil {
			return errors.Wrap(err, "Cannot start the Oblivious DNS over HTTPS proxy server")
		}
	} else {
		if err != nil {
			return errors.Wrap(err, "Cannot start the DNS over HTTPS proxy server")
		}
	}

	<-shutdownC
	listener.Stop()
	return nil
}
