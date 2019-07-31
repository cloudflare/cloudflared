package tunnel

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/google/uuid"

	"github.com/getsentry/raven-go"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/cmd/sqlgateway"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunneldns"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/coreos/go-systemd/daemon"
	"github.com/facebookgo/grace/gracenet"
	"github.com/pkg/errors"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"

var (
	shutdownC      chan struct{}
	graceShutdownC chan struct{}
	version        string
)

func Flags() []cli.Flag {
	return tunnelFlags(true)
}

func Commands() []*cli.Command {
	cmds := []*cli.Command{
		{
			Name:      "login",
			Action:    login,
			Usage:     "Generate a configuration file with your login details",
			ArgsUsage: " ",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:   "url",
					Hidden: true,
				},
			},
			Hidden: true,
		},
		{
			Name:   "proxy-dns",
			Action: tunneldns.Run,
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
			},
			ArgsUsage: " ", // can't be the empty string or we get the default output
			Hidden:    false,
		},
		{
			Name: "db",
			Action: func(c *cli.Context) error {
				tags := make(map[string]string)
				tags["hostname"] = c.String("hostname")
				raven.SetTagsContext(tags)

				fmt.Printf("\nSQL Database Password: ")
				pass, err := terminal.ReadPassword(int(syscall.Stdin))
				if err != nil {
					logger.Error(err)
				}

				go sqlgateway.StartProxy(c, logger, string(pass))

				raven.CapturePanic(func() { err = tunnel(c) }, nil)
				if err != nil {
					raven.CaptureError(err, nil)
				}
				return err
			},
			Before: Before,
			Usage:  "SQL Gateway is an SQL over HTTP reverse proxy",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "db",
					Value: true,
					Usage: "Enable the SQL Gateway Proxy",
				},
				&cli.StringFlag{
					Name:  "address",
					Value: "",
					Usage: "Database connection string: db://user:pass",
				},
			},
			Hidden: true,
		},
	}

	var subcommands []*cli.Command
	for _, cmd := range cmds {
		c := *cmd
		c.Hidden = false
		subcommands = append(subcommands, &c)
	}

	cmds = append(cmds, &cli.Command{
		Name:      "tunnel",
		Action:    tunnel,
		Before:    Before,
		Category:  "Tunnel",
		Usage:     "Make a locally-running web service accessible over the internet using Argo Tunnel.",
		ArgsUsage: "[origin-url]",
		Description: `Argo Tunnel asks you to specify a hostname on a Cloudflare-powered
		domain you control and a local address. Traffic from that hostname is routed
		(optionally via a Cloudflare Load Balancer) to this machine and appears on the
		specified port where it can be served.

		This feature requires your Cloudflare account be subscribed to the Argo Smart Routing feature.

		To use, begin by calling login to download a certificate:

		  cloudflared tunnel login

		With your certificate installed you can then launch your first tunnel,
		replacing my.site.com with a subdomain of your site:

		  cloudflared tunnel --hostname my.site.com --url http://localhost:8080

		If you have a web server running on port 8080 (in this example), it will be available on
		the internet!`,
		Subcommands: subcommands,
		Flags:       tunnelFlags(false),
	})

	return cmds
}

func tunnel(c *cli.Context) error {
	return StartServer(c, version, shutdownC, graceShutdownC)
}

func Init(v string, s, g chan struct{}) {
	version, shutdownC, graceShutdownC = v, s, g
}

func StartServer(c *cli.Context, version string, shutdownC, graceShutdownC chan struct{}) error {
	_ = raven.SetDSN(sentryDSN)
	var wg sync.WaitGroup
	listeners := gracenet.Net{}
	errC := make(chan error)
	connectedSignal := signal.New(make(chan struct{}))
	dnsReadySignal := make(chan struct{})

	if c.String("config") == "" {
		logger.Warnf("Cannot determine default configuration path. No file %v in %v", config.DefaultConfigFiles, config.DefaultConfigDirs)
	}

	if err := configMainLogger(c); err != nil {
		return errors.Wrap(err, "Error configuring logger")
	}

	transportLogger, err := configTransportLogger(c)
	if err != nil {
		return errors.Wrap(err, "Error configuring transport logger")
	}

	if c.IsSet("trace-output") {
		tmpTraceFile, err := ioutil.TempFile("", "trace")
		if err != nil {
			logger.WithError(err).Error("Failed to create new temporary file to save trace output")
		}

		defer func() {
			if err := tmpTraceFile.Close(); err != nil {
				logger.WithError(err).Errorf("Failed to close trace output file %s", tmpTraceFile.Name())
			}
			if err := os.Rename(tmpTraceFile.Name(), c.String("trace-output")); err != nil {
				logger.WithError(err).Errorf("Failed to rename temporary trace output file %s to %s", tmpTraceFile.Name(), c.String("trace-output"))
			} else {
				err := os.Remove(tmpTraceFile.Name())
				if err != nil {
					logger.WithError(err).Errorf("Failed to remove the temporary trace file %s", tmpTraceFile.Name())
				}
			}
		}()

		if err := trace.Start(tmpTraceFile); err != nil {
			logger.WithError(err).Error("Failed to start trace")
			return errors.Wrap(err, "Error starting tracing")
		}
		defer trace.Stop()
	}

	if c.String("logfile") != "" {
		if err := initLogFile(c, logger, transportLogger); err != nil {
			logger.Error(err)
		}
	}

	if err := handleDeprecatedOptions(c); err != nil {
		return err
	}

	buildInfo := buildinfo.GetBuildInfo(version)
	buildInfo.Log(logger)
	logClientOptions(c)

	if c.IsSet("proxy-dns") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errC <- runDNSProxyServer(c, dnsReadySignal, shutdownC)
		}()
	} else {
		close(dnsReadySignal)
	}

	// Wait for proxy-dns to come up (if used)
	<-dnsReadySignal

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		logger.WithError(err).Error("Error opening metrics server listener")
		return errors.Wrap(err, "Error opening metrics server listener")
	}
	defer metricsListener.Close()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- metrics.ServeMetrics(metricsListener, shutdownC, logger)
	}()

	go notifySystemd(connectedSignal)
	if c.IsSet("pidfile") {
		go writePidFile(connectedSignal, c.String("pidfile"))
	}

	cloudflaredID, err := uuid.NewRandom()
	if err != nil {
		logger.WithError(err).Error("Cannot generate cloudflared ID")
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-shutdownC
		cancel()
	}()

	if c.IsSet("use-declarative-tunnels") {
		return startDeclarativeTunnel(ctx, c, cloudflaredID, buildInfo, &listeners)
	}

	// update needs to be after DNS proxy is up to resolve equinox server address
	if updater.IsAutoupdateEnabled(c) {
		logger.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			autoupdater := updater.NewAutoUpdater(c.Duration("autoupdate-freq"), &listeners)
			errC <- autoupdater.Run(ctx)
		}()
	}

	// Serve DNS proxy stand-alone if no hostname or tag or app is going to run
	if dnsProxyStandAlone(c) {
		connectedSignal.Notify()
		// no grace period, handle SIGINT/SIGTERM immediately
		return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, 0)
	}

	if c.IsSet("hello-world") {
		logger.Infof("hello-world set")
		helloListener, err := hello.CreateTLSListener("127.0.0.1:")
		if err != nil {
			logger.WithError(err).Error("Cannot start Hello World Server")
			return errors.Wrap(err, "Cannot start Hello World Server")
		}
		defer helloListener.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			hello.StartHelloWorldServer(logger, helloListener, shutdownC)
		}()
		c.Set("url", "https://"+helloListener.Addr().String())
	}

	if host := hostnameFromURI(c.String("url")); host != "" {
		listener, err := net.Listen("tcp", "127.0.0.1:")
		if err != nil {
			logger.WithError(err).Error("Cannot start Websocket Proxy Server")
			return errors.Wrap(err, "Cannot start Websocket Proxy Server")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errC <- websocket.StartProxyServer(logger, listener, host, shutdownC)
		}()
		c.Set("url", "http://"+listener.Addr().String())
	}

	tunnelConfig, err := prepareTunnelConfig(c, buildInfo, version, logger, transportLogger)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- origin.StartTunnelDaemon(ctx, tunnelConfig, connectedSignal, cloudflaredID)
	}()

	return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, c.Duration("grace-period"))
}

func Before(c *cli.Context) error {
	if c.String("config") == "" {
		logger.Debugf("Cannot determine default configuration path. No file %v in %v", config.DefaultConfigFiles, config.DefaultConfigDirs)
	}
	inputSource, err := config.FindInputSourceContext(c)
	if err != nil {
		logger.WithError(err).Errorf("Cannot load configuration from %s", c.String("config"))
		return err
	} else if inputSource != nil {
		err := altsrc.ApplyInputSourceValues(c, inputSource, c.App.Flags)
		if err != nil {
			logger.WithError(err).Errorf("Cannot apply configuration from %s", c.String("config"))
			return err
		}
		logger.Debugf("Applied configuration from %s", c.String("config"))
	}
	return nil
}

func startDeclarativeTunnel(ctx context.Context,
	c *cli.Context,
	cloudflaredID uuid.UUID,
	buildInfo *buildinfo.BuildInfo,
	listeners *gracenet.Net,
) error {
	reverseProxyOrigin, err := defaultOriginConfig(c)
	if err != nil {
		logger.WithError(err)
		return err
	}
	reverseProxyConfig, err := pogs.NewReverseProxyConfig(
		c.String("hostname"),
		reverseProxyOrigin,
		c.Uint64("retries"),
		c.Duration("proxy-connection-timeout"),
		c.Uint64("compression-quality"),
	)
	if err != nil {
		logger.WithError(err).Error("Cannot initialize default client config because reverse proxy config is invalid")
		return err
	}
	defaultClientConfig := &pogs.ClientConfig{
		Version: pogs.InitVersion(),
		SupervisorConfig: &pogs.SupervisorConfig{
			AutoUpdateFrequency:    c.Duration("autoupdate-freq"),
			MetricsUpdateFrequency: c.Duration("metrics-update-freq"),
			GracePeriod:            c.Duration("grace-period"),
		},
		EdgeConnectionConfig: &pogs.EdgeConnectionConfig{
			NumHAConnections:    uint8(c.Int("ha-connections")),
			HeartbeatInterval:   c.Duration("heartbeat-interval"),
			Timeout:             c.Duration("dial-edge-timeout"),
			MaxFailedHeartbeats: c.Uint64("heartbeat-count"),
		},
		DoHProxyConfigs:     []*pogs.DoHProxyConfig{},
		ReverseProxyConfigs: []*pogs.ReverseProxyConfig{reverseProxyConfig},
	}

	autoupdater := updater.NewAutoUpdater(defaultClientConfig.SupervisorConfig.AutoUpdateFrequency, listeners)

	originCert, err := getOriginCert(c)
	if err != nil {
		logger.WithError(err).Error("error getting origin cert")
		return err
	}
	toEdgeTLSConfig, err := tlsconfig.CreateTunnelConfig(c)
	if err != nil {
		logger.WithError(err).Error("unable to create TLS config to connect with edge")
		return err
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		logger.WithError(err).Error("unable to parse tag")
		return err
	}

	var scope pogs.Scope
	if c.IsSet("group") == c.IsSet("system-name") {
		err = fmt.Errorf("exactly one of --group or --system-name must be specified")
		logger.WithError(err).Error("unable to determine scope")
		return err
	} else if c.IsSet("group") {
		scope = pogs.NewGroup(c.String("group"))
	} else {
		scope = pogs.NewSystemName(c.String("system-name"))
	}

	cloudflaredConfig := &connection.CloudflaredConfig{
		BuildInfo:     buildInfo,
		CloudflaredID: cloudflaredID,
		Scope:         scope,
		Tags:          tags,
	}

	serviceDiscoverer, err := serviceDiscoverer(c, logger)
	if err != nil {
		logger.WithError(err).Error("unable to create service discoverer")
		return err
	}
	supervisor, err := supervisor.NewSupervisor(defaultClientConfig, originCert, toEdgeTLSConfig,
		serviceDiscoverer, cloudflaredConfig, autoupdater, updater.SupportAutoUpdate(), logger)
	if err != nil {
		logger.WithError(err).Error("unable to create Supervisor")
		return err
	}
	return supervisor.Run(ctx)
}

func defaultOriginConfig(c *cli.Context) (pogs.OriginConfig, error) {
	if c.IsSet("hello-world") {
		return &pogs.HelloWorldOriginConfig{}, nil
	}
	originConfig := &pogs.HTTPOriginConfig{
		TCPKeepAlive:           c.Duration("proxy-tcp-keepalive"),
		DialDualStack:          !c.Bool("proxy-no-happy-eyeballs"),
		TLSHandshakeTimeout:    c.Duration("proxy-tls-timeout"),
		TLSVerify:              !c.Bool("no-tls-verify"),
		OriginCAPool:           c.String("origin-ca-pool"),
		OriginServerName:       c.String("origin-server-name"),
		MaxIdleConnections:     c.Uint64("proxy-keepalive-connections"),
		IdleConnectionTimeout:  c.Duration("proxy-keepalive-timeout"),
		ProxyConnectionTimeout: c.Duration("proxy-connection-timeout"),
		ExpectContinueTimeout:  c.Duration("proxy-expect-continue-timeout"),
		ChunkedEncoding:        c.Bool("no-chunked-encoding"),
	}
	if c.IsSet("unix-socket") {
		unixSocket, err := config.ValidateUnixSocket(c)
		if err != nil {
			return nil, errors.Wrap(err, "error validating --unix-socket")
		}
		originConfig.URLString = unixSocket
	}
	originAddr, err := config.ValidateUrl(c)
	if err != nil {
		return nil, errors.Wrap(err, "error validating origin URL")
	}
	originConfig.URLString = originAddr
	return originConfig, nil
}

func waitToShutdown(wg *sync.WaitGroup,
	errC chan error,
	shutdownC, graceShutdownC chan struct{},
	gracePeriod time.Duration,
) error {
	var err error
	if gracePeriod > 0 {
		err = waitForSignalWithGraceShutdown(errC, shutdownC, graceShutdownC, gracePeriod)
	} else {
		err = waitForSignal(errC, shutdownC)
		close(graceShutdownC)
	}

	if err != nil {
		logger.WithError(err).Error("Quitting due to error")
	} else {
		logger.Info("Quitting...")
	}
	// Wait for clean exit, discarding all errors
	go func() {
		for range errC {
		}
	}()
	wg.Wait()
	return err
}

func notifySystemd(waitForSignal *signal.Signal) {
	<-waitForSignal.Wait()
	daemon.SdNotify(false, "READY=1")
}

func writePidFile(waitForSignal *signal.Signal, pidFile string) {
	<-waitForSignal.Wait()
	file, err := os.Create(pidFile)
	if err != nil {
		logger.WithError(err).Errorf("Unable to write pid to %s", pidFile)
	}
	defer file.Close()
	fmt.Fprintf(file, "%d", os.Getpid())
}

func hostnameFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "ssh":
		return addPortIfMissing(u, 22)
	case "rdp":
		return addPortIfMissing(u, 3389)
	}
	return ""
}

func addPortIfMissing(uri *url.URL, port int) string {
	if uri.Port() != "" {
		return uri.Host
	}
	return fmt.Sprintf("%s:%d", uri.Hostname(), port)
}

func tunnelFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:   "config",
			Usage:  "Specifies a config file in YAML format.",
			Value:  config.FindDefaultConfigPath(),
			Hidden: shouldHide,
		},
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "autoupdate-freq",
			Usage:  fmt.Sprintf("Autoupdate frequency. Default is %v.", updater.DefaultCheckUpdateFreq),
			Value:  updater.DefaultCheckUpdateFreq,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "no-autoupdate",
			Usage:  "Disable periodic check for updates, restarting the server with the new version.",
			Value:  false,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "is-autoupdated",
			Usage:  "Signal the new process that Argo Tunnel client has been autoupdated",
			Value:  false,
			Hidden: true,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "edge",
			Usage:   "Address of the Cloudflare tunnel server.",
			EnvVars: []string{"TUNNEL_EDGE"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.CaCertFlag,
			Usage:   "Certificate Authority authenticating connections with Cloudflare's edge network.",
			EnvVars: []string{"TUNNEL_CACERT"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "no-tls-verify",
			Usage:   "Disables TLS verification of the certificate presented by your origin. Will allow any certificate from the origin to be accepted. Note: The connection from your machine to Cloudflare's Edge is still encrypted.",
			EnvVars: []string{"NO_TLS_VERIFY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origincert",
			Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
			EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
			Value:   findDefaultOriginCertPath(),
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.OriginCAPoolFlag,
			Usage:   "Path to the CA for the certificate of your origin. This option should be used only if your certificate is not signed by Cloudflare.",
			EnvVars: []string{"TUNNEL_ORIGIN_CA_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "url",
			Value:   "http://localhost:8080",
			Usage:   "Connect to the local webserver at `URL`.",
			EnvVars: []string{"TUNNEL_URL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "unix-socket",
			Usage:   "Path to unix socket to use instead of --url",
			EnvVars: []string{"TUNNEL_UNIX_SOCKET"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "hostname",
			Usage:   "Set a hostname on a Cloudflare zone to route traffic through this tunnel.",
			EnvVars: []string{"TUNNEL_HOSTNAME"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origin-server-name",
			Usage:   "Hostname on the origin server certificate.",
			EnvVars: []string{"TUNNEL_ORIGIN_SERVER_NAME"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "id",
			Usage:   "A unique identifier used to tie connections to this tunnel instance.",
			EnvVars: []string{"TUNNEL_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "lb-pool",
			Usage:   "The name of a (new/existing) load balancing pool to add this origin to.",
			EnvVars: []string{"TUNNEL_LB_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-key",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_KEY"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-email",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_EMAIL"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "api-ca-key",
			Usage:   "This parameter has been deprecated since version 2017.10.1.",
			EnvVars: []string{"TUNNEL_API_CA_KEY"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "metrics",
			Value:   "localhost:",
			Usage:   "Listen address for metrics reporting.",
			EnvVars: []string{"TUNNEL_METRICS"},
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "metrics-update-freq",
			Usage:   "Frequency to update tunnel metrics",
			Value:   time.Second * 5,
			EnvVars: []string{"TUNNEL_METRICS_UPDATE_FREQ"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "tag",
			Usage:   "Custom tags used to identify this tunnel, in format `KEY=VALUE`. Multiple tags may be specified",
			EnvVars: []string{"TUNNEL_TAG"},
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "heartbeat-interval",
			Usage:  "Minimum idle time before sending a heartbeat.",
			Value:  time.Second * 5,
			Hidden: true,
		}),
		altsrc.NewUint64Flag(&cli.Uint64Flag{
			Name:   "heartbeat-count",
			Usage:  "Minimum number of unacked heartbeats to send before closing the connection.",
			Value:  5,
			Hidden: true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "loglevel",
			Value:   "info",
			Usage:   "Application logging level {panic, fatal, error, warn, info, debug}. " + debugLevelWarning,
			EnvVars: []string{"TUNNEL_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "transport-loglevel",
			Aliases: []string{"proto-loglevel"}, // This flag used to be called proto-loglevel
			Value:   "warn",
			Usage:   "Transport logging level(previously called protocol logging level) {panic, fatal, error, warn, info, debug}",
			EnvVars: []string{"TUNNEL_PROTO_LOGLEVEL", "TUNNEL_TRANSPORT_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewUintFlag(&cli.UintFlag{
			Name:    "retries",
			Value:   5,
			Usage:   "Maximum number of retries for connection/protocol errors.",
			EnvVars: []string{"TUNNEL_RETRIES"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "hello-world",
			Value:   false,
			Usage:   "Run Hello World Server",
			EnvVars: []string{"TUNNEL_HELLO_WORLD"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "pidfile",
			Usage:   "Write the application's PID to this file after first successful connection.",
			EnvVars: []string{"TUNNEL_PIDFILE"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "logfile",
			Usage:   "Save application log to this file for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGFILE"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "ha-connections",
			Value:  4,
			Hidden: true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-connect-timeout",
			Usage:  "HTTP proxy timeout for establishing a new connection",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-tls-timeout",
			Usage:  "HTTP proxy timeout for completing a TLS handshake",
			Value:  time.Second * 10,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-tcp-keepalive",
			Usage:  "HTTP proxy TCP keepalive duration",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "proxy-no-happy-eyeballs",
			Usage:  "HTTP proxy should disable \"happy eyeballs\" for IPv4/v6 fallback",
			Hidden: shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "proxy-keepalive-connections",
			Usage:  "HTTP proxy maximum keepalive connection pool size",
			Value:  100,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-keepalive-timeout",
			Usage:  "HTTP proxy timeout for closing an idle connection",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-connection-timeout",
			Usage:  "HTTP proxy timeout for closing an idle connection",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-expect-continue-timeout",
			Usage:  "HTTP proxy timeout for closing an idle connection",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "proxy-dns",
			Usage:   "Run a DNS over HTTPS proxy server.",
			EnvVars: []string{"TUNNEL_DNS"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "proxy-dns-port",
			Value:   53,
			Usage:   "Listen on given port for the DNS over HTTPS proxy server.",
			EnvVars: []string{"TUNNEL_DNS_PORT"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "proxy-dns-address",
			Usage:   "Listen address for the DNS over HTTPS proxy server.",
			Value:   "localhost",
			EnvVars: []string{"TUNNEL_DNS_ADDRESS"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "proxy-dns-upstream",
			Usage:   "Upstream endpoint URL, you can specify multiple endpoints for redundancy.",
			Value:   cli.NewStringSlice("https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"),
			EnvVars: []string{"TUNNEL_DNS_UPSTREAM"},
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "grace-period",
			Usage:   "Duration to accept new requests after cloudflared receives first SIGINT/SIGTERM. A second SIGINT/SIGTERM will force cloudflared to shutdown immediately.",
			Value:   time.Second * 30,
			EnvVars: []string{"TUNNEL_GRACE_PERIOD"},
			Hidden:  true,
		}),
		altsrc.NewUintFlag(&cli.UintFlag{
			Name:    "compression-quality",
			Value:   0,
			Usage:   "(beta) Use cross-stream compression instead HTTP compression. 0-off, 1-low, 2-medium, >=3-high.",
			EnvVars: []string{"TUNNEL_COMPRESSION_LEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "no-chunked-encoding",
			Usage:   "Disables chunked transfer encoding; useful if you are running a WSGI server.",
			EnvVars: []string{"TUNNEL_NO_CHUNKED_ENCODING"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "trace-output",
			Usage:   "Name of trace output file, generated when cloudflared stops.",
			EnvVars: []string{"TUNNEL_TRACE_OUTPUT"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "use-declarative-tunnels",
			Usage:   "Test establishing connections with declarative tunnel methods.",
			EnvVars: []string{"TUNNEL_USE_DECLARATIVE"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "system-name",
			Usage:   "Unique identifier for this cloudflared instance. It can be configured individually in the Declarative Tunnel UI. Mutually exclusive with `--group`.",
			EnvVars: []string{"TUNNEL_SYSTEM_NAME"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "group",
			Usage:   "Name of a group of cloudflared instances, of which this instance should be an identical copy. They can be configured collectively in the Declarative Tunnel UI. Mutually exclusive with `--system-name`.",
			EnvVars: []string{"TUNNEL_GROUP"},
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "dial-edge-timeout",
			Usage:   "Maximum wait time to set up a connection with the edge",
			Value:   time.Second * 15,
			EnvVars: []string{"DIAL_EDGE_TIMEOUT"},
			Hidden:  true,
		}),
	}
}
