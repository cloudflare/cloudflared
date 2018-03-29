package main

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunneldns"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"

	"github.com/facebookgo/grace/gracenet"
	"github.com/getsentry/raven-go"
	"github.com/mitchellh/go-homedir"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"

	"github.com/coreos/go-systemd/daemon"
	"github.com/pkg/errors"
)

const (
	sentryDSN           = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"
	credentialFile      = "cert.pem"
	quickStartUrl       = "https://developers.cloudflare.com/argo-tunnel/quickstart/quickstart/"
	noAutoupdateMessage = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/argo-tunnel/reference/service/"
	licenseUrl          = "https://developers.cloudflare.com/argo-tunnel/licence/"
)

var listeners = gracenet.Net{}
var Version = "DEV"
var BuildTime = "unknown"
var Log *logrus.Logger
var defaultConfigFiles = []string{"config.yml", "config.yaml"}

// Windows default config dir was ~/cloudflare-warp in documentation; let's keep it compatible
var defaultConfigDirs = []string{"~/.cloudflared", "~/.cloudflare-warp", "~/cloudflare-warp"}

// Shutdown channel used by the app. When closed, app must terminate.
// May be closed by the Windows service runner.
var shutdownC chan struct{}

type BuildAndRuntimeInfo struct {
	GoOS        string                 `json:"go_os"`
	GoVersion   string                 `json:"go_version"`
	GoArch      string                 `json:"go_arch"`
	WarpVersion string                 `json:"warp_version"`
	WarpFlags   map[string]interface{} `json:"warp_flags"`
	WarpEnvs    map[string]string      `json:"warp_envs"`
}

func main() {
	metrics.RegisterBuildInfo(BuildTime, Version)
	raven.SetDSN(sentryDSN)
	raven.SetRelease(Version)
	shutdownC = make(chan struct{})
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
		altsrc.NewUintFlag(&cli.UintFlag{
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
			Value:   cli.NewStringSlice("https://dns.cloudflare.com/.well-known/dns-query"),
			EnvVars: []string{"TUNNEL_DNS_UPSTREAM"},
		}),
	}
	app.Action = func(c *cli.Context) error {
		raven.CapturePanic(func() { startServer(c) }, nil)
		return nil
	}
	app.Before = func(context *cli.Context) error {
		Log = logrus.New()
		inputSource, err := findInputSourceContext(context)
		if err != nil {
			Log.WithError(err).Infof("Cannot load configuration from %s", context.String("config"))
			return err
		} else if inputSource != nil {
			err := altsrc.ApplyInputSourceValues(context, inputSource, app.Flags)
			if err != nil {
				Log.WithError(err).Infof("Cannot apply configuration from %s", context.String("config"))
				return err
			}
			Log.Infof("Applied configuration from %s", context.String("config"))
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
			Action: hello,
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
					Value:   cli.NewStringSlice("https://dns.cloudflare.com/.well-known/dns-query"),
					EnvVars: []string{"TUNNEL_DNS_UPSTREAM"},
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

	// If the user choose to supply all options through env variables,
	// c.NumFlags() == 0 && c.NArg() == 0. For cloudflared to work, the user needs to at
	// least provide a hostname.
	if c.NumFlags() == 0 && c.NArg() == 0 && os.Getenv("TUNNEL_HOSTNAME") == "" {
		Log.Infof("No arguments were provided. You need to at least specify the hostname for this tunnel. See %s", quickStartUrl)
		cli.ShowAppHelp(c)
		return
	}
	logLevel, err := logrus.ParseLevel(c.String("loglevel"))
	if err != nil {
		Log.WithError(err).Fatal("Unknown logging level specified")
	}
	Log.SetLevel(logLevel)

	protoLogLevel, err := logrus.ParseLevel(c.String("proto-loglevel"))
	if err != nil {
		Log.WithError(err).Fatal("Unknown protocol logging level specified")
	}
	protoLogger := logrus.New()
	protoLogger.Level = protoLogLevel

	if c.String("logfile") != "" {
		if err := initLogFile(c, protoLogger); err != nil {
			Log.Error(err)
		}
	}

	if isAutoupdateEnabled(c) {
		if initUpdate() {
			return
		}
		Log.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		go autoupdate(c.Duration("autoupdate-freq"), shutdownC)
	}

	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil {
		Log.WithError(err).Fatal("Invalid hostname")
	}
	clientID := c.String("id")
	if !c.IsSet("id") {
		clientID = generateRandomClientID()
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		Log.WithError(err).Fatal("Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})
	if c.IsSet("hello-world") {
		wg.Add(1)
		listener, err := createListener("127.0.0.1:")
		if err != nil {
			listener.Close()
			Log.WithError(err).Fatal("Cannot start Hello World Server")
		}
		go func() {
			startHelloWorldServer(listener, shutdownC)
			wg.Done()
			listener.Close()
		}()
		c.Set("url", "https://"+listener.Addr().String())
	}

	if c.IsSet("proxy-dns") {
		wg.Add(1)
		listener, err := tunneldns.CreateListener(c.String("proxy-dns-address"), uint16(c.Uint("proxy-dns-port")), c.StringSlice("proxy-dns-upstream"))
		if err != nil {
			listener.Stop()
			Log.WithError(err).Fatal("Cannot start the DNS over HTTPS proxy server")
		}
		go func() {
			listener.Start()
			<-shutdownC
			listener.Stop()
			wg.Done()
		}()
	}

	url, err := validateUrl(c)
	if err != nil {
		Log.WithError(err).Fatal("Error validating url")
	}
	Log.Infof("Proxying tunnel requests to %s", url)

	// Fail if the user provided an old authentication method
	if c.IsSet("api-key") || c.IsSet("api-email") || c.IsSet("api-ca-key") {
		Log.Fatal("You don't need to give us your api-key anymore. Please use the new log in method. Just run cloudflared login")
	}

	// Check that the user has acquired a certificate using the log in command
	originCertPath, err := homedir.Expand(c.String("origincert"))
	if err != nil {
		Log.WithError(err).Fatalf("Cannot resolve path %s", c.String("origincert"))
	}
	ok, err := fileExists(originCertPath)
	if err != nil {
		Log.Fatalf("Cannot check if origin cert exists at path %s", c.String("origincert"))
	}
	if !ok {
		Log.Fatalf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

    %s login
`, originCertPath, os.Args[0])
	}
	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		Log.WithError(err).Fatalf("Cannot read %s to load origin certificate", originCertPath)
	}

	tunnelMetrics := origin.NewTunnelMetrics()
	httpTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   c.Duration("proxy-connect-timeout"),
			KeepAlive: c.Duration("proxy-tcp-keepalive"),
			DualStack: !c.Bool("proxy-no-happy-eyeballs"),
		}).DialContext,
		MaxIdleConns:          c.Int("proxy-keepalive-connections"),
		IdleConnTimeout:       c.Duration("proxy-keepalive-timeout"),
		TLSHandshakeTimeout:   c.Duration("proxy-tls-timeout"),
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: tlsconfig.LoadOriginCertsPool()},
	}

	if !c.IsSet("hello-world") && c.IsSet("origin-server-name") {
		httpTransport.TLSClientConfig.ServerName = c.String("origin-server-name")
	}

	tunnelConfig := &origin.TunnelConfig{
		EdgeAddrs:         c.StringSlice("edge"),
		OriginUrl:         url,
		Hostname:          hostname,
		OriginCert:        originCert,
		TlsConfig:         tlsconfig.CreateTunnelConfig(c, c.StringSlice("edge")),
		ClientTlsConfig:   httpTransport.TLSClientConfig,
		Retries:           c.Uint("retries"),
		HeartbeatInterval: c.Duration("heartbeat-interval"),
		MaxHeartbeats:     c.Uint64("heartbeat-count"),
		ClientID:          clientID,
		ReportedVersion:   Version,
		LBPool:            c.String("lb-pool"),
		Tags:              tags,
		HAConnections:     c.Int("ha-connections"),
		HTTPTransport:     httpTransport,
		Metrics:           tunnelMetrics,
		MetricsUpdateFreq: c.Duration("metrics-update-freq"),
		ProtocolLogger:    protoLogger,
		Logger:            Log,
		IsAutoupdated:     c.Bool("is-autoupdated"),
	}
	connectedSignal := make(chan struct{})

	go writePidFile(connectedSignal, c.String("pidfile"))
	go func() {
		errC <- origin.StartTunnelDaemon(tunnelConfig, shutdownC, connectedSignal)
		wg.Done()
	}()

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		Log.WithError(err).Fatal("Error opening metrics server listener")
	}
	go func() {
		errC <- metrics.ServeMetrics(metricsListener, shutdownC)
		wg.Done()
	}()

	var errCode int
	err = WaitForSignal(errC, shutdownC)
	if err != nil {
		Log.WithError(err).Fatal("Quitting due to error")
		raven.CaptureErrorAndWait(err, nil)
		errCode = 1
	} else {
		Log.Info("Quitting...")
	}
	// Wait for clean exit, discarding all errors
	go func() {
		for range errC {
		}
	}()
	wg.Wait()
	os.Exit(errCode)
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

func update(_ *cli.Context) error {
	if updateApplied() {
		os.Exit(64)
	}
	return nil
}

func initUpdate() bool {
	if updateApplied() {
		os.Args = append(os.Args, "--is-autoupdated=true")
		if _, err := listeners.StartProcess(); err != nil {
			Log.WithError(err).Error("Unable to restart server automatically")
			return false
		}
		return true
	}
	return false
}

func autoupdate(freq time.Duration, shutdownC chan struct{}) {
	for {
		if updateApplied() {
			os.Args = append(os.Args, "--is-autoupdated=true")
			if _, err := listeners.StartProcess(); err != nil {
				Log.WithError(err).Error("Unable to restart server automatically")
			}
			close(shutdownC)
			return
		}
		time.Sleep(freq)
	}
}

func updateApplied() bool {
	releaseInfo := checkForUpdates()
	if releaseInfo.Updated {
		Log.Infof("Updated to version %s", releaseInfo.Version)
		return true
	}
	if releaseInfo.Error != nil {
		Log.WithError(releaseInfo.Error).Error("Update check failed")
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

// returns the first path that contains a cert.pem file. If none of the defaultConfigDirs
// (differs by OS for legacy reasons) contains a cert.pem file, return empty string
func findDefaultOriginCertPath() string {
	for _, defaultConfigDir := range defaultConfigDirs {
		originCertPath, _ := homedir.Expand(filepath.Join(defaultConfigDir, credentialFile))
		if ok, _ := fileExists(originCertPath); ok {
			return originCertPath
		}
	}
	return ""
}

// returns the firt path that contains a config file. If none of the combination of
// defaultConfigDirs (differs by OS for legacy reasons) and defaultConfigFiles
// contains a config file, return empty string
func findDefaultConfigPath() string {
	for _, configDir := range defaultConfigDirs {
		for _, configFile := range defaultConfigFiles {
			dirPath, err := homedir.Expand(configDir)
			if err != nil {
				return ""
			}
			path := filepath.Join(dirPath, configFile)
			if ok, _ := fileExists(path); ok {
				return path
			}
		}
	}
	return ""
}

func findInputSourceContext(context *cli.Context) (altsrc.InputSourceContext, error) {
	if context.String("config") != "" {
		return altsrc.NewYamlSourceFromFile(context.String("config"))
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
		Log.WithError(err).Errorf("Unable to write pid to %s", pidFile)
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

func initLogFile(c *cli.Context, protoLogger *logrus.Logger) error {
	filePath, err := homedir.Expand(c.String("logfile"))
	if err != nil {
		return errors.Wrap(err, "Cannot resolve logfile path")
	}

	fileMode := os.O_WRONLY | os.O_APPEND | os.O_CREATE | os.O_TRUNC
	// do not truncate log file if the client has been autoupdated
	if c.Bool("is-autoupdated") {
		fileMode = os.O_WRONLY | os.O_APPEND | os.O_CREATE
	}
	f, err := os.OpenFile(filePath, fileMode, 0664)
	if err != nil {
		errors.Wrap(err, fmt.Sprintf("Cannot open file %s", filePath))
	}
	defer f.Close()
	pathMap := lfshook.PathMap{
		logrus.InfoLevel:  filePath,
		logrus.ErrorLevel: filePath,
		logrus.FatalLevel: filePath,
		logrus.PanicLevel: filePath,
	}

	Log.Hooks.Add(lfshook.NewHook(pathMap, &logrus.JSONFormatter{}))
	protoLogger.Hooks.Add(lfshook.NewHook(pathMap, &logrus.JSONFormatter{}))

	flags := make(map[string]interface{})
	envs := make(map[string]string)

	for _, flag := range c.LocalFlagNames() {
		flags[flag] = c.Generic(flag)
	}

	// Find env variables for Argo Tunnel
	for _, env := range os.Environ() {
		// All Argo Tunnel env variables start with TUNNEL_
		if strings.Contains(env, "TUNNEL_") {
			vars := strings.Split(env, "=")
			if len(vars) == 2 {
				envs[vars[0]] = vars[1]
			}
		}
	}

	Log.Infof("Argo Tunnel build and runtime configuration: %+v", BuildAndRuntimeInfo{
		GoOS:        runtime.GOOS,
		GoVersion:   runtime.Version(),
		GoArch:      runtime.GOARCH,
		WarpVersion: Version,
		WarpFlags:   flags,
		WarpEnvs:    envs,
	})

	return nil
}

func isAutoupdateEnabled(c *cli.Context) bool {
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		Log.Info(noAutoupdateMessage)
		return false
	}

	return !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0
}
