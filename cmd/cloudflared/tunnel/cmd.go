package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/ui"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/dbconnect"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunneldns"
	"github.com/cloudflare/cloudflared/tunnelstore"

	"github.com/coreos/go-systemd/daemon"
	"github.com/facebookgo/grace/gracenet"
	"github.com/getsentry/raven-go"
	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const (
	sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"

	sshLogFileDirectory = "/usr/local/var/log/cloudflared/"

	// sshPortFlag is the port on localhost the cloudflared ssh server will run on
	sshPortFlag = "local-ssh-port"

	// sshIdleTimeoutFlag defines the duration a SSH session can remain idle before being closed
	sshIdleTimeoutFlag = "ssh-idle-timeout"

	// sshMaxTimeoutFlag defines the max duration a SSH session can remain open for
	sshMaxTimeoutFlag = "ssh-max-timeout"

	// bucketNameFlag is the bucket name to use for the SSH log uploader
	bucketNameFlag = "bucket-name"

	// regionNameFlag is the AWS region name to use for the SSH log uploader
	regionNameFlag = "region-name"

	// secretIDFlag is the Secret id of SSH log uploader
	secretIDFlag = "secret-id"

	// accessKeyIDFlag is the Access key id of SSH log uploader
	accessKeyIDFlag = "access-key-id"

	// sessionTokenIDFlag is the Session token of SSH log uploader
	sessionTokenIDFlag = "session-token"

	// s3URLFlag is the S3 URL of SSH log uploader (e.g. don't use AWS s3 and use google storage bucket instead)
	s3URLFlag = "s3-url-host"

	// hostKeyPath is the path of the dir to save SSH host keys too
	hostKeyPath = "host-key-path"

	// uiFlag is to enable launching cloudflared in interactive UI mode
	uiFlag = "ui"

	debugLevelWarning = "At debug level, request URL, method, protocol, content legnth and header will be logged. " +
		"Response status, content length and header will also be logged in debug level."
)

var (
	shutdownC      chan struct{}
	graceShutdownC chan struct{}
	version        string
)

func Flags() []cli.Flag {
	return tunnelFlags(true)
}

func Commands() []*cli.Command {
	subcommands := []*cli.Command{
		buildLoginSubcommand(false),
		buildCreateCommand(),
		buildRouteCommand(),
		buildRunCommand(),
		buildListCommand(),
		buildIngressSubcommand(),
		buildDeleteCommand(),
		buildCleanupCommand(),
		// for compatibility, allow following as tunnel subcommands
		tunneldns.Command(true),
		dbConnectCmd(),
	}

	return []*cli.Command{
		buildTunnelCommand(subcommands),
		// for compatibility, allow following as top-level subcommands
		buildLoginSubcommand(true),
		dbConnectCmd(),
	}
}

func buildTunnelCommand(subcommands []*cli.Command) *cli.Command {
	return &cli.Command{
		Name:      "tunnel",
		Action:    cliutil.ErrorHandler(TunnelCommand),
		Before:    SetFlagsFromConfigFile,
		Category:  "Tunnel",
		Usage:     "Make a locally-running web service accessible over the internet using Argo Tunnel.",
		ArgsUsage: " ",
		Description: `Argo Tunnel asks you to specify a hostname on a Cloudflare-powered
		domain you control and a local address. Traffic from that hostname is routed
		(optionally via a Cloudflare Load Balancer) to this machine and appears on the
		specified port where it can be served.

		This feature requires your Cloudflare account be subscribed to the Argo Smart Routing feature.

		To use, begin by calling login to download a certificate:

			$ cloudflared tunnel login

		With your certificate installed you can then launch your first tunnel,
		replacing my.site.com with a subdomain of your site:

			$ cloudflared tunnel --hostname my.site.com --url http://localhost:8080

		If you have a web server running on port 8080 (in this example), it will be available on
		the internet!`,
		Subcommands: subcommands,
		Flags:       tunnelFlags(false),
	}
}

func TunnelCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if name := c.String("name"); name != "" { // Start a named tunnel
		return runAdhocNamedTunnel(sc, name)
	}
	if ref := config.GetConfiguration().TunnelID; ref != "" {
		return fmt.Errorf("Use `cloudflared tunnel run` to start tunnel %s", ref)
	}

	// Start a classic tunnel
	return runClassicTunnel(sc)
}

func Init(v string, s, g chan struct{}) {
	version, shutdownC, graceShutdownC = v, s, g
}

// runAdhocNamedTunnel create, route and run a named tunnel in one command
func runAdhocNamedTunnel(sc *subcommandContext, name string) error {
	tunnel, ok, err := sc.tunnelActive(name)
	if err != nil || !ok {
		tunnel, err = sc.create(name)
		if err != nil {
			return errors.Wrap(err, "failed to create tunnel")
		}
	} else {
		sc.logger.Infof("Tunnel already created with ID %s", tunnel.ID)
	}

	if r, ok := routeFromFlag(sc.c); ok {
		if res, err := sc.route(tunnel.ID, r); err != nil {
			sc.logger.Errorf("failed to create route, please create it manually. err: %v.", err)
		} else {
			sc.logger.Infof(res.SuccessSummary())
		}
	}

	if err := sc.run(tunnel.ID); err != nil {
		return errors.Wrap(err, "error running tunnel")
	}

	return nil
}

// runClassicTunnel creates a "classic" non-named tunnel
func runClassicTunnel(sc *subcommandContext) error {
	return StartServer(sc.c, version, shutdownC, graceShutdownC, nil, sc.logger, sc.isUIEnabled)
}

func routeFromFlag(c *cli.Context) (tunnelstore.Route, bool) {
	if hostname := c.String("hostname"); hostname != "" {
		if lbPool := c.String("lb-pool"); lbPool != "" {
			return tunnelstore.NewLBRoute(hostname, lbPool), true
		}
		return tunnelstore.NewDNSRoute(hostname), true
	}
	return nil, false
}

func StartServer(
	c *cli.Context,
	version string,
	shutdownC,
	graceShutdownC chan struct{},
	namedTunnel *connection.NamedTunnelConfig,
	generalLogger logger.Service,
	isUIEnabled bool,
) error {
	_ = raven.SetDSN(sentryDSN)
	var wg sync.WaitGroup
	listeners := gracenet.Net{}
	errC := make(chan error)
	connectedSignal := signal.New(make(chan struct{}))
	dnsReadySignal := make(chan struct{})

	if config.GetConfiguration().Source() == "" {
		generalLogger.Infof(config.ErrNoConfigFile.Error())
	}

	if c.IsSet("trace-output") {
		tmpTraceFile, err := ioutil.TempFile("", "trace")
		if err != nil {
			generalLogger.Errorf("Failed to create new temporary file to save trace output: %s", err)
		}

		defer func() {
			if err := tmpTraceFile.Close(); err != nil {
				generalLogger.Errorf("Failed to close trace output file %s with error: %s", tmpTraceFile.Name(), err)
			}
			if err := os.Rename(tmpTraceFile.Name(), c.String("trace-output")); err != nil {
				generalLogger.Errorf("Failed to rename temporary trace output file %s to %s with error: %s", tmpTraceFile.Name(), c.String("trace-output"), err)
			} else {
				err := os.Remove(tmpTraceFile.Name())
				if err != nil {
					generalLogger.Errorf("Failed to remove the temporary trace file %s with error: %s", tmpTraceFile.Name(), err)
				}
			}
		}()

		if err := trace.Start(tmpTraceFile); err != nil {
			generalLogger.Errorf("Failed to start trace: %s", err)
			return errors.Wrap(err, "Error starting tracing")
		}
		defer trace.Stop()
	}

	buildInfo := buildinfo.GetBuildInfo(version)
	buildInfo.Log(generalLogger)
	logClientOptions(c, generalLogger)

	if c.IsSet("proxy-dns") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.IsSet("proxy-dns-odoh") {
				errC <- runDNSProxyServer(c, dnsReadySignal, shutdownC, generalLogger, true)
			} else {
				errC <- runDNSProxyServer(c, dnsReadySignal, shutdownC, generalLogger, false)
			}
		}()
	} else {
		close(dnsReadySignal)
	}

	// Wait for proxy-dns to come up (if used)
	<-dnsReadySignal

	go notifySystemd(connectedSignal)
	if c.IsSet("pidfile") {
		go writePidFile(connectedSignal, c.String("pidfile"), generalLogger)
	}

	cloudflaredID, err := uuid.NewRandom()
	if err != nil {
		generalLogger.Errorf("Cannot generate cloudflared ID: %s", err)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-shutdownC
		cancel()
	}()

	// update needs to be after DNS proxy is up to resolve equinox server address
	if updater.IsAutoupdateEnabled(c, generalLogger) {
		generalLogger.Infof("Autoupdate frequency is set to %v", c.Duration("autoupdate-freq"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			autoupdater := updater.NewAutoUpdater(c.Duration("autoupdate-freq"), &listeners, generalLogger)
			errC <- autoupdater.Run(ctx)
		}()
	}

	// Serve DNS proxy stand-alone if no hostname or tag or app is going to run
	if dnsProxyStandAlone(c) {
		connectedSignal.Notify()
		// no grace period, handle SIGINT/SIGTERM immediately
		return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, 0, generalLogger)
	}

	url := c.String("url")
	hostname := c.String("hostname")
	if url == hostname && url != "" && hostname != "" {
		errText := "hostname and url shouldn't match. See --help for more information"
		generalLogger.Error(errText)
		return fmt.Errorf(errText)
	}

	transportLogger, err := logger.CreateTransportLoggerFromContext(c, isUIEnabled)
	if err != nil {
		return errors.Wrap(err, "error setting up transport logger")
	}

	readinessCh := make(chan connection.Event, 16)
	uiCh := make(chan connection.Event, 16)
	eventChannels := []chan connection.Event{
		readinessCh,
		uiCh,
	}
	tunnelConfig, ingressRules, err := prepareTunnelConfig(c, buildInfo, version, generalLogger, transportLogger, namedTunnel, isUIEnabled, eventChannels)
	if err != nil {
		generalLogger.Errorf("Couldn't start tunnel: %v", err)
		return err
	}

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		generalLogger.Errorf("Error opening metrics server listener: %s", err)
		return errors.Wrap(err, "Error opening metrics server listener")
	}
	defer metricsListener.Close()
	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- metrics.ServeMetrics(metricsListener, shutdownC, readinessCh, generalLogger)
	}()

	ingressRules.StartOrigins(&wg, generalLogger, shutdownC, errC)

	reconnectCh := make(chan origin.ReconnectSignal, 1)
	if c.IsSet("stdin-control") {
		generalLogger.Info("Enabling control through stdin")
		go stdinControl(reconnectCh, generalLogger)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		errC <- origin.StartTunnelDaemon(ctx, tunnelConfig, connectedSignal, cloudflaredID, reconnectCh)
	}()

	if isUIEnabled {
		tunnelInfo := ui.NewUIModel(
			version,
			hostname,
			metricsListener.Addr().String(),
			&ingressRules,
			tunnelConfig.HAConnections,
		)
		logLevels, err := logger.ParseLevelString(c.String("loglevel"))
		if err != nil {
			return err
		}
		tunnelInfo.LaunchUI(ctx, generalLogger, transportLogger, logLevels, uiCh)
	}

	return waitToShutdown(&wg, errC, shutdownC, graceShutdownC, c.Duration("grace-period"), generalLogger)
}

// forceSetFlag attempts to set the given flag value in the closest context that has it defined
func forceSetFlag(c *cli.Context, name, value string) {
	for _, ctx := range c.Lineage() {
		if err := ctx.Set(name, value); err == nil {
			break
		}
	}
}

func SetFlagsFromConfigFile(c *cli.Context) error {
	const exitCode = 1
	log, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return cliutil.PrintLoggerSetupError("error setting up logger", err)
	}
	inputSource, err := config.ReadConfigFile(c, log)
	if err != nil {
		if err == config.ErrNoConfigFile {
			return nil
		}
		return cli.Exit(err, exitCode)
	}
	targetFlags := c.Command.Flags
	if c.Command.Name == "" {
		targetFlags = c.App.Flags
	}
	if err := altsrc.ApplyInputSourceValues(c, inputSource, targetFlags); err != nil {
		return cli.Exit(err, exitCode)
	}
	return nil
}

func waitToShutdown(wg *sync.WaitGroup,
	errC chan error,
	shutdownC, graceShutdownC chan struct{},
	gracePeriod time.Duration,
	logger logger.Service,
) error {
	var err error
	if gracePeriod > 0 {
		err = waitForSignalWithGraceShutdown(errC, shutdownC, graceShutdownC, gracePeriod, logger)
	} else {
		err = waitForSignal(errC, shutdownC, logger)
		close(graceShutdownC)
	}

	if err != nil {
		logger.Errorf("Quitting due to error: %s", err)
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

func writePidFile(waitForSignal *signal.Signal, pidFile string, logger logger.Service) {
	<-waitForSignal.Wait()
	expandedPath, err := homedir.Expand(pidFile)
	if err != nil {
		logger.Errorf("Unable to expand %s, try to use absolute path in --pidfile: %s", pidFile, err)
		return
	}
	file, err := os.Create(expandedPath)
	if err != nil {
		logger.Errorf("Unable to write pid to %s: %s", expandedPath, err)
		return
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
	case "smb":
		return addPortIfMissing(u, 445)
	case "tcp":
		return addPortIfMissing(u, 7864) // just a random port since there isn't a default in this case
	}
	return ""
}

func addPortIfMissing(uri *url.URL, port int) string {
	if uri.Port() != "" {
		return uri.Host
	}
	return fmt.Sprintf("%s:%d", uri.Hostname(), port)
}

func dbConnectCmd() *cli.Command {
	cmd := dbconnect.Cmd()

	// Append the tunnel commands so users can customize the daemon settings.
	cmd.Flags = appendFlags(Flags(), cmd.Flags...)

	// Override before to run tunnel validation before dbconnect validation.
	cmd.Before = func(c *cli.Context) error {
		err := SetFlagsFromConfigFile(c)
		if err == nil {
			err = dbconnect.CmdBefore(c)
		}
		return err
	}

	// Override action to setup the Proxy, then if successful, start the tunnel daemon.
	cmd.Action = cliutil.ErrorHandler(func(c *cli.Context) error {
		err := dbconnect.CmdAction(c)
		if err == nil {
			err = TunnelCommand(c)
		}
		return err
	})

	return cmd
}

// appendFlags will append extra flags to a slice of flags.
//
// The cli package will panic if two flags exist with the same name,
// so if extraFlags contains a flag that was already defined, modify the
// original flags to use the extra version.
func appendFlags(flags []cli.Flag, extraFlags ...cli.Flag) []cli.Flag {
	for _, extra := range extraFlags {
		var found bool

		// Check if an extra flag overrides an existing flag.
		for i, flag := range flags {
			if reflect.DeepEqual(extra.Names(), flag.Names()) {
				flags[i] = extra
				found = true
				break
			}
		}

		// Append the extra flag if it has nothing to override.
		if !found {
			flags = append(flags, extra)
		}
	}

	return flags
}

func tunnelFlags(shouldHide bool) []cli.Flag {
	flags := configureCloudflaredFlags(shouldHide)
	flags = append(flags, configureProxyFlags(shouldHide)...)
	flags = append(flags, configureLoggingFlags(shouldHide)...)
	flags = append(flags, configureProxyDNSFlags(shouldHide)...)
	flags = append(flags, []cli.Flag{
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "is-autoupdated",
			Usage:  "Signal the new process that Argo Tunnel client has been autoupdated",
			Value:  false,
			Hidden: true,
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "edge",
			Usage:   "Address of the Cloudflare tunnel server. Only works in Cloudflare's internal testing environment.",
			EnvVars: []string{"TUNNEL_EDGE"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.CaCertFlag,
			Usage:   "Certificate Authority authenticating connections with Cloudflare's edge network.",
			EnvVars: []string{"TUNNEL_CACERT"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "hostname",
			Usage:   "Set a hostname on a Cloudflare zone to route traffic through this tunnel.",
			EnvVars: []string{"TUNNEL_HOSTNAME"},
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
			Name:    "api-url",
			Usage:   "Base URL for Cloudflare API v4",
			EnvVars: []string{"TUNNEL_API_URL"},
			Value:   "https://api.cloudflare.com/client/v4",
			Hidden:  true,
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
		altsrc.NewUintFlag(&cli.UintFlag{
			Name:    "retries",
			Value:   5,
			Usage:   "Maximum number of retries for connection/protocol errors.",
			EnvVars: []string{"TUNNEL_RETRIES"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "ha-connections",
			Value:  4,
			Hidden: true,
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
			Name:    "use-reconnect-token",
			Usage:   "Test reestablishing connections with the new 'reconnect token' flow.",
			Value:   true,
			EnvVars: []string{"TUNNEL_USE_RECONNECT_TOKEN"},
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "dial-edge-timeout",
			Usage:   "Maximum wait time to set up a connection with the edge",
			Value:   time.Second * 15,
			EnvVars: []string{"DIAL_EDGE_TIMEOUT"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "stdin-control",
			Usage:   "Control the process using commands sent through stdin",
			EnvVars: []string{"STDIN-CONTROL"},
			Hidden:  true,
			Value:   false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			EnvVars: []string{"TUNNEL_NAME"},
			Usage:   "Stable name to identify the tunnel. Using this flag will create, route and run a tunnel. For production usage, execute each command separately",
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   uiFlag,
			Usage:  "Launch tunnel UI. Tunnel logs are scrollable via 'j', 'k', or arrow keys.",
			Value:  false,
			Hidden: shouldHide,
		}),
		selectProtocolFlag,
	}...)

	return flags
}

// Flags in tunnel command that is relevant to run subcommand
func configureCloudflaredFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:   "config",
			Usage:  "Specifies a config file in YAML format.",
			Value:  config.FindDefaultConfigPath(),
			Hidden: shouldHide,
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "origincert",
			Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
			EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
			Value:   findDefaultOriginCertPath(),
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "autoupdate-freq",
			Usage:  fmt.Sprintf("Autoupdate frequency. Default is %v.", updater.DefaultCheckUpdateFreq),
			Value:  updater.DefaultCheckUpdateFreq,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "no-autoupdate",
			Usage:   "Disable periodic check for updates, restarting the server with the new version.",
			EnvVars: []string{"NO_AUTOUPDATE"},
			Value:   false,
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "metrics",
			Value:   "localhost:",
			Usage:   "Listen address for metrics reporting.",
			EnvVars: []string{"TUNNEL_METRICS"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "pidfile",
			Usage:   "Write the application's PID to this file after first successful connection.",
			EnvVars: []string{"TUNNEL_PIDFILE"},
			Hidden:  shouldHide,
		}),
	}
}

func configureProxyFlags(shouldHide bool) []cli.Flag {
	flags := []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "url",
			Value:   "http://localhost:8080",
			Usage:   "Connect to the local webserver at `URL`.",
			EnvVars: []string{"TUNNEL_URL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "hello-world",
			Value:   false,
			Usage:   "Run Hello World Server",
			EnvVars: []string{"TUNNEL_HELLO_WORLD"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.Socks5Flag,
			Usage:   "specify if this tunnel is running as a SOCK5 Server",
			EnvVars: []string{"TUNNEL_SOCKS"},
			Value:   false,
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyConnectTimeoutFlag,
			Usage:  "HTTP proxy timeout for establishing a new connection",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTLSTimeoutFlag,
			Usage:  "HTTP proxy timeout for completing a TLS handshake",
			Value:  time.Second * 10,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTCPKeepAlive,
			Usage:  "HTTP proxy TCP keepalive duration",
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   ingress.ProxyNoHappyEyeballsFlag,
			Usage:  "HTTP proxy should disable \"happy eyeballs\" for IPv4/v6 fallback",
			Hidden: shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   ingress.ProxyKeepAliveConnectionsFlag,
			Usage:  "HTTP proxy maximum keepalive connection pool size",
			Value:  100,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyKeepAliveTimeoutFlag,
			Usage:  "HTTP proxy timeout for closing an idle connection",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-connection-timeout",
			Usage:  "DEPRECATED. No longer has any effect.",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   "proxy-expect-continue-timeout",
			Usage:  "DEPRECATED. No longer has any effect.",
			Value:  time.Second * 90,
			Hidden: shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.HTTPHostHeaderFlag,
			Usage:   "Sets the HTTP Host header for the local webserver.",
			EnvVars: []string{"TUNNEL_HTTP_HOST_HEADER"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.OriginServerNameFlag,
			Usage:   "Hostname on the origin server certificate.",
			EnvVars: []string{"TUNNEL_ORIGIN_SERVER_NAME"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "unix-socket",
			Usage:   "Path to unix socket to use instead of --url",
			EnvVars: []string{"TUNNEL_UNIX_SOCKET"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    tlsconfig.OriginCAPoolFlag,
			Usage:   "Path to the CA for the certificate of your origin. This option should be used only if your certificate is not signed by Cloudflare.",
			EnvVars: []string{"TUNNEL_ORIGIN_CA_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoTLSVerifyFlag,
			Usage:   "Disables TLS verification of the certificate presented by your origin. Will allow any certificate from the origin to be accepted. Note: The connection from your machine to Cloudflare's Edge is still encrypted.",
			EnvVars: []string{"NO_TLS_VERIFY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoChunkedEncodingFlag,
			Usage:   "Disables chunked transfer encoding; useful if you are running a WSGI server.",
			EnvVars: []string{"TUNNEL_NO_CHUNKED_ENCODING"},
			Hidden:  shouldHide,
		}),
	}
	return append(flags, sshFlags(shouldHide)...)
}

func sshFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    sshPortFlag,
			Usage:   "Localhost port that cloudflared SSH server will run on",
			Value:   "2222",
			EnvVars: []string{"LOCAL_SSH_PORT"},
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    sshIdleTimeoutFlag,
			Usage:   "Connection timeout after no activity",
			EnvVars: []string{"SSH_IDLE_TIMEOUT"},
			Hidden:  true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    sshMaxTimeoutFlag,
			Usage:   "Absolute connection timeout",
			EnvVars: []string{"SSH_MAX_TIMEOUT"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    bucketNameFlag,
			Usage:   "Bucket name of where to upload SSH logs",
			EnvVars: []string{"BUCKET_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    regionNameFlag,
			Usage:   "Region name of where to upload SSH logs",
			EnvVars: []string{"REGION_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    secretIDFlag,
			Usage:   "Secret ID of where to upload SSH logs",
			EnvVars: []string{"SECRET_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    accessKeyIDFlag,
			Usage:   "Access Key ID of where to upload SSH logs",
			EnvVars: []string{"ACCESS_CLIENT_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    sessionTokenIDFlag,
			Usage:   "Session Token to use in the configuration of SSH logs uploading",
			EnvVars: []string{"SESSION_TOKEN_ID"},
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    s3URLFlag,
			Usage:   "S3 url of where to upload SSH logs",
			EnvVars: []string{"S3_URL"},
			Hidden:  true,
		}),
		altsrc.NewPathFlag(&cli.PathFlag{
			Name:    hostKeyPath,
			Usage:   "Absolute path of directory to save SSH host keys in",
			EnvVars: []string{"HOST_KEY_PATH"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.SSHServerFlag,
			Value:   false,
			Usage:   "Run an SSH Server",
			EnvVars: []string{"TUNNEL_SSH_SERVER"},
			Hidden:  true, // TODO: remove when feature is complete
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    config.BastionFlag,
			Value:   false,
			Usage:   "Runs as jump host",
			EnvVars: []string{"TUNNEL_BASTION"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.ProxyAddressFlag,
			Usage:   "Listen address for the proxy.",
			Value:   "127.0.0.1",
			EnvVars: []string{"TUNNEL_PROXY_ADDRESS"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    ingress.ProxyPortFlag,
			Usage:   "Listen port for the proxy.",
			Value:   0,
			EnvVars: []string{"TUNNEL_PROXY_PORT"},
			Hidden:  shouldHide,
		}),
	}
}

func configureLoggingFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogLevelFlag,
			Value:   "info",
			Usage:   "Application logging level {fatal, error, info, debug}. " + debugLevelWarning,
			EnvVars: []string{"TUNNEL_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogTransportLevelFlag,
			Aliases: []string{"proto-loglevel"}, // This flag used to be called proto-loglevel
			Value:   "info",
			Usage:   "Transport logging level(previously called protocol logging level) {fatal, error, info, debug}",
			EnvVars: []string{"TUNNEL_PROTO_LOGLEVEL", "TUNNEL_TRANSPORT_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogFileFlag,
			Usage:   "Save application log to this file for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGFILE"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    logger.LogDirectoryFlag,
			Usage:   "Save application log to this directory for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGDIRECTORY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "trace-output",
			Usage:   "Name of trace output file, generated when cloudflared stops.",
			EnvVars: []string{"TUNNEL_TRACE_OUTPUT"},
			Hidden:  shouldHide,
		}),
	}
}

func configureProxyDNSFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
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
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:    "proxy-dns-bootstrap",
			Usage:   "bootstrap endpoint URL, you can specify multiple endpoints for redundancy.",
			Value:   cli.NewStringSlice("https://162.159.36.1/dns-query", "https://162.159.46.1/dns-query", "https://[2606:4700:4700::1111]/dns-query", "https://[2606:4700:4700::1001]/dns-query"),
			EnvVars: []string{"TUNNEL_DNS_BOOTSTRAP"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "proxy-dns-odoh",
			Usage:   "Runs an Oblivious DNS over HTTPS client.",
			EnvVars: []string{"TUNNEL_DNS_ODOH"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "proxy-dns-odoh-target",
			Usage:   "ODoH target URL",
			Value:   "https://1.1.1.1/dns-query",
			EnvVars: []string{"TUNNEL_DNS_ODOH_TARGET"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "proxy-dns-odoh-proxy",
			Usage:   "ODoH proxy URL",
			Value:   "https://odoh1.surfdomeinen.nl/proxy",
			EnvVars: []string{"TUNNEL_DNS_ODOH_PROXY"},
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "proxy-dns-odoh-useproxy",
			Usage:   "Set flag to enable proxy usage",
			Value:   false,
			EnvVars: []string{"TUNNEL_DNS_ODOH_USE_PROXY"},
		}),
	}
}

func stdinControl(reconnectCh chan origin.ReconnectSignal, logger logger.Service) {
	for {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			command := scanner.Text()
			parts := strings.SplitN(command, " ", 2)

			switch parts[0] {
			case "":
				break
			case "reconnect":
				var reconnect origin.ReconnectSignal
				if len(parts) > 1 {
					var err error
					if reconnect.Delay, err = time.ParseDuration(parts[1]); err != nil {
						logger.Error(err.Error())
						continue
					}
				}
				logger.Infof("Sending reconnect signal %+v", reconnect)
				reconnectCh <- reconnect
			default:
				logger.Infof("Unknown command: %s", command)
				fallthrough
			case "help":
				logger.Info(`Supported command:
reconnect [delay]
- restarts one randomly chosen connection with optional delay before reconnect`)
			}
		}
	}
}
