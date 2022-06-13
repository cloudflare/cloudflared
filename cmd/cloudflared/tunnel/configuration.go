package tunnel

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/validation"
)

const LogFieldOriginCertPath = "originCertPath"
const secretValue = "*****"

var (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"

	LogFieldHostname = "hostname"

	secretFlags     = [2]*altsrc.StringFlag{credentialsContentsFlag, tunnelTokenFlag}
	defaultFeatures = []string{supervisor.FeatureAllowRemoteConfig, supervisor.FeatureSerializedHeaders}

	configFlags = []string{"autoupdate-freq", "no-autoupdate", "retries", "protocol", "loglevel", "transport-loglevel", "origincert", "metrics", "metrics-update-freq", "edge-ip-version"}
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
	for _, flag := range c.FlagNames() {
		if isSecretFlag(flag) {
			flags[flag] = secretValue
		} else {
			flags[flag] = c.Generic(flag)
		}
	}

	if len(flags) > 0 {
		log.Info().Msgf("Settings: %v", flags)
	}

	envs := make(map[string]string)
	// Find env variables for Argo Tunnel
	for _, env := range os.Environ() {
		// All Argo Tunnel env variables start with TUNNEL_
		if strings.Contains(env, "TUNNEL_") {
			vars := strings.Split(env, "=")
			if len(vars) == 2 {
				if isSecretEnvVar(vars[0]) {
					envs[vars[0]] = secretValue
				} else {
					envs[vars[0]] = vars[1]
				}
			}
		}
	}
	if len(envs) > 0 {
		log.Info().Msgf("Environmental variables %v", envs)
	}
}

func isSecretFlag(key string) bool {
	for _, flag := range secretFlags {
		if flag.Name == key {
			return true
		}
	}
	return false
}

func isSecretEnvVar(key string) bool {
	for _, flag := range secretFlags {
		for _, secretEnvVar := range flag.EnvVars {
			if secretEnvVar == key {
				return true
			}
		}
	}
	return false
}

func dnsProxyStandAlone(c *cli.Context, namedTunnel *connection.NamedTunnelProperties) bool {
	return c.IsSet("proxy-dns") && (!c.IsSet("hostname") && !c.IsSet("tag") && !c.IsSet("hello-world") && namedTunnel == nil)
}

func findOriginCert(originCertPath string, log *zerolog.Logger) (string, error) {
	if originCertPath == "" {
		log.Info().Msgf("Cannot determine default origin certificate path. No file %s in %v", config.DefaultCredentialFile, config.DefaultConfigSearchDirectories())
		if isRunningFromTerminal() {
			log.Error().Msgf("You need to specify the origin certificate path with --origincert option, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", argumentsUrl)
			return "", fmt.Errorf("client didn't specify origincert path when running from terminal")
		} else {
			log.Error().Msgf("You need to specify the origin certificate path by specifying the origincert option in the configuration file, or set TUNNEL_ORIGIN_CERT environment variable. See %s for more information.", serviceUrl)
			return "", fmt.Errorf("client didn't specify origincert path")
		}
	}
	var err error
	originCertPath, err = homedir.Expand(originCertPath)
	if err != nil {
		log.Err(err).Msgf("Cannot resolve origin certificate path")
		return "", fmt.Errorf("cannot resolve path %s", originCertPath)
	}
	// Check that the user has acquired a certificate using the login command
	ok, err := config.FileExists(originCertPath)
	if err != nil {
		log.Error().Err(err).Msgf("Cannot check if origin cert exists at path %s", originCertPath)
		return "", fmt.Errorf("cannot check if origin cert exists at path %s", originCertPath)
	}
	if !ok {
		log.Error().Msgf(`Cannot find a valid certificate for your origin at the path:

    %s

If the path above is wrong, specify the path with the -origincert option.
If you don't have a certificate signed by Cloudflare, run the command:

	%s login
`, originCertPath, os.Args[0])
		return "", fmt.Errorf("cannot find a valid certificate at the path %s", originCertPath)
	}

	return originCertPath, nil
}

func readOriginCert(originCertPath string) ([]byte, error) {
	// Easier to send the certificate as []byte via RPC than decoding it at this point
	originCert, err := ioutil.ReadFile(originCertPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s to load origin certificate", originCertPath)
	}
	return originCert, nil
}

func getOriginCert(originCertPath string, log *zerolog.Logger) ([]byte, error) {
	if originCertPath, err := findOriginCert(originCertPath, log); err != nil {
		return nil, err
	} else {
		return readOriginCert(originCertPath)
	}
}

func prepareTunnelConfig(
	c *cli.Context,
	info *cliutil.BuildInfo,
	log, logTransport *zerolog.Logger,
	observer *connection.Observer,
	namedTunnel *connection.NamedTunnelProperties,
) (*supervisor.TunnelConfig, *orchestration.Config, error) {
	isNamedTunnel := namedTunnel != nil

	configHostname := c.String("hostname")
	hostname, err := validation.ValidateHostname(configHostname)
	if err != nil {
		log.Err(err).Str(LogFieldHostname, configHostname).Msg("Invalid hostname")
		return nil, nil, errors.Wrap(err, "Invalid hostname")
	}
	clientID := c.String("id")
	if !c.IsSet("id") {
		clientID, err = generateRandomClientID(log)
		if err != nil {
			return nil, nil, err
		}
	}

	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		log.Err(err).Msg("Tag parse failure")
		return nil, nil, errors.Wrap(err, "Tag parse failure")
	}

	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID})

	var (
		ingressRules  ingress.Ingress
		classicTunnel *connection.ClassicTunnelProperties
	)

	transportProtocol := c.String("protocol")
	protocolFetcher := edgediscovery.ProtocolPercentage

	cfg := config.GetConfiguration()
	if isNamedTunnel {
		clientUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, nil, errors.Wrap(err, "can't generate connector UUID")
		}
		log.Info().Msgf("Generated Connector ID: %s", clientUUID)
		features := append(c.StringSlice("features"), defaultFeatures...)
		if c.IsSet(TunnelTokenFlag) {
			if transportProtocol == connection.AutoSelectFlag {
				protocolFetcher = func() (edgediscovery.ProtocolPercents, error) {
					// If the Tunnel is remotely managed and no protocol is set, we prefer QUIC, but still allow fall-back.
					preferQuic := []edgediscovery.ProtocolPercent{
						{
							Protocol:   connection.QUIC.String(),
							Percentage: 100,
						},
						{
							Protocol:   connection.HTTP2.String(),
							Percentage: 100,
						},
					}
					return preferQuic, nil
				}
			}
			log.Info().Msg("Will be fetching remotely managed configuration from Cloudflare API. Defaulting to protocol: quic")
		}
		namedTunnel.Client = tunnelpogs.ClientInfo{
			ClientID: clientUUID[:],
			Features: dedup(features),
			Version:  info.Version(),
			Arch:     info.OSArch(),
		}
		ingressRules, err = ingress.ParseIngress(cfg)
		if err != nil && err != ingress.ErrNoIngressRules {
			return nil, nil, err
		}
		if !ingressRules.IsEmpty() && c.IsSet("url") {
			return nil, nil, ingress.ErrURLIncompatibleWithIngress
		}
	} else {

		originCertPath := c.String("origincert")
		originCertLog := log.With().
			Str(LogFieldOriginCertPath, originCertPath).
			Logger()

		originCert, err := getOriginCert(originCertPath, &originCertLog)
		if err != nil {
			return nil, nil, errors.Wrap(err, "Error getting origin cert")
		}

		classicTunnel = &connection.ClassicTunnelProperties{
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
			return nil, nil, err
		}
	}

	warpRoutingEnabled := isWarpRoutingEnabled(cfg.WarpRouting, isNamedTunnel)
	protocolSelector, err := connection.NewProtocolSelector(transportProtocol, warpRoutingEnabled, namedTunnel, protocolFetcher, supervisor.ResolveTTL, log)
	if err != nil {
		return nil, nil, err
	}
	log.Info().Msgf("Initial protocol %s", protocolSelector.Current())

	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, p := range connection.ProtocolList {
		tlsSettings := p.TLSSettings()
		if tlsSettings == nil {
			return nil, nil, fmt.Errorf("%s has unknown TLS settings", p)
		}
		edgeTLSConfig, err := tlsconfig.CreateTunnelConfig(c, tlsSettings.ServerName)
		if err != nil {
			return nil, nil, errors.Wrap(err, "unable to create TLS config to connect with edge")
		}
		if len(tlsSettings.NextProtos) > 0 {
			edgeTLSConfig.NextProtos = tlsSettings.NextProtos
		}
		edgeTLSConfigs[p] = edgeTLSConfig
	}

	gracePeriod, err := gracePeriod(c)
	if err != nil {
		return nil, nil, err
	}
	muxerConfig := &connection.MuxerConfig{
		HeartbeatInterval: c.Duration("heartbeat-interval"),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		MaxHeartbeats: uint64(c.Int("heartbeat-count")),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		CompressionSetting: h2mux.CompressionSetting(uint64(c.Int("compression-quality"))),
		MetricsUpdateFreq:  c.Duration("metrics-update-freq"),
	}
	edgeIPVersion, err := parseConfigIPVersion(c.String("edge-ip-version"))
	if err != nil {
		return nil, nil, err
	}

	tunnelConfig := &supervisor.TunnelConfig{
		GracePeriod:     gracePeriod,
		ReplaceExisting: c.Bool("force"),
		OSArch:          info.OSArch(),
		ClientID:        clientID,
		EdgeAddrs:       c.StringSlice("edge"),
		Region:          c.String("region"),
		EdgeIPVersion:   edgeIPVersion,
		HAConnections:   c.Int("ha-connections"),
		IncidentLookup:  supervisor.NewIncidentLookup(),
		IsAutoupdated:   c.Bool("is-autoupdated"),
		LBPool:          c.String("lb-pool"),
		Tags:            tags,
		Log:             log,
		LogTransport:    logTransport,
		Observer:        observer,
		ReportedVersion: info.Version(),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		Retries:          uint(c.Int("retries")),
		RunFromTerminal:  isRunningFromTerminal(),
		NamedTunnel:      namedTunnel,
		ClassicTunnel:    classicTunnel,
		MuxerConfig:      muxerConfig,
		ProtocolSelector: protocolSelector,
		EdgeTLSConfigs:   edgeTLSConfigs,
	}
	orchestratorConfig := &orchestration.Config{
		Ingress:            &ingressRules,
		WarpRouting:        ingress.NewWarpRoutingConfig(&cfg.WarpRouting),
		ConfigurationFlags: parseConfigFlags(c),
	}
	return tunnelConfig, orchestratorConfig, nil
}

func parseConfigFlags(c *cli.Context) map[string]string {
	result := make(map[string]string)

	for _, flag := range configFlags {
		if v := c.String(flag); c.IsSet(flag) && v != "" {
			result[flag] = v
		}
	}

	return result
}

func gracePeriod(c *cli.Context) (time.Duration, error) {
	period := c.Duration("grace-period")
	if period > connection.MaxGracePeriod {
		return time.Duration(0), fmt.Errorf("grace-period must be equal or less than %v", connection.MaxGracePeriod)
	}
	return period, nil
}

func isWarpRoutingEnabled(warpConfig config.WarpRoutingConfig, isNamedTunnel bool) bool {
	return warpConfig.Enabled && isNamedTunnel
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}

// Remove any duplicates from the slice
func dedup(slice []string) []string {

	// Convert the slice into a set
	set := make(map[string]bool, 0)
	for _, str := range slice {
		set[str] = true
	}

	// Convert the set back into a slice
	keys := make([]string, len(set))
	i := 0
	for str := range set {
		keys[i] = str
		i++
	}
	return keys
}

// ParseConfigIPVersion returns the IP version from possible expected values from config
func parseConfigIPVersion(version string) (v allregions.ConfigIPVersion, err error) {
	switch version {
	case "4":
		v = allregions.IPv4Only
	case "6":
		v = allregions.IPv6Only
	case "auto":
		v = allregions.Auto
	default: // unspecified or invalid
		err = fmt.Errorf("invalid value for edge-ip-version: %s", version)
	}
	return
}
