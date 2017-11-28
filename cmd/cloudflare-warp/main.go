package main

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflare-warp/metrics"
	"github.com/cloudflare/cloudflare-warp/origin"
	"github.com/cloudflare/cloudflare-warp/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflare-warp/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflare-warp/validation"

	log "github.com/Sirupsen/logrus"
	"github.com/facebookgo/grace/gracenet"
	raven "github.com/getsentry/raven-go"
	homedir "github.com/mitchellh/go-homedir"
	cli "gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"

	"github.com/coreos/go-systemd/daemon"
	"github.com/pkg/errors"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"
const defaultConfigDir = "~/.cloudflare-warp"
const credentialFile = "cert.pem"
const configFile = "config.yml"

var listeners = gracenet.Net{}
var Version = "DEV"
var BuildTime = "unknown"

// Shutdown channel used by the app. When closed, app must terminate.
// May be closed by the Windows service runner.
var shutdownC chan struct{}

func main() {
	metrics.RegisterBuildInfo(BuildTime, Version)
	raven.SetDSN(sentryDSN)
	raven.SetRelease(Version)
	shutdownC = make(chan struct{})
	app := &cli.App{}
	app.Name = "cloudflare-warp"
	app.Copyright = `(c) 2017 Cloudflare Inc.
   Use is subject to the license agreement at https://warp.cloudflare.com/licence/`
	app.Usage = "Cloudflare reverse tunnelling proxy agent \033[1;31m*BETA*\033[0m"
	app.ArgsUsage = "origin-url"
	app.Version = fmt.Sprintf("%s (built %s)", Version, BuildTime)
	app.Description = `A reverse tunnel proxy agent that connects to Cloudflare's infrastructure.
   Upon connecting, you are assigned a unique subdomain on cftunnel.com.
   Alternatively, you can specify a hostname on a zone you control.

   Requests made to Cloudflare's servers for your hostname will be proxied
   through the tunnel to your local webserver.

WARNING:
   ` + "\033[1;31m*** THIS IS A BETA VERSION OF THE CLOUDFLARE WARP AGENT ***\033[0m" + `

   At this time, do not use Cloudflare Warp for connecting production servers to Cloudflare.
   Availability and reliability of this service is not guaranteed through the beta period.`
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "config",
			Usage: "Specifies a config file in YAML format.",
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
			Usage:   "Path to the certificate generated for your origin when you run cloudflare-warp login.",
			EnvVars: []string{"ORIGIN_CERT"},
			Value:   filepath.Join(defaultConfigDir, credentialFile),
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "url",
			Value:   "http://localhost:8080",
			Usage:   "Connect to the local webserver at `URL`.",
			EnvVars: []string{"TUNNEL_URL"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "hostname",
			Usage:   "Set a hostname on a Cloudflare zone to route traffic through this tunnel.",
			EnvVars: []string{"TUNNEL_HOSTNAME"},
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
			Name:  "hello-world",
			Usage: "Run Hello World Server",
			Value: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "pidfile",
			Usage:   "Write the application's PID to this file after first successful connection.",
			EnvVars: []string{"TUNNEL_PIDFILE"},
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "ha-connections",
			Value:  4,
			Hidden: true,
		}),
	}
	app.Action = func(c *cli.Context) error {
		raven.CapturePanic(func() { startServer(c) }, nil)
		return nil
	}
	app.Before = func(context *cli.Context) error {
		inputSource, err := findInputSourceContext(context)
		if err != nil {
			return err
		} else if inputSource != nil {
			return altsrc.ApplyInputSourceValues(context, inputSource, app.Flags)
		}
		return nil
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:      "update",
			Action:    update,
			Usage:     "Update the agent if a new version exists",
			ArgsUsage: " ",
			Description: `Looks for a new version on the offical download server.
   If a new version exists, updates the agent binary and quits.
   Otherwise, does nothing.

   To determine if an update happened in a script, check for error code 64.`,
		},
		&cli.Command{
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
		&cli.Command{
			Name:   "hello",
			Action: hello,
			Usage:  "Run a simple \"Hello World\" server for testing Cloudflare Warp.",
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:  "port",
					Usage: "Listen on the selected port.",
					Value: 8080,
				},
			},
			ArgsUsage: " ", // can't be the empty string or we get the default output
		},
	}
	runApp(app)
}

func startServer(c *cli.Context) {
	var wg sync.WaitGroup
	errC := make(chan error)
	wg.Add(2)

	if c.NumFlags() == 0 && c.NArg() == 0 {
		cli.ShowAppHelp(c)
		return
	}

	logLevel, err := log.ParseLevel(c.String("loglevel"))
	if err != nil {
		log.WithError(err).Fatal("Unknown logging level specified")
	}

	log.SetLevel(logLevel)
	protoLogLevel, err := log.ParseLevel(c.String("proto-loglevel"))
	if err != nil {
		log.WithError(err).Fatal("Unknown protocol logging level specified")
	}
	protoLogger := log.New()
	protoLogger.Level = protoLogLevel

	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil {
		log.WithError(err).Fatal("Invalid hostname")

	}
	clientID := c.String("id")
	if !c.IsSet("id") {
		clientID = generateRandomClientID()
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		log.WithError(err).Fatal("Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})

	if c.IsSet("hello-world") {
		wg.Add(1)
		listener, err := findAvailablePort()
		if err != nil {
			listener.Close()
			log.WithError(err).Fatal("Cannot start Hello World Server")
		}
		go func() {
			startHelloWorldServer(listener, shutdownC)
			wg.Done()
			listener.Close()
		}()
		c.Set("url", "http://"+listener.Addr().String())
		log.Infof("Starting Hello World Server at %s", c.String("url"))
	}

	url, err := validateUrl(c)
	if err != nil {
		log.WithError(err).Fatal("Error validating url")
	}
	log.Infof("Proxying tunnel requests to %s", url)

	// Fail if the user provided an old authentication method
	if c.IsSet("api-key") || c.IsSet("api-email") || c.IsSet("api-ca-key") {
		log.Fatal("You don't need to give us your api-key anymore. Please use the new log in method. Just run cloudflare-warp login")
	}

	// Check that the user has acquired a certificate using the log in command
	originCertPath, err := homedir.Expand(c.String("origincert"))
	if err != nil {
		log.WithError(err).Fatalf("Cannot resolve path %s", c.String("origincert"))
	}
	ok, err := fileExists(originCertPath)
	if !ok {
		log.Fatalf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

    %s login
`, originCertPath, os.Args[0])
	}
	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		log.WithError(err).Fatalf("Cannot read %s to load origin certificate", originCertPath)
	}
	tunnelMetrics := origin.NewTunnelMetrics()
	tunnelConfig := &origin.TunnelConfig{
		EdgeAddrs:         c.StringSlice("edge"),
		OriginUrl:         url,
		Hostname:          hostname,
		OriginCert:        originCert,
		TlsConfig:         &tls.Config{},
		Retries:           c.Uint("retries"),
		HeartbeatInterval: c.Duration("heartbeat-interval"),
		MaxHeartbeats:     c.Uint64("heartbeat-count"),
		ClientID:          clientID,
		ReportedVersion:   Version,
		LBPool:            c.String("lb-pool"),
		Tags:              tags,
		HAConnections:     c.Int("ha-connections"),
		Metrics:           tunnelMetrics,
		MetricsUpdateFreq: c.Duration("metrics-update-freq"),
		ProtocolLogger:    protoLogger,
	}
	connectedSignal := make(chan struct{})

	tunnelConfig.TlsConfig = tlsconfig.CLIFlags{RootCA: "cacert"}.GetConfig(c)
	if tunnelConfig.TlsConfig.RootCAs == nil {
		tunnelConfig.TlsConfig.RootCAs = GetCloudflareRootCA()
		tunnelConfig.TlsConfig.ServerName = "cftunnel.com"
	} else if len(tunnelConfig.EdgeAddrs) > 0 {
		// Set for development environments and for testing specific origintunneld instances
		tunnelConfig.TlsConfig.ServerName, _, _ = net.SplitHostPort(tunnelConfig.EdgeAddrs[0])
	}

	go writePidFile(connectedSignal, c.String("pidfile"))
	go func() {
		errC <- origin.StartTunnelDaemon(tunnelConfig, shutdownC, connectedSignal)
		wg.Done()
	}()

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		log.WithError(err).Fatal("Error opening metrics server listener")
	}
	go func() {
		errC <- metrics.ServeMetrics(metricsListener, shutdownC)
		wg.Done()
	}()

	if !c.Bool("no-autoupdate") {
		log.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		go autoupdate(c.Duration("autoupdate-period"), shutdownC)
	}

	err = WaitForSignal(errC, shutdownC)
	if err != nil {
		log.WithError(err).Error("Quitting due to error")
		raven.CaptureErrorAndWait(err, nil)
	} else {
		log.Info("Quitting...")
	}
	// Wait for clean exit, discarding all errors
	go func() {
		for range errC {
		}
	}()
	wg.Wait()
}

func WaitForSignal(errC chan error, shutdownC chan struct{}) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	select {
	case err := <-errC:
		close(shutdownC)
		return err
	case <-signals:
		close(shutdownC)
	case <-shutdownC:
	}
	return nil
}

func update(c *cli.Context) error {
	if updateApplied() {
		os.Exit(64)
	}
	return nil
}

func autoupdate(frequency time.Duration, shutdownC chan struct{}) {
	if int64(frequency) == 0 {
		return
	}
	for {
		if updateApplied() {
			if _, err := listeners.StartProcess(); err != nil {
				log.WithError(err).Error("Unable to restart server automatically")
			}
			close(shutdownC)
			return
		}
		time.Sleep(frequency)
	}
}

func updateApplied() bool {
	releaseInfo := checkForUpdates()
	if releaseInfo.Updated {
		log.Infof("Updated to version %s", releaseInfo.Version)
		return true
	}
	if releaseInfo.Error != nil {
		log.WithError(releaseInfo.Error).Error("Update check failed")
	}
	return false
}

func fileExists(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// ignore missing files
			return false, nil
		}
		return false, err
	}
	f.Close()
	return true, nil
}

func findInputSourceContext(context *cli.Context) (altsrc.InputSourceContext, error) {
	if context.IsSet("config") {
		return altsrc.NewYamlSourceFromFile(context.String("config"))
	}
	dirPath, err := homedir.Expand(defaultConfigDir)
	if err != nil {
		return nil, nil
	}
	for _, path := range []string{
		filepath.Join(dirPath, "/config.yml"),
		filepath.Join(dirPath, "/config.yaml"),
	} {
		ok, err := fileExists(path)
		if ok {
			return altsrc.NewYamlSourceFromFile(path)
		} else if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func generateRandomClientID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := make([]byte, 32)
	r.Read(id)
	return hex.EncodeToString(id)
}

func writePidFile(waitForSignal chan struct{}, pidFile string) {
	<-waitForSignal
	daemon.SdNotify(false, "READY=1")
	if pidFile == "" {
		return
	}
	file, err := os.Create(pidFile)
	if err != nil {
		log.WithError(err).Errorf("Unable to write pid to %s", pidFile)
	}
	defer file.Close()
	fmt.Fprintf(file, "%d", os.Getpid())
}

// validate url. It can be either from --url or argument
func validateUrl(c *cli.Context) (string, error) {
	var url = c.String("url")
	if c.NArg() > 0 {
		if c.IsSet("url") {
			return "", errors.New("Specified origin urls using both --url and argument. Decide which one you want, I can only support one.")
		}
		url = c.Args().Get(0)
	}
	validUrl, err := validation.ValidateUrl(url)
	return validUrl, err
}
