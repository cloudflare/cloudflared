package tunneldns

import (
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/cache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const (
	LogFieldAddress         = "address"
	LogFieldURL             = "url"
	MaxUpstreamConnsDefault = 5
)

// Listener is an adapter between CoreDNS server and Warp runnable
type Listener struct {
	server *dnsserver.Server
	wg     sync.WaitGroup
	log    *zerolog.Logger
}

func Command(hidden bool) *cli.Command {
	return &cli.Command{
		Name:   "proxy-dns",
		Action: cliutil.ErrorHandler(Run),
		Usage:  "Run a DNS over HTTPS proxy server.",
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
				Value:   MaxUpstreamConnsDefault,
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

	go metrics.ServeMetrics(metricsListener, nil, nil, log)

	listener, err := CreateListener(
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

// Create a CoreDNS server plugin from configuration
func createConfig(address string, port uint16, p plugin.Handler) *dnsserver.Config {
	c := &dnsserver.Config{
		Zone:        ".",
		Transport:   "dns",
		ListenHosts: []string{address},
		Port:        strconv.FormatUint(uint64(port), 10),
	}

	c.AddPlugin(func(next plugin.Handler) plugin.Handler { return p })
	return c
}

// Start blocks for serving requests
func (l *Listener) Start(readySignal chan struct{}) error {
	defer close(readySignal)
	l.log.Info().Str(LogFieldAddress, l.server.Address()).Msg("Starting DNS over HTTPS proxy server")

	// Start UDP listener
	if udp, err := l.server.ListenPacket(); err == nil {
		l.wg.Add(1)
		go func() {
			_ = l.server.ServePacket(udp)
			l.wg.Done()
		}()
	} else {
		return errors.Wrap(err, "failed to create a UDP listener")
	}

	// Start TCP listener
	tcp, err := l.server.Listen()
	if err == nil {
		l.wg.Add(1)
		go func() {
			_ = l.server.Serve(tcp)
			l.wg.Done()
		}()
	}

	return errors.Wrap(err, "failed to create a TCP listener")
}

// Stop signals server shutdown and blocks until completed
func (l *Listener) Stop() error {
	if err := l.server.Stop(); err != nil {
		return err
	}

	l.wg.Wait()
	return nil
}

// CreateListener configures the server and bound sockets
func CreateListener(address string, port uint16, upstreams []string, bootstraps []string, maxUpstreamConnections int, log *zerolog.Logger) (*Listener, error) {
	// Build the list of upstreams
	upstreamList := make([]Upstream, 0)
	for _, url := range upstreams {
		log.Info().Str(LogFieldURL, url).Msg("Adding DNS upstream")
		upstream, err := NewUpstreamHTTPS(url, bootstraps, maxUpstreamConnections, log)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create HTTPS upstream")
		}
		upstreamList = append(upstreamList, upstream)
	}

	// Create a local cache with HTTPS proxy plugin
	chain := cache.New()
	chain.Next = ProxyPlugin{
		Upstreams: upstreamList,
	}

	// Format an endpoint
	endpoint := "dns://" + net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10))

	// Create the actual middleware server
	server, err := dnsserver.NewServer(endpoint, []*dnsserver.Config{createConfig(address, port, NewMetricsPlugin(chain))})
	if err != nil {
		return nil, err
	}

	return &Listener{server: server, log: log}, nil
}
