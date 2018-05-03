package main

import (
	"github.com/cloudflare/cloudflared/tunneldns"

	"gopkg.in/urfave/cli.v2"
)

func runDNSProxyServer(c *cli.Context, dnsReadySignal, shutdownC chan struct{}) {
	port := c.Int("proxy-dns-port")
	if port <= 0 || port > 65535 {
		logger.Fatal("The 'proxy-dns-port' must be a valid port number in <1, 65535> range.")
	}
	listener, err := tunneldns.CreateListener(c.String("proxy-dns-address"), uint16(port), c.StringSlice("proxy-dns-upstream"))
	if err != nil {
		close(dnsReadySignal)
		listener.Stop()
		logger.WithError(err).Fatal("Cannot create the DNS over HTTPS proxy server")
	}

	err = listener.Start(dnsReadySignal)
	if err != nil {
		logger.WithError(err).Fatal("Cannot start the DNS over HTTPS proxy server")
	}
	<-shutdownC
	listener.Stop()
}
