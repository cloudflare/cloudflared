package tunnel

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/buildinfo"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	quickStartUrl   = developerPortal + "/quickstart/quickstart/"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"
)

// returns the first path that contains a cert.pem file. If none of the DefaultConfigSearchDirectories
// contains a cert.pem file, return empty string
func findDefaultOriginCertPath() string {
	for _, defaultConfigDir := range config.DefaultConfigSearchDirectories() {
		originCertPath, _ := homedir.Expand(filepath.Join(defaultConfigDir, config.DefaultCredentialFile))
		if ok, _ := config.FileExists(originCertPath); ok {
			return originCertPath
		}
	}
	return ""
}

func generateRandomClientID(log *zerolog.Logger) (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		log.Error().Msgf("couldn't create UUID for client ID %s", err)
		return "", err
	}
	return u.String(), nil
}

func logClientOptions(c *cli.Context, log *zerolog.Logger) {
	flags := make(map[string]interface{})
	for _, flag := range c.LocalFlagNames() {
		flags[flag] = c.Generic(flag)
	}

	sliceFlags := []string{"header", "tag", "proxy-dns-upstream", "upstream", "edge"}
	for _, sliceFlag := range sliceFlags {
		if len(c.StringSlice(sliceFlag)) > 0 {
			flags[sliceFlag] = strings.Join(c.StringSlice(sliceFlag), ", ")
		}
	}

	if len(flags) > 0 {
		log.Info().Msgf("Environment variables %v", flags)
	}

	envs := make(map[string]string)
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
	if len(envs) > 0 {
		log.Info().Msgf("Environmental variables %v", envs)
	}
}

func dnsProxyStandAlone(c *cli.Context) bool {
	return c.IsSet("proxy-dns") && (!c.IsSet("hostname") && !c.IsSet("tag") && !c.IsSet("hello-world"))
}

func findOriginCert(c *cli.Context, log *zerolog.Logger) (string, error) {
	originCertPath := c.String("origincert")
	if originCertPath == "" {
		log.Info().Msgf("Cannot determine default origin certificate path. No file %s in %v", config.DefaultCredentialFile, config.DefaultConfigSearchDirectories())
		if isRunningFromTerminal() {
			log.Error().Msgf("You need to specify the origin certificate path with --origincert option, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", argumentsUrl)
			return "", fmt.Errorf("Client didn't specify origincert path when running from terminal")
		} else {
			log.Error().Msgf("You need to specify the origin certificate path by specifying the origincert option in the configuration file, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", serviceUrl)
			return "", fmt.Errorf("Client didn't specify origincert path")
		}
	}
	var err error
	originCertPath, err = homedir.Expand(originCertPath)
	if err != nil {
		log.Error().Msgf("Cannot resolve path %s: %s", originCertPath, err)
		return "", fmt.Errorf("Cannot resolve path %s", originCertPath)
	}
	// Check that the user has acquired a certificate using the login command
	ok, err := config.FileExists(originCertPath)
	if err != nil {
		log.Error().Msgf("Cannot check if origin cert exists at path %s", originCertPath)
		return "", fmt.Errorf("Cannot check if origin cert exists at path %s", originCertPath)
	}
	if !ok {
		log.Error().Msgf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

	%s login
`, originCertPath, os.Args[0])
		return "", fmt.Errorf("Cannot find a valid certificate at the path %s", originCertPath)
	}

	return originCertPath, nil
}

func readOriginCert(originCertPath string, log *zerolog.Logger) ([]byte, error) {
	log.Debug().Msgf("Reading origin cert from %s", originCertPath)

	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		log.Error().Msgf("Cannot read %s to load origin certificate: %s", originCertPath, err)
		return nil, fmt.Errorf("Cannot read %s to load origin certificate", originCertPath)
	}
	return originCert, nil
}

func getOriginCert(c *cli.Context, log *zerolog.Logger) ([]byte, error) {
	if originCertPath, err := findOriginCert(c, log); err != nil {
		return nil, err
	} else {
		return readOriginCert(originCertPath, log)
	}
}

func prepareTunnelConfig(
	c *cli.Context,
	buildInfo *buildinfo.BuildInfo,
	version string,
	log *zerolog.Logger,
	transportLogger *zerolog.Logger,
	namedTunnel *connection.NamedTunnelConfig,
	isUIEnabled bool,
	eventChans []chan connection.Event,
) (*origin.TunnelConfig, ingress.Ingress, error) {
	isNamedTunnel := namedTunnel != nil

	hostname, err := validation.ValidateHostname(c.String("hostname"))
	if err != nil {
		log.Error().Msgf("Invalid hostname: %s", err)
		return nil, ingress.Ingress{}, errors.Wrap(err, "Invalid hostname")
	}
	isFreeTunnel := hostname == ""
	clientID := c.String("id")
	if !c.IsSet("id") {
		clientID, err = generateRandomClientID(log)
		if err != nil {
			return nil, ingress.Ingress{}, err
		}
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		log.Error().Msgf("Tag parse failure: %s", err)
		return nil, ingress.Ingress{}, errors.Wrap(err, "Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})

	var originCert []byte
	if !isFreeTunnel {
		originCert, err = getOriginCert(c, log)
		if err != nil {
			return nil, ingress.Ingress{}, errors.Wrap(err, "Error getting origin cert")
		}
	}

	var (
		ingressRules  ingress.Ingress
		classicTunnel *connection.ClassicTunnelConfig
	)
	if isNamedTunnel {
		clientUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, ingress.Ingress{}, errors.Wrap(err, "can't generate clientUUID")
		}
		namedTunnel.Client = tunnelpogs.ClientInfo{
			ClientID: clientUUID[:],
			Features: []string{origin.FeatureSerializedHeaders},
			Version:  version,
			Arch:     fmt.Sprintf("%s_%s", buildInfo.GoOS, buildInfo.GoArch),
		}
		ingressRules, err = ingress.ParseIngress(config.GetConfiguration())
		if err != nil && err != ingress.ErrNoIngressRules {
			return nil, ingress.Ingress{}, err
		}
		if !ingressRules.IsEmpty() && c.IsSet("url") {
			return nil, ingress.Ingress{}, ingress.ErrURLIncompatibleWithIngress
		}
	} else {
		classicTunnel = &connection.ClassicTunnelConfig{
			Hostname:   hostname,
			OriginCert: originCert,
			// turn off use of reconnect token and auth refresh when using named tunnels
			UseReconnectToken: !isNamedTunnel && c.Bool("use-reconnect-token"),
		}
	}

	// Convert single-origin configuration into multi-origin configuration.
	if ingressRules.IsEmpty() {
		ingressRules, err = ingress.NewSingleOrigin(c, !isNamedTunnel)
		if err != nil {
			return nil, ingress.Ingress{}, err
		}
	}

	protocolSelector, err := connection.NewProtocolSelector(c.String("protocol"), namedTunnel, edgediscovery.HTTP2Percentage, origin.ResolveTTL, log)
	if err != nil {
		return nil, ingress.Ingress{}, err
	}
	log.Info().Msgf("Initial protocol %s", protocolSelector.Current())

	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, p := range connection.ProtocolList {
		edgeTLSConfig, err := tlsconfig.CreateTunnelConfig(c, p.ServerName())
		if err != nil {
			return nil, ingress.Ingress{}, errors.Wrap(err, "unable to create TLS config to connect with edge")
		}
		edgeTLSConfigs[p] = edgeTLSConfig
	}

	originClient := origin.NewClient(ingressRules, tags, log)
	connectionConfig := &connection.Config{
		OriginClient:    originClient,
		GracePeriod:     c.Duration("grace-period"),
		ReplaceExisting: c.Bool("force"),
	}
	muxerConfig := &connection.MuxerConfig{
		HeartbeatInterval:  c.Duration("heartbeat-interval"),
		MaxHeartbeats:      c.Uint64("heartbeat-count"),
		CompressionSetting: h2mux.CompressionSetting(c.Uint64("compression-quality")),
		MetricsUpdateFreq:  c.Duration("metrics-update-freq"),
	}

	return &origin.TunnelConfig{
		ConnectionConfig: connectionConfig,
		BuildInfo:        buildInfo,
		ClientID:         clientID,
		EdgeAddrs:        c.StringSlice("edge"),
		HAConnections:    c.Int("ha-connections"),
		IncidentLookup:   origin.NewIncidentLookup(),
		IsAutoupdated:    c.Bool("is-autoupdated"),
		IsFreeTunnel:     isFreeTunnel,
		LBPool:           c.String("lb-pool"),
		Tags:             tags,
		Log:              log,
		Observer:         connection.NewObserver(transportLogger, eventChans, isUIEnabled),
		ReportedVersion:  version,
		Retries:          c.Uint("retries"),
		RunFromTerminal:  isRunningFromTerminal(),
		NamedTunnel:      namedTunnel,
		ClassicTunnel:    classicTunnel,
		MuxerConfig:      muxerConfig,
		TunnelEventChans: eventChans,
		ProtocolSelector: protocolSelector,
		EdgeTLSConfigs:   edgeTLSConfigs,
	}, ingressRules, nil
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
