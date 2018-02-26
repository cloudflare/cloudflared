package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflare-warp/metrics"
	"github.com/cloudflare/cloudflare-warp/tlsconfig"
	"github.com/cloudflare/cloudflare-warp/validation"
	"github.com/cloudflare/cloudflare-warp/warp"

	"github.com/facebookgo/grace/gracenet"
	"github.com/getsentry/raven-go"
	"github.com/mitchellh/go-homedir"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"

	"github.com/coreos/go-systemd/daemon"
	"github.com/pkg/errors"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"
const configFile = "config.yml"

var listeners = gracenet.Net{}
var Version = "DEV"
var BuildTime = "unknown"
var Log *logrus.Logger

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
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "is-autoupdated",
			Usage:  "Signal the new process that Warp client has been autoupdated",
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
			Usage:   "Path to the certificate generated for your origin when you run cloudflare-warp login.",
			EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
			Value:   filepath.Join(warp.DefaultConfigDir, warp.DefaultCredentialFilename),
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
	}
	app.Action = func(c *cli.Context) error {
		raven.CapturePanic(func() { startServer(c) }, nil)
		return nil
	}
	app.Before = func(context *cli.Context) error {
		Log = logrus.New()
		inputSource, err := findInputSourceContext(context)
		if err != nil {
			return err
		} else if inputSource != nil {
			return altsrc.ApplyInputSourceValues(context, inputSource, app.Flags)
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
	wg.Add(2)
	errC := make(chan error)

	// If the user choose to supply all options through env variables,
	// c.NumFlags() == 0 && c.NArg() == 0. For warp to work, the user needs to at
	// least provide a hostname.
	if c.NumFlags() == 0 && c.NArg() == 0 && os.Getenv("TUNNEL_HOSTNAME") == "" {
		cli.ShowAppHelp(c)
		return
	}

	logLevel, err := logrus.ParseLevel(c.String("loglevel"))
	if err != nil {
		Log.WithError(err).Fatal("Unknown logging level specified")
	}
	logrus.SetLevel(logLevel)

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

	if !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0 {
		if initUpdate() {
			return
		}
		Log.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		go autoupdate(c.Duration("autoupdate-freq"), shutdownC)
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		Log.WithError(err).Fatal("Tag parse failure")
	}

	validURL, err := validateUrl(c)
	if err != nil {
		Log.WithError(err).Fatal("Error validating url")
	}

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
		validURL = "https://" + listener.Addr().String()
	}

	Log.Infof("Proxying tunnel requests to %s", validURL)

	// Fail if the user provided an old authentication method
	if c.IsSet("api-key") || c.IsSet("api-email") || c.IsSet("api-ca-key") {
		Log.Fatal("You don't need to give us your api-key anymore. Please use the new log in method. Just run cloudflare-warp login")
	}

	connectedSignal := make(chan struct{})
	go writePidFile(connectedSignal, c.String("pidfile"))

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		Log.WithError(err).Fatal("Error opening metrics server listener")
	}
	go func() {
		errC <- metrics.ServeMetrics(metricsListener, shutdownC)
		wg.Done()
	}()

	tlsConfig := tlsconfig.CLIFlags{RootCA: "cacert"}.GetConfig(c)

	// Start the server
	go func() {
		errC <- warp.StartServer(warp.ServerConfig{
			Hostname:   c.String("hostname"),
			ServerURL:  validURL,
			Tags:       tags,
			OriginCert: c.String("origincert"),

			ConnectedChan: connectedSignal,
			ShutdownChan:  shutdownC,

			Timeout:   c.Duration("proxy-connect-timeout"),
			KeepAlive: c.Duration("proxy-tcp-keepalive"),
			DualStack: !c.Bool("proxy-no-happy-eyeballs"),

			MaxIdleConns:        c.Int("proxy-keepalive-connections"),
			IdleConnTimeout:     c.Duration("proxy-keepalive-timeout"),
			TLSHandshakeTimeout: c.Duration("proxy-tls-timeout"),

			EdgeAddrs:         c.StringSlice("edge"),
			Retries:           c.Uint("retries"),
			HeartbeatInterval: c.Duration("heartbeat-interval"),
			MaxHeartbeats:     c.Uint64("heartbeat-count"),
			LBPool:            c.String("lb-pool"),
			HAConnections:     c.Int("ha-connections"),
			MetricsUpdateFreq: c.Duration("metrics-update-freq"),
			IsAutoupdated:     c.Bool("is-autoupdated"),
			TLSConfig:         tlsConfig,
			ReportedVersion:   Version,
			ProtoLogger:       protoLogger,
			Logger:            Log,
		})
		wg.Done()
	}()

	var errCode int
	err = WaitForSignal(errC, shutdownC)
	if err != nil {
		Log.WithError(err).Error("Quitting due to error")
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

func login(c *cli.Context) error {
	err := warp.Login(warp.DefaultConfigDir, warp.DefaultCredentialFilename, c.String("url"))
	if err != nil {
		fmt.Println(err)
	}
	return err
}

func update(c *cli.Context) error {
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

func findInputSourceContext(context *cli.Context) (altsrc.InputSourceContext, error) {
	if context.IsSet("config") {
		return altsrc.NewYamlSourceFromFile(context.String("config"))
	}
	dirPath, err := homedir.Expand(warp.DefaultConfigDir)
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
	fileMode := os.O_WRONLY | os.O_APPEND | os.O_CREATE | os.O_TRUNC
	// do not truncate log file if the client has been autoupdated
	if c.Bool("is-autoupdated") {
		fileMode = os.O_WRONLY | os.O_APPEND | os.O_CREATE
	}
	f, err := os.OpenFile(c.String("logfile"), fileMode, 0664)
	if err != nil {
		errors.Wrap(err, fmt.Sprintf("Cannot open file %s", c.String("logfile")))
	}
	defer f.Close()

	pathMap := lfshook.PathMap{
		logrus.InfoLevel:  c.String("logfile"),
		logrus.ErrorLevel: c.String("logfile"),
		logrus.FatalLevel: c.String("logfile"),
		logrus.PanicLevel: c.String("logfile"),
	}

	Log.Hooks.Add(lfshook.NewHook(pathMap, &logrus.JSONFormatter{}))
	protoLogger.Hooks.Add(lfshook.NewHook(pathMap, &logrus.JSONFormatter{}))

	flags := make(map[string]interface{})
	envs := make(map[string]string)

	for _, flag := range c.LocalFlagNames() {
		flags[flag] = c.Generic(flag)
	}

	// Find env variables for Warp
	for _, env := range os.Environ() {
		// All Warp env variables start with TUNNEL_
		if strings.Contains(env, "TUNNEL_") {
			vars := strings.Split(env, "=")
			if len(vars) == 2 {
				envs[vars[0]] = vars[1]
			}
		}
	}

	Log.Infof("Warp build and runtime configuration: %+v", BuildAndRuntimeInfo{
		GoOS:        runtime.GOOS,
		GoVersion:   runtime.Version(),
		GoArch:      runtime.GOARCH,
		WarpVersion: Version,
		WarpFlags:   flags,
		WarpEnvs:    envs,
	})

	return nil
}
