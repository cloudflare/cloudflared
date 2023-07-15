package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/daemon"
	"github.com/facebookgo/grace/gracenet"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/proxydns"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/credentials"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunneldns"
	"github.com/cloudflare/cloudflared/validation"
)

const (
	sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b:3e8827f6f9f740738eb11138f7bebb68@sentry.io/189878"

	// ha-Connections specifies how many connections to make to the edge
	haConnectionsFlag = "ha-connections"

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

	// udpUnregisterSessionTimeout is how long we wait before we stop trying to unregister a UDP session from the edge
	udpUnregisterSessionTimeoutFlag = "udp-unregister-session-timeout"

	// quicDisablePathMTUDiscovery sets if QUIC should not perform PTMU discovery and use a smaller (safe) packet size.
	// Packets will then be at most 1252 (IPv4) / 1232 (IPv6) bytes in size.
	// Note that this may result in packet drops for UDP proxying, since we expect being able to send at least 1280 bytes of inner packets.
	quicDisablePathMTUDiscovery = "quic-disable-pmtu-discovery"

	// uiFlag is to enable launching cloudflared in interactive UI mode
	uiFlag = "ui"

	LogFieldCommand             = "command"
	LogFieldExpandedPath        = "expandedPath"
	LogFieldPIDPathname         = "pidPathname"
	LogFieldTmpTraceFilename    = "tmpTraceFilename"
	LogFieldTraceOutputFilepath = "traceOutputFilepath"

	tunnelCmdErrorMessage = `You did not specify any valid additional argument to the cloudflared tunnel command.

If you are trying to run a Quick Tunnel then you need to explicitly pass the --url flag.
Eg. cloudflared tunnel --url localhost:8080/.

Please note that Quick Tunnels are meant to be ephemeral and should only be used for testing purposes.
For production usage, we recommend creating Named Tunnels. (https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/tunnel-guide/)
`
	connectorLabelFlag = "label"
)

var (
	graceShutdownC chan struct{}
	buildInfo      *cliutil.BuildInfo

	routeFailMsg = fmt.Sprintf("failed to provision routing, please create it manually via Cloudflare dashboard or UI; "+
		"most likely you already have a conflicting record there. You can also rerun this command with --%s to overwrite "+
		"any existing DNS records for this hostname.", overwriteDNSFlag)
	deprecatedClassicTunnelErr = fmt.Errorf("Classic tunnels have been deprecated, please use Named Tunnels. (https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/tunnel-guide/)")
)

func Flags() []cli.Flag {
	return tunnelFlags(true)
}

func Commands() []*cli.Command {
	subcommands := []*cli.Command{
		buildLoginSubcommand(false),
		buildCreateCommand(),
		buildRouteCommand(),
		buildVirtualNetworkSubcommand(false),
		buildRunCommand(),
		buildListCommand(),
		buildInfoCommand(),
		buildIngressSubcommand(),
		buildDeleteCommand(),
		buildCleanupCommand(),
		buildTokenCommand(),
		// for compatibility, allow following as tunnel subcommands
		proxydns.Command(true),
		cliutil.RemovedCommand("db-connect"),
	}

	return []*cli.Command{
		buildTunnelCommand(subcommands),
		// for compatibility, allow following as top-level subcommands
		buildLoginSubcommand(true),
		cliutil.RemovedCommand("db-connect"),
	}
}

func buildTunnelCommand(subcommands []*cli.Command) *cli.Command {
	return &cli.Command{
		Name:      "tunnel",
		Action:    cliutil.ConfiguredAction(TunnelCommand),
		Category:  "Tunnel",
		Usage:     "Use Cloudflare Tunnel to expose private services to the Internet or to Cloudflare connected private users.",
		ArgsUsage: " ",
		Description: `    Cloudflare Tunnel allows to expose private services without opening any ingress port on this machine. It can expose:
  A) Locally reachable HTTP-based private services to the Internet on DNS with Cloudflare as authority (which you can
then protect with Cloudflare Access).
  B) Locally reachable TCP/UDP-based private services to Cloudflare connected private users in the same account, e.g.,
those enrolled to a Zero Trust WARP Client.

You can manage your Tunnels via dash.teams.cloudflare.com. This approach will only require you to run a single command
later in each machine where you wish to run a Tunnel.

Alternatively, you can manage your Tunnels via the command line. Begin by obtaining a certificate to be able to do so:

	$ cloudflared tunnel login

With your certificate installed you can then get started with Tunnels:

	$ cloudflared tunnel create my-first-tunnel
	$ cloudflared tunnel route dns my-first-tunnel my-first-tunnel.mydomain.com
	$ cloudflared tunnel run --hello-world my-first-tunnel

You can now access my-first-tunnel.mydomain.com and be served an example page by your local cloudflared process.

For exposing local TCP/UDP services by IP to your privately connected users, check out:

	$ cloudflared tunnel route ip --help

See https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/tunnel-guide/ for more info.`,
		Subcommands: subcommands,
		Flags:       tunnelFlags(false),
	}
}

func TunnelCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	// Run a adhoc named tunnel
	// Allows for the creation, routing (optional), and startup of a tunnel in one command
	// --name required
	// --url or --hello-world required
	// --hostname optional
	if name := c.String("name"); name != "" {
		hostname, err := validation.ValidateHostname(c.String("hostname"))
		if err != nil {
			return errors.Wrap(err, "Invalid hostname provided")
		}
		url := c.String("url")
		if url == hostname && url != "" && hostname != "" {
			return fmt.Errorf("hostname and url shouldn't match. See --help for more information")
		}

		return runAdhocNamedTunnel(sc, name, c.String(CredFileFlag))
	}

	// Run a quick tunnel
	// A unauthenticated named tunnel hosted on <random>.<quick-tunnels-service>.com
	// We don't support running proxy-dns and a quick tunnel at the same time as the same process
	shouldRunQuickTunnel := c.IsSet("url") || c.IsSet(ingress.HelloWorldFlag)
	if !c.IsSet("proxy-dns") && c.String("quick-service") != "" && shouldRunQuickTunnel {
		return RunQuickTunnel(sc)
	}

	// If user provides a config, check to see if they meant to use `tunnel run` instead
	if ref := config.GetConfiguration().TunnelID; ref != "" {
		return fmt.Errorf("Use `cloudflared tunnel run` to start tunnel %s", ref)
	}

	// Classic tunnel usage is no longer supported
	if c.String("hostname") != "" {
		return deprecatedClassicTunnelErr
	}

	if c.IsSet("proxy-dns") {
		if shouldRunQuickTunnel {
			return fmt.Errorf("running a quick tunnel with `proxy-dns` is not supported")
		}
		// NamedTunnelProperties are nil since proxy dns server does not need it.
		// This is supported for legacy reasons: dns proxy server is not a tunnel and ideally should
		// not run as part of cloudflared tunnel.
		return StartServer(sc.c, buildInfo, nil, sc.log)
	}

	return errors.New(tunnelCmdErrorMessage)
}

func Init(info *cliutil.BuildInfo, gracefulShutdown chan struct{}) {
	buildInfo, graceShutdownC = info, gracefulShutdown
}

// runAdhocNamedTunnel create, route and run a named tunnel in one command
func runAdhocNamedTunnel(sc *subcommandContext, name, credentialsOutputPath string) error {
	tunnel, ok, err := sc.tunnelActive(name)
	if err != nil || !ok {
		// pass empty string as secret to generate one
		tunnel, err = sc.create(name, credentialsOutputPath, "")
		if err != nil {
			return errors.Wrap(err, "failed to create tunnel")
		}
	} else {
		sc.log.Info().Str(LogFieldTunnelID, tunnel.ID.String()).Msg("Reusing existing tunnel with this name")
	}

	if r, ok := routeFromFlag(sc.c); ok {
		if res, err := sc.route(tunnel.ID, r); err != nil {
			sc.log.Err(err).Str("route", r.String()).Msg(routeFailMsg)
		} else {
			sc.log.Info().Msg(res.SuccessSummary())
		}
	}

	if err := sc.run(tunnel.ID); err != nil {
		return errors.Wrap(err, "error running tunnel")
	}

	return nil
}

func routeFromFlag(c *cli.Context) (route cfapi.HostnameRoute, ok bool) {
	if hostname := c.String("hostname"); hostname != "" {
		if lbPool := c.String("lb-pool"); lbPool != "" {
			return cfapi.NewLBRoute(hostname, lbPool), true
		}
		return cfapi.NewDNSRoute(hostname, c.Bool(overwriteDNSFlagName)), true
	}
	return nil, false
}

func StartServer(
	c *cli.Context,
	info *cliutil.BuildInfo,
	namedTunnel *connection.NamedTunnelProperties,
	log *zerolog.Logger,
) error {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:     sentryDSN,
		Release: c.App.Version,
	})
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	listeners := gracenet.Net{}
	errC := make(chan error)

	// Only log for locally configured tunnels (Token is blank).
	if config.GetConfiguration().Source() == "" && c.String(TunnelTokenFlag) == "" {
		log.Info().Msg(config.ErrNoConfigFile.Error())
	}

	if c.IsSet("trace-output") {
		tmpTraceFile, err := os.CreateTemp("", "trace")
		if err != nil {
			log.Err(err).Msg("Failed to create new temporary file to save trace output")
		}

		traceLog := log.With().Str(LogFieldTmpTraceFilename, tmpTraceFile.Name()).Logger()

		defer func() {
			if err := tmpTraceFile.Close(); err != nil {
				traceLog.Err(err).Msg("Failed to close temporary trace output file")
			}
			traceOutputFilepath := c.String("trace-output")
			if err := os.Rename(tmpTraceFile.Name(), traceOutputFilepath); err != nil {
				traceLog.
					Err(err).
					Str(LogFieldTraceOutputFilepath, traceOutputFilepath).
					Msg("Failed to rename temporary trace output file")
			} else {
				err := os.Remove(tmpTraceFile.Name())
				if err != nil {
					traceLog.Err(err).Msg("Failed to remove the temporary trace file")
				}
			}
		}()

		if err := trace.Start(tmpTraceFile); err != nil {
			traceLog.Err(err).Msg("Failed to start trace")
			return errors.Wrap(err, "Error starting tracing")
		}
		defer trace.Stop()
	}

	info.Log(log)
	logClientOptions(c, log)

	// this context drives the server, when it's cancelled tunnel and all other components (origins, dns, etc...) should stop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go waitForSignal(graceShutdownC, log)

	if c.IsSet("proxy-dns") {
		dnsReadySignal := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			errC <- runDNSProxyServer(c, dnsReadySignal, ctx.Done(), log)
		}()
		// Wait for proxy-dns to come up (if used)
		<-dnsReadySignal
	}

	connectedSignal := signal.New(make(chan struct{}))
	go notifySystemd(connectedSignal)
	if c.IsSet("pidfile") {
		go writePidFile(connectedSignal, c.String("pidfile"), log)
	}

	// update needs to be after DNS proxy is up to resolve equinox server address
	wg.Add(1)
	go func() {
		defer wg.Done()
		autoupdater := updater.NewAutoUpdater(
			c.Bool("no-autoupdate"), c.Duration("autoupdate-freq"), &listeners, log,
		)
		errC <- autoupdater.Run(ctx)
	}()

	// Serve DNS proxy stand-alone if no tunnel type (quick, adhoc, named) is going to run
	if dnsProxyStandAlone(c, namedTunnel) {
		connectedSignal.Notify()
		// no grace period, handle SIGINT/SIGTERM immediately
		return waitToShutdown(&wg, cancel, errC, graceShutdownC, 0, log)
	}

	logTransport := logger.CreateTransportLoggerFromContext(c, logger.EnableTerminalLog)

	observer := connection.NewObserver(log, logTransport)

	// Send Quick Tunnel URL to UI if applicable
	var quickTunnelURL string
	if namedTunnel != nil {
		quickTunnelURL = namedTunnel.QuickTunnelUrl
	}
	if quickTunnelURL != "" {
		observer.SendURL(quickTunnelURL)
	}

	tunnelConfig, orchestratorConfig, err := prepareTunnelConfig(c, info, log, logTransport, observer, namedTunnel)
	if err != nil {
		log.Err(err).Msg("Couldn't start tunnel")
		return err
	}
	var clientID uuid.UUID
	if tunnelConfig.NamedTunnel != nil {
		clientID, err = uuid.FromBytes(tunnelConfig.NamedTunnel.Client.ClientID)
		if err != nil {
			// set to nil for classic tunnels
			clientID = uuid.Nil
		}
	}

	internalRules := []ingress.Rule{}
	if features.Contains(features.FeatureManagementLogs) {
		serviceIP := c.String("service-op-ip")
		if edgeAddrs, err := edgediscovery.ResolveEdge(log, tunnelConfig.Region, tunnelConfig.EdgeIPVersion); err == nil {
			if serviceAddr, err := edgeAddrs.GetAddrForRPC(); err == nil {
				serviceIP = serviceAddr.TCP.String()
			}
		}

		mgmt := management.New(
			c.String("management-hostname"),
			c.Bool("management-diagnostics"),
			serviceIP,
			clientID,
			c.String(connectorLabelFlag),
			logger.ManagementLogger.Log,
			logger.ManagementLogger,
		)
		internalRules = []ingress.Rule{ingress.NewManagementRule(mgmt)}
	}
	orchestrator, err := orchestration.NewOrchestrator(ctx, orchestratorConfig, tunnelConfig.Tags, internalRules, tunnelConfig.Log)
	if err != nil {
		return err
	}

	metricsListener, err := listeners.Listen("tcp", c.String("metrics"))
	if err != nil {
		log.Err(err).Msg("Error opening metrics server listener")
		return errors.Wrap(err, "Error opening metrics server listener")
	}
	defer metricsListener.Close()
	wg.Add(1)
	go func() {
		defer wg.Done()
		readinessServer := metrics.NewReadyServer(log, clientID)
		observer.RegisterSink(readinessServer)
		metricsConfig := metrics.Config{
			ReadyServer:         readinessServer,
			QuickTunnelHostname: quickTunnelURL,
			Orchestrator:        orchestrator,
		}
		errC <- metrics.ServeMetrics(metricsListener, ctx, metricsConfig, log)
	}()

	reconnectCh := make(chan supervisor.ReconnectSignal, c.Int(haConnectionsFlag))
	if c.IsSet("stdin-control") {
		log.Info().Msg("Enabling control through stdin")
		go stdinControl(reconnectCh, log)
	}

	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
			log.Info().Msg("Tunnel server stopped")
		}()
		errC <- supervisor.StartTunnelDaemon(ctx, tunnelConfig, orchestrator, connectedSignal, reconnectCh, graceShutdownC)
	}()

	gracePeriod, err := gracePeriod(c)
	if err != nil {
		return err
	}
	return waitToShutdown(&wg, cancel, errC, graceShutdownC, gracePeriod, log)
}

func waitToShutdown(wg *sync.WaitGroup,
	cancelServerContext func(),
	errC <-chan error,
	graceShutdownC <-chan struct{},
	gracePeriod time.Duration,
	log *zerolog.Logger,
) error {
	var err error
	select {
	case err = <-errC:
		log.Error().Err(err).Msg("Initiating shutdown")
	case <-graceShutdownC:
		log.Debug().Msg("Graceful shutdown signalled")
		if gracePeriod > 0 {
			// wait for either grace period or service termination
			select {
			case <-time.Tick(gracePeriod):
			case <-errC:
			}
		}
	}

	// stop server context
	cancelServerContext()

	// Wait for clean exit, discarding all errors while we wait
	stopDiscarding := make(chan struct{})
	go func() {
		for {
			select {
			case <-errC: // ignore
			case <-stopDiscarding:
				return
			}
		}
	}()
	wg.Wait()
	close(stopDiscarding)

	return err
}

func notifySystemd(waitForSignal *signal.Signal) {
	<-waitForSignal.Wait()
	daemon.SdNotify(false, "READY=1")
}

func writePidFile(waitForSignal *signal.Signal, pidPathname string, log *zerolog.Logger) {
	<-waitForSignal.Wait()
	expandedPath, err := homedir.Expand(pidPathname)
	if err != nil {
		log.Err(err).Str(LogFieldPIDPathname, pidPathname).Msg("Unable to expand the path, try to use absolute path in --pidfile")
		return
	}
	file, err := os.Create(expandedPath)
	if err != nil {
		log.Err(err).Str(LogFieldExpandedPath, expandedPath).Msg("Unable to write pid")
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

func tunnelFlags(shouldHide bool) []cli.Flag {
	flags := configureCloudflaredFlags(shouldHide)
	flags = append(flags, configureProxyFlags(shouldHide)...)
	flags = append(flags, cliutil.ConfigureLoggingFlags(shouldHide)...)
	flags = append(flags, configureProxyDNSFlags(shouldHide)...)
	flags = append(flags, []cli.Flag{
		credentialsFileFlag,
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   "is-autoupdated",
			Usage:  "Signal the new process that Cloudflare Tunnel connector has been autoupdated",
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
			Name:    "region",
			Usage:   "Cloudflare Edge region to connect to. Omit or set to empty to connect to the global region.",
			EnvVars: []string{"TUNNEL_REGION"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "edge-ip-version",
			Usage:   "Cloudflare Edge IP address version to connect with. {4, 6, auto}",
			EnvVars: []string{"TUNNEL_EDGE_IP_VERSION"},
			Value:   "4",
			Hidden:  false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "edge-bind-address",
			Usage:   "Bind to IP address for outgoing connections to Cloudflare Edge.",
			EnvVars: []string{"TUNNEL_EDGE_BIND_ADDRESS"},
			Hidden:  false,
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
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "heartbeat-count",
			Usage:  "Minimum number of unacked heartbeats to send before closing the connection.",
			Value:  5,
			Hidden: true,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   "max-edge-addr-retries",
			Usage:  "Maximum number of times to retry on edge addrs before falling back to a lower protocol",
			Value:  8,
			Hidden: true,
		}),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "retries",
			Value:   5,
			Usage:   "Maximum number of retries for connection/protocol errors.",
			EnvVars: []string{"TUNNEL_RETRIES"},
			Hidden:  shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   haConnectionsFlag,
			Value:  4,
			Hidden: true,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   udpUnregisterSessionTimeoutFlag,
			Value:  5 * time.Second,
			Hidden: true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    quicDisablePathMTUDiscovery,
			EnvVars: []string{"TUNNEL_DISABLE_QUIC_PMTU"},
			Usage:   "Use this option to disable PTMU discovery for QUIC connections. This will result in lower packet sizes. Not however, that this may cause instability for UDP proxying.",
			Value:   false,
			Hidden:  true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:  connectorLabelFlag,
			Usage: "Use this option to give a meaningful label to a specific connector. When a tunnel starts up, a connector id unique to the tunnel is generated. This is a uuid. To make it easier to identify a connector, we will use the hostname of the machine the tunnel is running on along with the connector ID. This option exists if one wants to have more control over what their individual connectors are called.",
			Value: "",
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:    "grace-period",
			Usage:   "When cloudflared receives SIGINT/SIGTERM it will stop accepting new requests, wait for in-progress requests to terminate, then shutdown. Waiting for in-progress requests will timeout after this grace period, or when a second SIGTERM/SIGINT is received.",
			Value:   time.Second * 30,
			EnvVars: []string{"TUNNEL_GRACE_PERIOD"},
			Hidden:  shouldHide,
		}),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
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
			EnvVars: []string{"STDIN_CONTROL"},
			Hidden:  true,
			Value:   false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			EnvVars: []string{"TUNNEL_NAME"},
			Usage:   "Stable name to identify the tunnel. Using this flag will create, route and run a tunnel. For production usage, execute each command separately",
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   uiFlag,
			Usage:  "(depreciated) Launch tunnel UI. Tunnel logs are scrollable via 'j', 'k', or arrow keys.",
			Value:  false,
			Hidden: true,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:   "quick-service",
			Usage:  "URL for a service which manages unauthenticated 'quick' tunnels.",
			Value:  "https://api.trycloudflare.com",
			Hidden: true,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "max-fetch-size",
			Usage:   `The maximum number of results that cloudflared can fetch from Cloudflare API for any listing operations needed`,
			EnvVars: []string{"TUNNEL_MAX_FETCH_SIZE"},
			Hidden:  true,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "post-quantum",
			Usage:   "When given creates an experimental post-quantum secure tunnel",
			Aliases: []string{"pq"},
			EnvVars: []string{"TUNNEL_POST_QUANTUM"},
			Hidden:  FipsEnabled,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    "management-diagnostics",
			Usage:   "Enables the in-depth diagnostic routes to be made available over the management service (/debug/pprof, /metrics, etc.)",
			EnvVars: []string{"TUNNEL_MANAGEMENT_DIAGNOSTICS"},
			Value:   false,
		}),
		selectProtocolFlag,
		overwriteDNSFlag,
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
			Name:    credentials.OriginCertFlag,
			Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
			EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
			Value:   credentials.FindDefaultOriginCertPath(),
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
			Name:    ingress.HelloWorldFlag,
			Value:   false,
			Usage:   "Run Hello World Server",
			EnvVars: []string{"TUNNEL_HELLO_WORLD"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.Socks5Flag,
			Usage:   legacyTunnelFlag("specify if this tunnel is running as a SOCK5 Server"),
			EnvVars: []string{"TUNNEL_SOCKS"},
			Value:   false,
			Hidden:  shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyConnectTimeoutFlag,
			Usage:  legacyTunnelFlag("HTTP proxy timeout for establishing a new connection"),
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTLSTimeoutFlag,
			Usage:  legacyTunnelFlag("HTTP proxy timeout for completing a TLS handshake"),
			Value:  time.Second * 10,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyTCPKeepAliveFlag,
			Usage:  legacyTunnelFlag("HTTP proxy TCP keepalive duration"),
			Value:  time.Second * 30,
			Hidden: shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:   ingress.ProxyNoHappyEyeballsFlag,
			Usage:  legacyTunnelFlag("HTTP proxy should disable \"happy eyeballs\" for IPv4/v6 fallback"),
			Hidden: shouldHide,
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:   ingress.ProxyKeepAliveConnectionsFlag,
			Usage:  legacyTunnelFlag("HTTP proxy maximum keepalive connection pool size"),
			Value:  100,
			Hidden: shouldHide,
		}),
		altsrc.NewDurationFlag(&cli.DurationFlag{
			Name:   ingress.ProxyKeepAliveTimeoutFlag,
			Usage:  legacyTunnelFlag("HTTP proxy timeout for closing an idle connection"),
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
			Usage:   legacyTunnelFlag("Sets the HTTP Host header for the local webserver."),
			EnvVars: []string{"TUNNEL_HTTP_HOST_HEADER"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    ingress.OriginServerNameFlag,
			Usage:   legacyTunnelFlag("Hostname on the origin server certificate."),
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
			Usage:   legacyTunnelFlag("Path to the CA for the certificate of your origin. This option should be used only if your certificate is not signed by Cloudflare."),
			EnvVars: []string{"TUNNEL_ORIGIN_CA_POOL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoTLSVerifyFlag,
			Usage:   legacyTunnelFlag("Disables TLS verification of the certificate presented by your origin. Will allow any certificate from the origin to be accepted. Note: The connection from your machine to Cloudflare's Edge is still encrypted."),
			EnvVars: []string{"NO_TLS_VERIFY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.NoChunkedEncodingFlag,
			Usage:   legacyTunnelFlag("Disables chunked transfer encoding; useful if you are running a WSGI server."),
			EnvVars: []string{"TUNNEL_NO_CHUNKED_ENCODING"},
			Hidden:  shouldHide,
		}),
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name:    ingress.Http2OriginFlag,
			Usage:   "Enables HTTP/2 origin servers.",
			EnvVars: []string{"TUNNEL_ORIGIN_ENABLE_HTTP2"},
			Hidden:  shouldHide,
			Value:   false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "management-hostname",
			Usage:   "Management hostname to signify incoming management requests",
			EnvVars: []string{"TUNNEL_MANAGEMENT_HOSTNAME"},
			Hidden:  true,
			Value:   "management.argotunnel.com",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "service-op-ip",
			Usage:   "Fallback IP for service operations run by the management service.",
			EnvVars: []string{"TUNNEL_SERVICE_OP_IP"},
			Hidden:  true,
			Value:   "198.41.200.113:80",
		}),
	}
	return append(flags, sshFlags(shouldHide)...)
}

func legacyTunnelFlag(msg string) string {
	return fmt.Sprintf(
		"%s This flag only takes effect if you define your origin with `--url` and if you do not use ingress rules."+
			" The recommended way is to rely on ingress rules and define this property under `originRequest` as per"+
			" https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/configuration/configuration-file/ingress",
		msg,
	)
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
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    ingress.ProxyPortFlag,
			Usage:   "Listen port for the proxy.",
			Value:   0,
			EnvVars: []string{"TUNNEL_PROXY_PORT"},
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
		altsrc.NewIntFlag(&cli.IntFlag{
			Name:    "proxy-dns-max-upstream-conns",
			Usage:   "Maximum concurrent connections to upstream. Setting to 0 means unlimited.",
			Value:   tunneldns.MaxUpstreamConnsDefault,
			Hidden:  shouldHide,
			EnvVars: []string{"TUNNEL_DNS_MAX_UPSTREAM_CONNS"},
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name:  "proxy-dns-bootstrap",
			Usage: "bootstrap endpoint URL, you can specify multiple endpoints for redundancy.",
			Value: cli.NewStringSlice(
				"https://162.159.36.1/dns-query",
				"https://162.159.46.1/dns-query",
				"https://[2606:4700:4700::1111]/dns-query",
				"https://[2606:4700:4700::1001]/dns-query",
			),
			EnvVars: []string{"TUNNEL_DNS_BOOTSTRAP"},
			Hidden:  shouldHide,
		}),
	}
}

func stdinControl(reconnectCh chan supervisor.ReconnectSignal, log *zerolog.Logger) {
	for {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			command := scanner.Text()
			parts := strings.SplitN(command, " ", 2)

			switch parts[0] {
			case "":
				break
			case "reconnect":
				var reconnect supervisor.ReconnectSignal
				if len(parts) > 1 {
					var err error
					if reconnect.Delay, err = time.ParseDuration(parts[1]); err != nil {
						log.Error().Msg(err.Error())
						continue
					}
				}
				log.Info().Msgf("Sending %+v", reconnect)
				reconnectCh <- reconnect
			default:
				log.Info().Str(LogFieldCommand, command).Msg("Unknown command")
				fallthrough
			case "help":
				log.Info().Msg(`Supported command:
reconnect [delay]
- restarts one randomly chosen connection with optional delay before reconnect`)
			}
		}
	}
}
