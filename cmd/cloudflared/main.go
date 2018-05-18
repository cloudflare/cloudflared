package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tunneldns"

	"github.com/getsentry/raven-go"
	"github.com/mitchellh/go-homedir"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"

	"github.com/coreos/go-systemd/daemon"
	"github.com/facebookgo/grace/gracenet"
	"github.com/pkg/errors"
)

const (
	sentryDSN       = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	quickStartUrl   = developerPortal + "/quickstart/quickstart/"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"
	licenseUrl      = developerPortal + "/licence/"
)

var (
	Version   = "DEV"
	BuildTime = "unknown"
)

func main() {
	metrics.RegisterBuildInfo(BuildTime, Version)
	raven.SetDSN(sentryDSN)
	raven.SetRelease(Version)

	// Shutdown channel used by the app. When closed, app must terminate.
	// May be closed by the Windows service runner.
	shutdownC := make(chan struct{})

	app := &cli.App{}
	app.Name = "cloudflared"
	app.Copyright = fmt.Sprintf(`(c) %d Cloudflare Inc.
   Use is subject to the license agreement at %s`, time.Now().Year(), licenseUrl)
	app.Usage = "Cloudflare reverse tunnelling proxy agent"
	app.ArgsUsage = "origin-url"
	app.Version = fmt.Sprintf("%s (built %s)", Version, BuildTime)
	app.Description = `A reverse tunnel proxy agent that connects to Cloudflare's infrastructure.
   Upon connecting, you are assigned a unique subdomain on cftunnel.com.
   You need to specify a hostname on a zone you control.
   A DNS record will be created to CNAME your hostname to the unique subdomain on cftunnel.com.

   Requests made to Cloudflare's servers for your hostname will be proxied
   through the tunnel to your local webserver.`
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Specifies a config file in YAML format.",
			Value: findDefaultConfigPath(),
		},
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:  "autoupdate-freq",
			Usage: "Autoupdate frequency. Default is 24h.",
			Value: time.Hour * 24,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:  "no-autoupdate",
			Usage: "Disable periodic check for updates, restarting the server with the new version.",
			Value: false,
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
			Name:    "cacert",
			Usage:   "Certificate Authority authenticating the Cloudflare tunnel connection.",
			EnvVars: []string{"TUNNEL_CACERT"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origincert",
			Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
			EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
			Value:   findDefaultOriginCertPath(),
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origin-ca-pool",
			Usage:   "Path to the CA for the certificate of your origin. This option should be used only if your certificate is not signed by Cloudflare.",
			EnvVars: []string{"TUNNEL_ORIGIN_CA_POOL"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "url",
			Value:   "https://localhost:8080",
			Usage:   "Connect to the local webserver at `URL`.",
			EnvVars: []string{"TUNNEL_URL"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "hostname",
			Usage:   "Set a hostname on a Cloudflare zone to route traffic through this tunnel.",
			EnvVars: []string{"TUNNEL_HOSTNAME"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origin-server-name",
			Usage:   "Hostname on the origin server certificate.",
			EnvVars: []string{"TUNNEL_ORIGIN_SERVER_NAME"},
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
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "metrics-update-freq",
			Usage:   "Frequency to update tunnel metrics",
			Value:   time.Second * 5,
			EnvVars: []string{"TUNNEL_METRICS_UPDATE_FREQ"},
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "tag",
			Usage:   "Custom tags used to identify this tunnel, in format `KEY=VALUE`. Multiple tags may be specified",
			EnvVars: []string{"TUNNEL_TAG"},
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
			Usage:   "Application logging level {panic, fatal, error, warn, info, debug}",
			EnvVars: []string{"TUNNEL_LOGLEVEL"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "proto-loglevel",
			Value:   "warn",
			Usage:   "Protocol logging level {panic, fatal, error, warn, info, debug}",
			EnvVars: []string{"TUNNEL_PROTO_LOGLEVEL"},
		}),
		altsrc.NewUintFlag(&cli.UintFlag{
			Name:    "retries",
			Value:   5,
			Usage:   "Maximum number of retries for connection/protocol errors.",
			EnvVars: []string{"TUNNEL_RETRIES"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "hello-world",
			Value:   false,
			Usage:   "Run Hello World Server",
			EnvVars: []string{"TUNNEL_HELLO_WORLD"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "pidfile",
			Usage:   "Write the application's PID to this file after first successful connection.",
			EnvVars: []string{"TUNNEL_PIDFILE"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "logfile",
			Usage:   "Save application log to this file for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGFILE"},
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "ha-connections",
			Value:  4,
			Hidden: true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:  "proxy-connect-timeout",
			Usage: "HTTP proxy timeout for establishing a new connection",
			Value: time.Second * 30,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:  "proxy-tls-timeout",
			Usage: "HTTP proxy timeout for completing a TLS handshake",
			Value: time.Second * 10,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:  "proxy-tcp-keepalive",
			Usage: "HTTP proxy TCP keepalive duration",
			Value: time.Second * 30,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:  "proxy-no-happy-eyeballs",
			Usage: "HTTP proxy should disable \"happy eyeballs\" for IPv4/v6 fallback",
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:  "proxy-keepalive-connections",
			Usage: "HTTP proxy maximum keepalive connection pool size",
			Value: 100,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:  "proxy-keepalive-timeout",
			Usage: "HTTP proxy timeout for closing an idle connection",
			Value: time.Second * 90,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "proxy-dns",
			Usage:   "Run a DNS over HTTPS proxy server.",
			EnvVars: []string{"TUNNEL_DNS"},
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "proxy-dns-port",
			Value:   53,
			Usage:   "Listen on given port for the DNS over HTTPS proxy server.",
			EnvVars: []string{"TUNNEL_DNS_PORT"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "proxy-dns-address",
			Usage:   "Listen address for the DNS over HTTPS proxy server.",
			Value:   "localhost",
			EnvVars: []string{"TUNNEL_DNS_ADDRESS"},
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "proxy-dns-upstream",
			Usage:   "Upstream endpoint URL, you can specify multiple endpoints for redundancy.",
			Value:   cli.NewStringSlice("https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"),
			EnvVars: []string{"TUNNEL_DNS_UPSTREAM"},
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "grace-period",
			Usage:   "Duration to accpet new requests after cloudflared receives first SIGINT/SIGTERM. A second SIGINT/SIGTERM will force cloudflared to shutdown immediately.",
			Value:   time.Second * 30,
			EnvVars: []string{"TUNNEL_GRACE_PERIOD"},
			Hidden:  true,
		}),
	}
	app.Action = func(c *cli.Context) (err error) {
		tags := make(map[string]string)
		tags["hostname"] = c.String("hostname")
		raven.SetTagsContext(tags)
		raven.CapturePanicAndWait(func() { err = startServer(c, shutdownC) }, nil)
		if err != nil {
			raven.CaptureErrorAndWait(err, nil)
		}
		return err
	}
	app.Before = func(context *cli.Context) error {
		if context.String("config") == "" {
			logger.Warnf("Cannot determine default configuration path. No file %v in %v", defaultConfigFiles, defaultConfigDirs)
		}
		inputSource, err := findInputSourceContext(context)
		if err != nil {
			logger.WithError(err).Infof("Cannot load configuration from %s", context.String("config"))
			return err
		} else if inputSource != nil {
			err := altsrc.ApplyInputSourceValues(context, inputSource, app.Flags)
			if err != nil {
				logger.WithError(err).Infof("Cannot apply configuration from %s", context.String("config"))
				return err
			}
			logger.Infof("Applied configuration from %s", context.String("config"))
		}
		return nil
	}
	app.Commands = []*cli.Command{
		{
			Name:      "update",
			Action:    update,
			Usage:     "Update the agent if a new version exists",
			ArgsUsage: " ",
			Description: `Looks for a new version on the offical download server.
   If a new version exists, updates the agent binary and quits.
   Otherwise, does nothing.

   To determine if an update happened in a script, check for error code 64.`,
		},
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
		},
		{
			Name:   "hello",
			Action: helloWorld,
			Usage:  "Run a simple \"Hello World\" server for testing Argo Tunnel.",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:  "port",
					Usage: "Listen on the selected port.",
					Value: 8080,
				},
			},
			ArgsUsage: " ", // can't be the empty string or we get the default output
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
		},
	}
	runApp(app, shutdownC)
}

func startServer(c *cli.Context, shutdownC chan struct{}) error {
	var wg sync.WaitGroup
	listeners := gracenet.Net{}
	errC := make(chan error)
	connectedSignal := make(chan struct{})
	dnsReadySignal := make(chan struct{})
	graceShutdownSignal := make(chan struct{})

	// check whether client provides enough flags or env variables. If not, print help.
	if ok := enoughOptionsSet(c); !ok {
		return nil
	}

	if err := configMainLogger(c); err != nil {
		return errors.Wrap(err, "Error configuring logger")
	}

	protoLogger, err := configProtoLogger(c)
	if err != nil {
		return errors.Wrap(err, "Error configuring protocol logger")
	}
	if c.String("logfile") != "" {
		if err := initLogFile(c, logger, protoLogger); err != nil {
			logger.Error(err)
		}
	}

	if err := handleDeprecatedOptions(c); err != nil {
		return err
	}

	buildInfo := origin.GetBuildInfo()
	logger.Infof("Build info: %+v", *buildInfo)
	logger.Infof("Version %s", Version)
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

	// update needs to be after DNS proxy is up to resolve equinox server address
	if isAutoupdateEnabled(c) {
		logger.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		wg.Add(1)
		go func(){
			defer wg.Done()
			errC <- autoupdate(c.Duration("autoupdate-freq"), &listeners, shutdownC)
		}()
	}

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
	if c.IsSet("pidFile") {
		go writePidFile(connectedSignal, c.String("pidfile"))
	}

	// Serve DNS proxy stand-alone if no hostname or tag or app is going to run
	if dnsProxyStandAlone(c) {
		close(connectedSignal)
		// no grace period, handle SIGINT/SIGTERM immediately
		return waitToShutdown(&wg, errC, shutdownC, graceShutdownSignal, 0)
	}

	if c.IsSet("hello-world") {
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

	tunnelConfig, err := prepareTunnelConfig(c, buildInfo, logger, protoLogger)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- origin.StartTunnelDaemon(tunnelConfig, graceShutdownSignal, connectedSignal)
	}()

	return waitToShutdown(&wg, errC, shutdownC, graceShutdownSignal, c.Duration("grace-period"))
}

func waitToShutdown(wg *sync.WaitGroup,
	errC chan error,
	shutdownC, graceShutdownSignal chan struct{},
	gracePeriod time.Duration,
) error {
	var err error
	if gracePeriod > 0 {
		err = waitForSignalWithGraceShutdown(errC, shutdownC, graceShutdownSignal, gracePeriod)
	} else {
		err = waitForSignal(errC, shutdownC)
		close(graceShutdownSignal)
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

func notifySystemd(waitForSignal chan struct{}) {
	<-waitForSignal
	daemon.SdNotify(false, "READY=1")
}

func writePidFile(waitForSignal chan struct{}, pidFile string) {
	<-waitForSignal
	file, err := os.Create(pidFile)
	if err != nil {
		logger.WithError(err).Errorf("Unable to write pid to %s", pidFile)
	}
	defer file.Close()
	fmt.Fprintf(file, "%d", os.Getpid())
}

func userHomeDir() (string, error) {
	// This returns the home dir of the executing user using OS-specific method
	// for discovering the home dir. It's not recommended to call this function
	// when the user has root permission as $HOME depends on what options the user
	// use with sudo.
	homeDir, err := homedir.Dir()
	if err != nil {
		logger.WithError(err).Error("Cannot determine home directory for the user")
		return "", errors.Wrap(err, "Cannot determine home directory for the user")
	}
	return homeDir, nil
}
