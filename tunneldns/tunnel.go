package tunneldns

import (
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/cache"
	"github.com/pkg/errors"
	"gopkg.in/urfave/cli.v2"
)

// Listener is an adapter between CoreDNS server and Warp runnable
type Listener struct {
	server *dnsserver.Server
	wg     sync.WaitGroup
	logger logger.Service
}

// Run implements a foreground runner
func Run(c *cli.Context) error {
	logDirectory, logLevel := config.FindLogSettings()
	logger, err := logger.New(logger.DefaultFile(logDirectory), logger.LogLevelString(logLevel))
	if err != nil {
		return cliutil.PrintLoggerSetupError("error setting up logger", err)
	}

	metricsListener, err := net.Listen("tcp", c.String("metrics"))
	if err != nil {
		logger.Fatalf("Failed to open the metrics listener: %s", err)
	}

	go metrics.ServeMetrics(metricsListener, nil, logger)

	listener, err := CreateListener(c.String("address"), uint16(c.Uint("port")), c.StringSlice("upstream"), c.StringSlice("bootstrap"), logger)
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
	l.logger.Infof("Starting DNS over HTTPS proxy server on: %s", l.server.Address())

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
		upstream, err := NewUpstreamHTTPS(url, bootstraps, logger)
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

	return &Listener{server: server, logger: logger}, nil
}
