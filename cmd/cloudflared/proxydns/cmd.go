package proxydns

import (
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/tunneldns"
)

func Command(hidden bool) *cli.Command {
	return &cli.Command{
		Name:   "proxy-dns",
		Action: cliutil.ConfiguredAction(Run),

		Usage: "Run a DNS over HTTPS proxy server.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "metrics",
				Value:   "localhost:",
				Usage:   "Listen address for metrics reporting.",
				EnvVars: []string{"TUNNEL_METRICS"},
			},
			&cli.StringFlag{
				Name:    "address",
				Usage:   "Listen address for the DNS over HTTPS proxy server.",
				Value:   "localhost",
				EnvVars: []string{"TUNNEL_DNS_ADDRESS"},
			},
			// Note TUN-3758 , we use Int because UInt is not supported with altsrc
			&cli.IntFlag{
				Name:    "port",
				Usage:   "Listen on given port for the DNS over HTTPS proxy server.",
				Value:   53,
				EnvVars: []string{"TUNNEL_DNS_PORT"},
			},
			&cli.StringSliceFlag{
				Name:    "upstream",
				Usage:   "Upstream endpoint URL, you can specify multiple endpoints for redundancy.",
				Value:   cli.NewStringSlice("https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"),
				EnvVars: []string{"TUNNEL_DNS_UPSTREAM"},
			},
			&cli.StringSliceFlag{
				Name:    "bootstrap",
				Usage:   "bootstrap endpoint URL, you can specify multiple endpoints for redundancy.",
				Value:   cli.NewStringSlice("https://162.159.36.1/dns-query", "https://162.159.46.1/dns-query", "https://[2606:4700:4700::1111]/dns-query", "https://[2606:4700:4700::1001]/dns-query"),
				EnvVars: []string{"TUNNEL_DNS_BOOTSTRAP"},
			},
			&cli.IntFlag{
				Name:    "max-upstream-conns",
				Usage:   "Maximum concurrent connections to upstream. Setting to 0 means unlimited.",
				Value:   tunneldns.MaxUpstreamConnsDefault,
				EnvVars: []string{"TUNNEL_DNS_MAX_UPSTREAM_CONNS"},
			},
		},
		ArgsUsage: " ", // can't be the empty string or we get the default output
		Hidden:    hidden,
	}
}

// Run implements a foreground runner
func Run(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	metricsListener, err := net.Listen("tcp", c.String("metrics"))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open the metrics listener")
	}

	go metrics.ServeMetrics(metricsListener, nil, nil, "", log)

	listener, err := tunneldns.CreateListener(
		c.String("address"),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		uint16(c.Int("port")),
		c.StringSlice("upstream"),
		c.StringSlice("bootstrap"),
		c.Int("max-upstream-conns"),
		log,
	)

	if err != nil {
		log.Err(err).Msg("Failed to create the listeners")
		return err
	}

	// Try to start the server
	readySignal := make(chan struct{})
	err = listener.Start(readySignal)
	if err != nil {
		log.Err(err).Msg("Failed to start the listeners")
		return listener.Stop()
	}
	<-readySignal

	// Wait for signal
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	<-signals

	// Shut down server
	err = listener.Stop()
	if err != nil {
		log.Err(err).Msg("failed to stop")
	}
	return err
}
