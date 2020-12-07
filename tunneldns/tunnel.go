package tunneldns

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"
	odoh "github.com/cloudflare/odoh-go"
	"github.com/miekg/dns"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/cache"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

// Listener is an adapter between CoreDNS server and Warp runnable
type Listener struct {
	server *dnsserver.Server
	wg     sync.WaitGroup
	logger logger.Service
}

const (
	dohResolver = "https://1.1.1.1/dns-query"
)

var OdohConfig odoh.ObliviousDoHConfigContents

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
		},
		Subcommands: []*cli.Command{
			{
				Name:   "odoh",
				Action: cliutil.ErrorHandler(RunOdoh),
				Usage:  "Runs an Oblivious DNS over HTTPS client.",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "target",
						Usage:   "ODoH target URL",
						Value:   "https://1.1.1.1/dns-query",
						EnvVars: []string{"TUNNEL_DNS_ODOH_TARGET"},
					},
					&cli.StringFlag{
						Name:    "proxy",
						Usage:   "ODoH proxy URL",
						Value:   "https://odoh1.surfdomeinen.nl/proxy",
						EnvVars: []string{"TUNNEL_DNS_ODOH_PROXY"},
					},
					&cli.BoolFlag{
						Name:    "useproxy",
						Usage:   "Set flag to enable proxy usage",
						Value:   false,
						EnvVars: []string{"TUNNEL_DNS_ODOH_USE_PROXY"},
					},
				},
			},
		},
		ArgsUsage: " ", // can't be the empty string or we get the default output
		Hidden:    hidden,
	}
}

// Run implements a foreground runner
func Run(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return cliutil.PrintLoggerSetupError("error setting up logger", err)
	}

	metricsListener, err := net.Listen("tcp", c.String("metrics"))
	if err != nil {
		logger.Fatalf("Failed to open the metrics listener: %s", err)
	}

	go metrics.ServeMetrics(metricsListener, nil, nil, logger)

	listener, err := CreateListener(
		c.String("address"),
		uint16(c.Uint("port")),
		c.StringSlice("upstream"),
		c.StringSlice("bootstrap"),
		logger,
	)
	if err != nil {
		logger.Errorf("Failed to create the listeners: %s", err)
		return err
	}

	// Try to start the server
	readySignal := make(chan struct{})
	err = listener.Start(readySignal)
	if err != nil {
		logger.Errorf("Failed to start the listeners: %s", err)
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
		logger.Errorf("failed to stop: %s", err)
	}
	return err
}

// RunOdoh implements a foreground runner
func RunOdoh(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return cliutil.PrintLoggerSetupError("error setting up logger", err)
	}

	metricsListener, err := net.Listen("tcp", c.String("metrics"))
	if err != nil {
		logger.Fatalf("Failed to open the metrics listener: %s", err)
	}

	go metrics.ServeMetrics(metricsListener, nil, nil, logger)

	listener, err := CreateObliviousDNSListener(
		c.String("address"),
		uint16(c.Uint("port")),
		c.String("target"),
		c.String("proxy"),
		c.Bool("useproxy"),
		logger,
	)
	if err != nil {
		logger.Errorf("Failed to create the listeners: %s", err)
		return err
	}

	// Update odohconfig
	go listener.UpdateOdohConfig()

	// Try to start the server
	readySignal := make(chan struct{})
	err = listener.Start(readySignal)
	if err != nil {
		logger.Errorf("Failed to start the listeners: %s", err)
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
		logger.Errorf("failed to stop: %s", err)
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

	l.logger.Infof("Starting DNS proxy server on: %s", l.server.Address())

	// Start UDP listener
	if udp, err := l.server.ListenPacket(); err == nil {
		l.wg.Add(1)
		go func() {
			l.server.ServePacket(udp)
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
			l.server.Serve(tcp)
			l.wg.Done()
		}()
	}

	return errors.Wrap(err, "failed to create a TCP listener")
}

// UpdateOdohConfig periodically updates odoh configs
// Currently supports `odoh.cloudflare-dns.com.`.
func (l *Listener) UpdateOdohConfig() {
	l.logger.Infof("Starting Oblivious DoH key updates")
	dohResolver, _ := url.Parse(dohResolver)
	client := http.Client{}
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(targetHostname, dns.TypeHTTPS)
	dnsQuery.RecursionDesired = true
	packedDNSQuery, _ := dnsQuery.Pack()
	for {
		configs, err := FetchObliviousDoHConfig(&client, packedDNSQuery, dohResolver)
		if err != nil {
			l.logger.Errorf("odoh config not updated with err ", err)
		}
		OdohConfig = configs.Configs[0].Contents
		time.Sleep(odohConfigDuration)
	}
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
func CreateListener(address string, port uint16, upstreams []string, bootstraps []string, logger logger.Service) (*Listener, error) {
	// Build the list of upstreams
	upstreamList := make([]Upstream, 0)
	for _, url := range upstreams {
		logger.Infof("Adding DNS upstream - url: %s", url)
		upstream, err := NewUpstreamHTTPS(url, bootstraps, nil, logger)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create HTTPS upstream")
		}
		upstreamList = append(upstreamList, upstream)
	}

	return buildListenerFromUpstream(upstreamList, address, port, logger)
}

// CreateObliviousDNSListener configures the server and bound sockets
func CreateObliviousDNSListener(address string, port uint16, target string, proxy string, useproxy bool, logger logger.Service) (*Listener, error) {
	logger.Infof("Adding Oblivious DoH target - url: %s", target)
	var upstream Upstream
	var err error
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	odohCtx := ObliviousDoHCtx{
		useproxy: useproxy,
		target:   targetURL,
	}
	if useproxy {
		logger.Infof("Adding Oblivious DoH proxy - url: %s", proxy)
		upstream, err = NewUpstreamHTTPS(proxy, nil, &odohCtx, logger)
	} else {
		logger.Infof("No Oblivious DoH proxy is set")
		upstream, err = NewUpstreamHTTPS(target, nil, &odohCtx, logger)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to create HTTPS upstream")
	}

	return buildListenerFromUpstream([]Upstream{upstream}, address, port, logger)
}

func buildListenerFromUpstream(upstreams []Upstream, address string, port uint16, logger logger.Service) (*Listener, error) {
	// Create a local cache with HTTPS proxy plugin
	chain := cache.New()
	chain.Next = ProxyPlugin{
		Upstreams: upstreams,
	}

	// Format an endpoint
	endpoint := "dns://" + net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10))

	// Create the actual middleware server
	server, err := dnsserver.NewServer(endpoint, []*dnsserver.Config{createConfig(address, port, NewMetricsPlugin(chain))})
	if err != nil {
		return nil, err
	}

	return &Listener{server: server, logger: logger}, nil
}
