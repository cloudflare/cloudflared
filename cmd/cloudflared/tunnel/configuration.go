package tunnel

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	mathRand "math/rand"
	"net"
	"net/netip"
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
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const LogFieldOriginCertPath = "originCertPath"
const secretValue = "*****"

var (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	serviceUrl      = developerPortal + "/reference/service/"
	argumentsUrl    = developerPortal + "/reference/arguments/"

	secretFlags     = [2]*altsrc.StringFlag{credentialsContentsFlag, tunnelTokenFlag}
	defaultFeatures = []string{supervisor.FeatureAllowRemoteConfig, supervisor.FeatureSerializedHeaders, supervisor.FeatureDatagramV2, supervisor.FeatureQUICSupportEOF}

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
	return c.IsSet("proxy-dns") &&
		!(c.IsSet("name") || // adhoc-named tunnel
			c.IsSet("hello-world") || // quick or named tunnel
			namedTunnel != nil) // named tunnel
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
	clientID, err := uuid.NewRandom()
	if err != nil {
		return nil, nil, errors.Wrap(err, "can't generate connector UUID")
	}
	log.Info().Msgf("Generated Connector ID: %s", clientID)
	tags, err := NewTagSliceFromCLI(c.StringSlice("tag"))
	if err != nil {
		log.Err(err).Msg("Tag parse failure")
		return nil, nil, errors.Wrap(err, "Tag parse failure")
	}
	tags = append(tags, tunnelpogs.Tag{Name: "ID", Value: clientID.String()})

	transportProtocol := c.String("protocol")
	needPQ := c.Bool("post-quantum")
	if needPQ {
		if FipsEnabled {
			return nil, nil, fmt.Errorf("post-quantum not supported in FIPS mode")
		}
		// Error if the user tries to force a non-quic transport protocol
		if transportProtocol != connection.AutoSelectFlag && transportProtocol != connection.QUIC.String() {
			return nil, nil, fmt.Errorf("post-quantum is only supported with the quic transport")
		}
		transportProtocol = connection.QUIC.String()
	}

	features := dedup(append(c.StringSlice("features"), defaultFeatures...))
	if needPQ {
		features = append(features, supervisor.FeaturePostQuantum)
	}
	namedTunnel.Client = tunnelpogs.ClientInfo{
		ClientID: clientID[:],
		Features: features,
		Version:  info.Version(),
		Arch:     info.OSArch(),
	}
	cfg := config.GetConfiguration()
	ingressRules, err := ingress.ParseIngress(cfg)
	if err != nil && err != ingress.ErrNoIngressRules {
		return nil, nil, err
	}
	if c.IsSet("url") {
		// Ingress rules cannot be provided with --url flag
		if !ingressRules.IsEmpty() {
			return nil, nil, ingress.ErrURLIncompatibleWithIngress
		} else {
			// Only for quick or adhoc tunnels will we attempt to parse:
			// --url, --hello-world, or --unix-socket flag for a tunnel ingress rule
			ingressRules, err = ingress.NewSingleOrigin(c, false)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	protocolSelector, err := connection.NewProtocolSelector(transportProtocol, namedTunnel.Credentials.AccountTag, c.IsSet(TunnelTokenFlag), c.Bool("post-quantum"), edgediscovery.ProtocolPercentage, connection.ResolveTTL, log)
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

	var pqKexIdx int
	if needPQ {
		pqKexIdx = mathRand.Intn(len(supervisor.PQKexes))
		log.Info().Msgf(
			"Using experimental hybrid post-quantum key agreement %s",
			supervisor.PQKexNames[supervisor.PQKexes[pqKexIdx]],
		)
	}

	tunnelConfig := &supervisor.TunnelConfig{
		GracePeriod:     gracePeriod,
		ReplaceExisting: c.Bool("force"),
		OSArch:          info.OSArch(),
		ClientID:        clientID.String(),
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
		Retries:            uint(c.Int("retries")),
		RunFromTerminal:    isRunningFromTerminal(),
		NamedTunnel:        namedTunnel,
		MuxerConfig:        muxerConfig,
		ProtocolSelector:   protocolSelector,
		EdgeTLSConfigs:     edgeTLSConfigs,
		NeedPQ:             needPQ,
		PQKexIdx:           pqKexIdx,
		MaxEdgeAddrRetries: uint8(c.Int("max-edge-addr-retries")),
	}
	packetConfig, err := newPacketConfig(c, log)
	if err != nil {
		log.Warn().Err(err).Msg("ICMP proxy feature is disabled")
	} else {
		tunnelConfig.PacketConfig = packetConfig
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

func newPacketConfig(c *cli.Context, logger *zerolog.Logger) (*ingress.GlobalRouterConfig, error) {
	ipv4Src, err := determineICMPv4Src(c.String("icmpv4-src"), logger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to determine IPv4 source address for ICMP proxy")
	}
	logger.Info().Msgf("ICMP proxy will use %s as source for IPv4", ipv4Src)

	ipv6Src, zone, err := determineICMPv6Src(c.String("icmpv6-src"), logger, ipv4Src)
	if err != nil {
		return nil, errors.Wrap(err, "failed to determine IPv6 source address for ICMP proxy")
	}
	if zone != "" {
		logger.Info().Msgf("ICMP proxy will use %s in zone %s as source for IPv6", ipv6Src, zone)
	} else {
		logger.Info().Msgf("ICMP proxy will use %s as source for IPv6", ipv6Src)
	}

	icmpRouter, err := ingress.NewICMPRouter(ipv4Src, ipv6Src, zone, logger)
	if err != nil {
		return nil, err
	}
	return &ingress.GlobalRouterConfig{
		ICMPRouter: icmpRouter,
		IPv4Src:    ipv4Src,
		IPv6Src:    ipv6Src,
		Zone:       zone,
	}, nil
}

func determineICMPv4Src(userDefinedSrc string, logger *zerolog.Logger) (netip.Addr, error) {
	if userDefinedSrc != "" {
		addr, err := netip.ParseAddr(userDefinedSrc)
		if err != nil {
			return netip.Addr{}, err
		}
		if addr.Is4() {
			return addr, nil
		}
		return netip.Addr{}, fmt.Errorf("expect IPv4, but %s is IPv6", userDefinedSrc)
	}

	addr, err := findLocalAddr(net.ParseIP("192.168.0.1"), 53)
	if err != nil {
		addr = netip.IPv4Unspecified()
		logger.Debug().Err(err).Msgf("Failed to determine the IPv4 for this machine. It will use %s to send/listen for ICMPv4 echo", addr)
	}
	return addr, nil
}

type interfaceIP struct {
	name string
	ip   net.IP
}

func determineICMPv6Src(userDefinedSrc string, logger *zerolog.Logger, ipv4Src netip.Addr) (addr netip.Addr, zone string, err error) {
	if userDefinedSrc != "" {
		userDefinedIP, zone, _ := strings.Cut(userDefinedSrc, "%")
		addr, err := netip.ParseAddr(userDefinedIP)
		if err != nil {
			return netip.Addr{}, "", err
		}
		if addr.Is6() {
			return addr, zone, nil
		}
		return netip.Addr{}, "", fmt.Errorf("expect IPv6, but %s is IPv4", userDefinedSrc)
	}

	// Loop through all the interfaces, the preference is
	// 1. The interface where ipv4Src is in
	// 2. Interface with IPv6 address
	// 3. Unspecified interface

	interfaces, err := net.Interfaces()
	if err != nil {
		return netip.IPv6Unspecified(), "", nil
	}

	interfacesWithIPv6 := make([]interfaceIP, 0)
	for _, interf := range interfaces {
		interfaceAddrs, err := interf.Addrs()
		if err != nil {
			continue
		}

		foundIPv4SrcInterface := false
		for _, interfaceAddr := range interfaceAddrs {
			if ipnet, ok := interfaceAddr.(*net.IPNet); ok {
				ip := ipnet.IP
				if ip.Equal(ipv4Src.AsSlice()) {
					foundIPv4SrcInterface = true
				}
				if ip.To4() == nil {
					interfacesWithIPv6 = append(interfacesWithIPv6, interfaceIP{
						name: interf.Name,
						ip:   ip,
					})
				}
			}
		}
		// Found the interface of ipv4Src. Loop through the addresses to see if there is an IPv6
		if foundIPv4SrcInterface {
			for _, interfaceAddr := range interfaceAddrs {
				if ipnet, ok := interfaceAddr.(*net.IPNet); ok {
					ip := ipnet.IP
					if ip.To4() == nil {
						addr, err := netip.ParseAddr(ip.String())
						if err == nil {
							return addr, interf.Name, nil
						}
					}
				}
			}
		}
	}

	for _, interf := range interfacesWithIPv6 {
		addr, err := netip.ParseAddr(interf.ip.String())
		if err == nil {
			return addr, interf.name, nil
		}
	}
	logger.Debug().Err(err).Msgf("Failed to determine the IPv6 for this machine. It will use %s to send/listen for ICMPv6 echo", netip.IPv6Unspecified())

	return netip.IPv6Unspecified(), "", nil
}

// FindLocalAddr tries to dial UDP and returns the local address picked by the OS
func findLocalAddr(dst net.IP, port int) (netip.Addr, error) {
	udpConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   dst,
		Port: port,
	})
	if err != nil {
		return netip.Addr{}, err
	}
	defer udpConn.Close()
	localAddrPort, err := netip.ParseAddrPort(udpConn.LocalAddr().String())
	if err != nil {
		return netip.Addr{}, err
	}
	localAddr := localAddrPort.Addr()
	return localAddr, nil
}
