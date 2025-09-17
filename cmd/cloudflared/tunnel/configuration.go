package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"golang.org/x/term"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/ingress/origins"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	secretValue       = "*****"
	icmpFunnelTimeout = time.Second * 10
)

var (
	secretFlags = [2]*altsrc.StringFlag{credentialsContentsFlag, tunnelTokenFlag}

	configFlags = []string{
		flags.AutoUpdateFreq,
		flags.NoAutoUpdate,
		flags.Retries,
		flags.Protocol,
		flags.LogLevel,
		flags.TransportLogLevel,
		flags.OriginCert,
		flags.Metrics,
		flags.MetricsUpdateFreq,
		flags.EdgeIpVersion,
		flags.EdgeBindAddress,
		flags.MaxActiveFlows,
	}
)

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

func dnsProxyStandAlone(c *cli.Context, namedTunnel *connection.TunnelProperties) bool {
	return c.IsSet(flags.ProxyDns) &&
		!(c.IsSet(flags.Name) || // adhoc-named tunnel
			c.IsSet(ingress.HelloWorldFlag) || // quick or named tunnel
			namedTunnel != nil) // named tunnel
}

func prepareTunnelConfig(
	ctx context.Context,
	c *cli.Context,
	info *cliutil.BuildInfo,
	log, logTransport *zerolog.Logger,
	observer *connection.Observer,
	namedTunnel *connection.TunnelProperties,
) (*supervisor.TunnelConfig, *orchestration.Config, error) {
	transportProtocol := c.String(flags.Protocol)
	isPostQuantumEnforced := c.Bool(flags.PostQuantum)
	featureSelector, err := features.NewFeatureSelector(ctx, namedTunnel.Credentials.AccountTag, c.StringSlice(flags.Features), isPostQuantumEnforced, log)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to create feature selector")
	}

	clientConfig, err := client.NewConfig(info.Version(), info.OSArch(), featureSelector)
	if err != nil {
		return nil, nil, err
	}

	log.Info().Msgf("Generated Connector ID: %s", clientConfig.ConnectorID)

	tags, err := NewTagSliceFromCLI(c.StringSlice(flags.Tag))
	if err != nil {
		log.Err(err).Msg("Tag parse failure")
		return nil, nil, errors.Wrap(err, "Tag parse failure")
	}
	tags = append(tags, pogs.Tag{Name: "ID", Value: clientConfig.ConnectorID.String()})

	clientFeatures := featureSelector.Snapshot()
	pqMode := clientFeatures.PostQuantum
	if pqMode == features.PostQuantumStrict {
		// Error if the user tries to force a non-quic transport protocol
		if transportProtocol != connection.AutoSelectFlag && transportProtocol != connection.QUIC.String() {
			return nil, nil, fmt.Errorf("post-quantum is only supported with the quic transport")
		}
		transportProtocol = connection.QUIC.String()
	}

	cfg := config.GetConfiguration()
	ingressRules, err := ingress.ParseIngressFromConfigAndCLI(cfg, c, log)
	if err != nil {
		return nil, nil, err
	}

	protocolSelector, err := connection.NewProtocolSelector(transportProtocol, namedTunnel.Credentials.AccountTag, c.IsSet(TunnelTokenFlag), isPostQuantumEnforced, edgediscovery.ProtocolPercentage, connection.ResolveTTL, log)
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
	edgeIPVersion, err := parseConfigIPVersion(c.String(flags.EdgeIpVersion))
	if err != nil {
		return nil, nil, err
	}
	edgeBindAddr, err := parseConfigBindAddress(c.String(flags.EdgeBindAddress))
	if err != nil {
		return nil, nil, err
	}
	if err := testIPBindable(edgeBindAddr); err != nil {
		return nil, nil, fmt.Errorf("invalid edge-bind-address %s: %v", edgeBindAddr, err)
	}
	edgeIPVersion, err = adjustIPVersionByBindAddress(edgeIPVersion, edgeBindAddr)
	if err != nil {
		// This is not a fatal error, we just overrode edgeIPVersion
		log.Warn().Str("edgeIPVersion", edgeIPVersion.String()).Err(err).Msg("Overriding edge-ip-version")
	}

	region := c.String(flags.Region)
	endpoint := namedTunnel.Credentials.Endpoint
	var resolvedRegion string
	// set resolvedRegion to either the region passed as argument
	// or to the endpoint in the credentials.
	// Region and endpoint are interchangeable
	if region != "" && endpoint != "" {
		return nil, nil, fmt.Errorf("region provided with a token that has an endpoint")
	} else if region != "" {
		resolvedRegion = region
	} else if endpoint != "" {
		resolvedRegion = endpoint
	}

	warpRoutingConfig := ingress.NewWarpRoutingConfig(&cfg.WarpRouting)

	// Setup origin dialer service and virtual services
	originDialerService := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   ingress.NewDialer(warpRoutingConfig),
		TCPWriteTimeout: c.Duration(flags.WriteStreamTimeout),
	}, log)

	// Setup DNS Resolver Service
	originMetrics := origins.NewMetrics(prometheus.DefaultRegisterer)
	dnsResolverAddrs := c.StringSlice(flags.VirtualDNSServiceResolverAddresses)
	dnsService := origins.NewDNSResolverService(origins.NewDNSDialer(), log, originMetrics)
	if len(dnsResolverAddrs) > 0 {
		addrs, err := parseResolverAddrPorts(dnsResolverAddrs)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid %s provided: %w", flags.VirtualDNSServiceResolverAddresses, err)
		}
		dnsService = origins.NewStaticDNSResolverService(addrs, origins.NewDNSDialer(), log, originMetrics)
	}
	originDialerService.AddReservedService(dnsService, []netip.AddrPort{origins.VirtualDNSServiceAddr})

	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:    clientConfig,
		GracePeriod:     gracePeriod,
		EdgeAddrs:       c.StringSlice(flags.Edge),
		Region:          resolvedRegion,
		EdgeIPVersion:   edgeIPVersion,
		EdgeBindAddr:    edgeBindAddr,
		HAConnections:   c.Int(flags.HaConnections),
		IsAutoupdated:   c.Bool(flags.IsAutoUpdated),
		LBPool:          c.String(flags.LBPool),
		Tags:            tags,
		Log:             log,
		LogTransport:    logTransport,
		Observer:        observer,
		ReportedVersion: info.Version(),
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		Retries:                             uint(c.Int(flags.Retries)), // nolint: gosec
		RunFromTerminal:                     isRunningFromTerminal(),
		NamedTunnel:                         namedTunnel,
		ProtocolSelector:                    protocolSelector,
		EdgeTLSConfigs:                      edgeTLSConfigs,
		MaxEdgeAddrRetries:                  uint8(c.Int(flags.MaxEdgeAddrRetries)), // nolint: gosec
		RPCTimeout:                          c.Duration(flags.RpcTimeout),
		WriteStreamTimeout:                  c.Duration(flags.WriteStreamTimeout),
		DisableQUICPathMTUDiscovery:         c.Bool(flags.QuicDisablePathMTUDiscovery),
		QUICConnectionLevelFlowControlLimit: c.Uint64(flags.QuicConnLevelFlowControlLimit),
		QUICStreamLevelFlowControlLimit:     c.Uint64(flags.QuicStreamLevelFlowControlLimit),
		OriginDNSService:                    dnsService,
		OriginDialerService:                 originDialerService,
	}
	icmpRouter, err := newICMPRouter(c, log)
	if err != nil {
		log.Warn().Err(err).Msg("ICMP proxy feature is disabled")
	} else {
		tunnelConfig.ICMPRouterServer = icmpRouter
	}
	orchestratorConfig := &orchestration.Config{
		Ingress:             &ingressRules,
		WarpRouting:         warpRoutingConfig,
		OriginDialerService: originDialerService,
		ConfigurationFlags:  parseConfigFlags(c),
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
	period := c.Duration(flags.GracePeriod)
	if period > connection.MaxGracePeriod {
		return time.Duration(0), fmt.Errorf("%s must be equal or less than %v", flags.GracePeriod, connection.MaxGracePeriod)
	}
	return period, nil
}

func isRunningFromTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
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

func parseConfigBindAddress(ipstr string) (net.IP, error) {
	// Unspecified - it's fine
	if ipstr == "" {
		return nil, nil
	}
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return nil, fmt.Errorf("invalid value for edge-bind-address: %s", ipstr)
	}
	return ip, nil
}

func testIPBindable(ip net.IP) error {
	// "Unspecified" = let OS choose, so always bindable
	if ip == nil {
		return nil
	}

	addr := &net.UDPAddr{IP: ip, Port: 0}
	listener, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	listener.Close()
	return nil
}

func adjustIPVersionByBindAddress(ipVersion allregions.ConfigIPVersion, ip net.IP) (allregions.ConfigIPVersion, error) {
	if ip == nil {
		return ipVersion, nil
	}
	// https://pkg.go.dev/net#IP.To4: "If ip is not an IPv4 address, To4 returns nil."
	if ip.To4() != nil {
		if ipVersion == allregions.IPv6Only {
			return allregions.IPv4Only, fmt.Errorf("IPv4 bind address is specified, but edge-ip-version is IPv6")
		}
		return allregions.IPv4Only, nil
	} else {
		if ipVersion == allregions.IPv4Only {
			return allregions.IPv6Only, fmt.Errorf("IPv6 bind address is specified, but edge-ip-version is IPv4")
		}
		return allregions.IPv6Only, nil
	}
}

func newICMPRouter(c *cli.Context, logger *zerolog.Logger) (ingress.ICMPRouterServer, error) {
	ipv4Src, ipv6Src, err := determineICMPSources(c, logger)
	if err != nil {
		return nil, err
	}

	icmpRouter, err := ingress.NewICMPRouter(ipv4Src, ipv6Src, logger, icmpFunnelTimeout)
	if err != nil {
		return nil, err
	}
	return icmpRouter, nil
}

func determineICMPSources(c *cli.Context, logger *zerolog.Logger) (netip.Addr, netip.Addr, error) {
	ipv4Src, err := determineICMPv4Src(c.String(flags.ICMPV4Src), logger)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, errors.Wrap(err, "failed to determine IPv4 source address for ICMP proxy")
	}

	logger.Info().Msgf("ICMP proxy will use %s as source for IPv4", ipv4Src)

	ipv6Src, zone, err := determineICMPv6Src(c.String(flags.ICMPV6Src), logger, ipv4Src)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, errors.Wrap(err, "failed to determine IPv6 source address for ICMP proxy")
	}

	if zone != "" {
		logger.Info().Msgf("ICMP proxy will use %s in zone %s as source for IPv6", ipv6Src, zone)
	} else {
		logger.Info().Msgf("ICMP proxy will use %s as source for IPv6", ipv6Src)
	}

	return ipv4Src, ipv6Src, nil
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
		addr, err := netip.ParseAddr(userDefinedSrc)
		if err != nil {
			return netip.Addr{}, "", err
		}
		if addr.Is6() {
			return addr, addr.Zone(), nil
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

func parseResolverAddrPorts(input []string) ([]netip.AddrPort, error) {
	// We don't allow more than 10 resolvers to be provided statically for the resolver service.
	if len(input) > 10 {
		return nil, errors.New("too many addresses provided, max: 10")
	}
	addrs := make([]netip.AddrPort, 0, len(input))
	for _, val := range input {
		addr, err := netip.ParseAddrPort(val)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}
