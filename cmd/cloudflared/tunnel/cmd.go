package tunnel

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	"github.com/getsentry/raven-go"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/cmd/sqlgateway"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tunneldns"
	"github.com/coreos/go-systemd/daemon"
	"github.com/facebookgo/grace/gracenet"
	"github.com/pkg/errors"
	cli "gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"

var (
	shutdownC      chan struct{}
	graceShutdownC chan struct{}
	version        string
)

func Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Specifies a config file in YAML format.",
			Value: config.FindDefaultConfigPath(),
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
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "no-tls-verify",
			Usage:   "Disables TLS verification of the certificate presented by your origin. Will allow any certificate from the origin to be accepted. Note: The connection from your machine to Cloudflare's Edge is still encrypted.",
			EnvVars: []string{"NO_TLS_VERIFY"},
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
			Usage:   "Duration to accept new requests after cloudflared receives first SIGINT/SIGTERM. A second SIGINT/SIGTERM will force cloudflared to shutdown immediately.",
			Value:   time.Second * 30,
			EnvVars: []string{"TUNNEL_GRACE_PERIOD"},
			Hidden:  true,
		}),
		altsrc.NewUintFlag(&cli.UintFlag{
			Name:    "compression-quality",
			Value:   0,
			Usage:   "Use cross-stream compression instead HTTP compression. 0-off, 1-low, 2-medium, >=3-high",
			EnvVars: []string{"TUNNEL_COMPRESSION_LEVEL"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "no-chunked-encoding",
			Usage:   "Disables chunked transfer encoding; useful if you are running a WSGI server.",
			EnvVars: []string{"TUNNEL_NO_CHUNKED_ENCODING"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "trace-output",
			Usage:   "Name of trace output file, generated when cloudflared stops.",
			EnvVars: []string{"TUNNEL_TRACE_OUTPUT"},
		}),
	}
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
			Before: func(c *cli.Context) error {
				if c.String("config") == "" {
					logger.Warnf("Cannot determine default configuration path. No file %v in %v", defaultConfigFiles, config.DefaultConfigDirs)
				}
				inputSource, err := config.FindInputSourceContext(c)
				if err != nil {
					logger.WithError(err).Infof("Cannot load configuration from %s", c.String("config"))
					return err
				} else if inputSource != nil {
					err := altsrc.ApplyInputSourceValues(c, inputSource, c.App.Flags)
					if err != nil {
						logger.WithError(err).Infof("Cannot apply configuration from %s", c.String("config"))
						return err
					}
					logger.Infof("Applied configuration from %s", c.String("config"))
				}
				return nil
			},
			Usage: "SQL Gateway is an SQL over HTTP reverse proxy",
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
		},
	}
	cmds = append(cmds, &cli.Command{
		Name:      "tunnel",
		Action:    tunnel,
		Category:  "Tunnel",
		Usage:     "Cloudflare reverse tunnelling proxy agent",
		ArgsUsage: "origin-url",
		Description: `A reverse tunnel proxy agent that connects to Cloudflare's infrastructure.
		Upon connecting, you are assigned a unique subdomain on cftunnel.com.
		You need to specify a hostname on a zone you control.
		A DNS record will be created to CNAME your hostname to the unique subdomain on cftunnel.com.
	 
		Requests made to Cloudflare's servers for your hostname will be proxied
		through the tunnel to your local webserver.`,
		Subcommands: cmds,
		Flags:       Flags(),
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
	raven.SetDSN(sentryDSN)
	var wg sync.WaitGroup
	listeners := gracenet.Net{}
	errC := make(chan error)
	connectedSignal := make(chan struct{})
	dnsReadySignal := make(chan struct{})

	if c.String("config") == "" {
		logger.Warnf("Cannot determine default configuration path. No file %v in %v", defaultConfigFiles, config.DefaultConfigDirs)
	}

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

	if c.IsSet("trace-output") {
		tmpTraceFile, err := ioutil.TempFile("", "trace")
		if err != nil {
			logger.WithError(err).Error("Failed to create new temporary file to save trace output")
		}

		defer func() {
			if err := tmpTraceFile.Close(); err != nil {
				logger.WithError(err).Error("Failed to close trace output file %s", tmpTraceFile.Name())
			}
			if err := os.Rename(tmpTraceFile.Name(), c.String("trace-output")); err != nil {
				logger.WithError(err).Errorf("Failed to rename temporary trace output file %s to %s", tmpTraceFile.Name(), c.String("trace-output"))
			} else {
				os.Remove(tmpTraceFile.Name())
			}
		}()

		if err := trace.Start(tmpTraceFile); err != nil {
			logger.WithError(err).Error("Failed to start trace")
			return errors.Wrap(err, "Error starting tracing")
		}
		defer trace.Stop()
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
	logger.Infof("Version %s", version)
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
	if updater.IsAutoupdateEnabled(c) {
		logger.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			errC <- updater.Autoupdate(c.Duration("autoupdate-freq"), &listeners, shutdownC)
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
	if c.IsSet("pidfile") {
		go writePidFile(connectedSignal, c.String("pidfile"))
	}

	// Serve DNS proxy stand-alone if no hostname or tag or app is going to run
	if dnsProxyStandAlone(c) {
		close(connectedSignal)
		// no grace period, handle SIGINT/SIGTERM immediately
		return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, 0)
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

	tunnelConfig, err := prepareTunnelConfig(c, buildInfo, version, logger, protoLogger)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- origin.StartTunnelDaemon(tunnelConfig, graceShutdownC, connectedSignal)
	}()

	return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, c.Duration("grace-period"))
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
