package prechecks

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"net/netip"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection/dialopts"

	"github.com/cloudflare/cloudflared/connection"
	edgedial "github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

const (
	perProbeDialTimeout = 5 * time.Second

	// Action messages for each probe outcome.
	actionDNSFail        = "Ensure your DNS resolver can resolve '%s'. Run: dig A %s @1.1.1.1. If that fails, contact your network administrator."
	actionQUICBlocked    = "Allow outbound QUIC traffic on port 7844 or use HTTP2."
	actionHTTP2Blocked   = "Allow outbound TCP on port 7844."
	actionAPIUnreachable = "cloudflared will still run, but automatic software updates are unavailable. " +
		"Ensure port 443 TCP to api.cloudflare.com is open if you want auto-updates."

	// Component names for CheckResult.
	componentDNSResolution   = "DNS Resolution"
	componentUDPConnectivity = "UDP Connectivity"
	componentTCPConnectivity = "TCP Connectivity"
	componentCloudflareAPI   = "Cloudflare API"

	// Target identifiers for CheckResult.
	targetPortQUIC  = "Port 7844 (QUIC)"
	targetPortHTTP2 = "Port 7844 (HTTP/2)"
	targetAPI       = "api.cloudflare.com:443"
	noDNSTarget     = "No DNS target (Using edge flag)"

	// Details messages for CheckResult.
	dnsNoAddressesReturned           = "No addresses returned"
	dnsResolvedSuccessfully          = "DNS Resolved successfully"
	detailsQUICHandshakeFailed       = "QUIC connection failed"
	detailsQUICHandshakeSuccessful   = "QUIC connection successful"
	detailsHTTP2BlockedOrUnreachable = "HTTP/2 connection is blocked or unreachable"
	detailsHTTP2HandshakeSuccessful  = "HTTP/2 connection successful"
	detailsAPIConnectionFailed       = "API Connection failed"
	detailsApiReachable              = "API is reachable"
	detailsDNSPrerequisiteFailed     = "DNS prerequisite failed"
	detailsTLSConfigFailed           = "TLS configuration failed"

	// Region hostname templates.
	region1Global = "region1.v2.argotunnel.com"
	region2Global = "region2.v2.argotunnel.com"
	region1US     = "us-region1.v2.argotunnel.com"
	region2US     = "us-region2.v2.argotunnel.com"
	region1Fed    = "fed-region1.v2.argotunnel.com"
	region2Fed    = "fed-region2.v2.argotunnel.com"
)

// EdgeDNSResolver implements DNSResolver for the standard DNS-based edge
// discovery path.
type EdgeDNSResolver struct {
	Log *zerolog.Logger
}

func (r *EdgeDNSResolver) Resolve(region string) ([][]*allregions.EdgeAddr, error) {
	return allregions.EdgeDiscovery(r.Log, allregions.RegionalServiceName(region))
}

type EdgeTCPDialer struct{}

func (d *EdgeTCPDialer) DialEdge(
	ctx context.Context,
	timeout time.Duration,
	tlsConfig *tls.Config,
	addr *net.TCPAddr,
	localIP net.IP,
) (net.Conn, error) {
	return edgedial.DialEdge(ctx, timeout, tlsConfig, addr, localIP)
}

type EdgeQUICDialer struct{}

func (d *EdgeQUICDialer) DialQuic(
	ctx context.Context,
	quicConfig *quic.Config,
	tlsConfig *tls.Config,
	addr netip.AddrPort,
	localAddr net.IP,
	connIndex uint8,
	logger *zerolog.Logger,
	opts dialopts.DialOpts,
) (quic.Connection, error) {
	return connection.DialQuic(ctx, quicConfig, tlsConfig, addr, localAddr, connIndex, logger, opts)
}

type NetManagementDialer struct {
	Dialer net.Dialer
}

func (d *NetManagementDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.Dialer.DialContext(ctx, network, addr)
}

// probeTLSConfig builds a *tls.Config for a pre-check probe using the same
// certificate pool as the production tunnel. The SNI and NextProtos are taken from
// p.ProbeTLSSettings() so that the probe SNI is used instead of the production SNI,
// which avoids noisy logs in origintunneld.
func probeTLSConfig(caCert string, p connection.Protocol) (*tls.Config, error) {
	settings := p.ProbeTLSSettings()
	if settings == nil {
		return nil, fmt.Errorf("no probe TLS settings for protocol %s", p)
	}
	cfg, err := tlsconfig.CreateTunnelConfig(caCert, settings.ServerName)
	if err != nil {
		return nil, err
	}
	if len(settings.NextProtos) > 0 {
		cfg.NextProtos = settings.NextProtos
	}
	return cfg, nil
}

// probeDNS resolves edge addresses for the given region via the supplied
// DNSResolver and returns one ResolvedTarget per discovered region. If
// resolution fails entirely, every ResolvedTarget will carry a Fail DNSResult
// and nil Addrs.
func probeDNS(
	resolver DNSResolver,
	region string,
) []ResolvedTarget {
	region1Target, region2Target := regionTargets(region)
	targets := []string{region1Target, region2Target}

	addrGroups, err := resolver.Resolve(region)
	if err != nil || len(addrGroups) == 0 {
		detail := dnsNoAddressesReturned
		if err != nil {
			detail = err.Error()
		}
		return []ResolvedTarget{
			{DNSResult: newDNSCheckResult(region1Target, Fail, detail, fmt.Sprintf(actionDNSFail, region1Target, region1Target))},
			{DNSResult: newDNSCheckResult(region2Target, Fail, detail, fmt.Sprintf(actionDNSFail, region2Target, region2Target))},
		}
	}

	resolved := make([]ResolvedTarget, 0, len(addrGroups))
	for i, target := range targets {
		if i >= len(addrGroups) {
			break
		}
		group := addrGroups[i]
		if len(group) == 0 {
			resolved = append(resolved, ResolvedTarget{
				DNSResult: newDNSCheckResult(target, Fail, dnsNoAddressesReturned, fmt.Sprintf(actionDNSFail, target, target)),
			})
		} else {
			resolved = append(resolved, ResolvedTarget{
				Addrs:     group,
				DNSResult: newDNSCheckResult(target, Pass, dnsResolvedSuccessfully, ""),
			})
		}
	}

	return resolved
}

// probeQUIC performs a QUIC handshake to a single edge address and returns a
// CheckResult. The connection is closed immediately after the handshake – no
// streams are opened and no RPC frames are sent – to avoid triggering the OTD
// registration timeout (TUN-6732). The probe SNI (probe.cftunnel.com) is used
// instead of the production quic.cftunnel.com to prevent OTD log noise.
//
// A per-probe deadline (perProbeDialTimeout) is applied on top of the parent
// context so that a single blocked handshake cannot consume the entire suite
// budget.
func probeQUIC(
	ctx context.Context,
	tlsConfig *tls.Config,
	dialer QUICDialer,
	addr *allregions.EdgeAddr,
	logger *zerolog.Logger,
) CheckResult {
	dialCtx, cancel := context.WithTimeout(ctx, perProbeDialTimeout)
	defer cancel()

	// We call dialer.DialQuic with isProbe = true, which bypasses connIndex check.
	// Therefore, whatever we add to connIndex will not be relevant.
	edgeAddrPort := addr.UDP.AddrPort()
	conn, err := dialer.DialQuic(
		dialCtx,
		&quic.Config{},
		tlsConfig,
		edgeAddrPort,
		nil,
		math.MaxUint8,
		logger,
		dialopts.DialOpts{SkipPortReuse: true},
	)
	if err != nil {
		return CheckResult{
			Type:        ProbeTypeQUIC,
			Component:   componentUDPConnectivity,
			Target:      targetPortQUIC,
			ProbeStatus: Fail,
			Details:     detailsQUICHandshakeFailed,
			Action:      actionQUICBlocked,
		}
	}

	if err := conn.CloseWithError(0, "precheck complete"); err != nil {
		logger.Debug().Err(err).Msg("Failed to close QUIC connection after successful handshake")
	}

	return CheckResult{
		Type:        ProbeTypeQUIC,
		Component:   componentUDPConnectivity,
		Target:      targetPortQUIC,
		ProbeStatus: Pass,
		Details:     detailsQUICHandshakeSuccessful,
	}
}

// probeHTTP2 performs a TCP + TLS handshake to a single edge address and
// returns a CheckResult. The connection is closed immediately after the
// handshake – no HTTP/2 frames are sent – to keep the probe minimal. The probe
// SNI (probe.cftunnel.com) is used instead of the production h2.cftunnel.com
// to prevent OTD log noise.
//
// The dial timeout is capped at perProbeDialTimeout so that a single blocked
// dial cannot exhaust the entire suite budget.
func probeHTTP2(ctx context.Context, tlsConfig *tls.Config, dialer TCPDialer, addr *allregions.EdgeAddr) CheckResult {
	conn, err := dialer.DialEdge(ctx, perProbeDialTimeout, tlsConfig, addr.TCP, nil)
	if err != nil {
		return CheckResult{
			Type:        ProbeTypeHTTP2,
			Component:   componentTCPConnectivity,
			Target:      targetPortHTTP2,
			ProbeStatus: Fail,
			Details:     detailsHTTP2BlockedOrUnreachable,
			Action:      actionHTTP2Blocked,
		}
	}
	_ = conn.Close()

	return CheckResult{
		Type:        ProbeTypeHTTP2,
		Component:   componentTCPConnectivity,
		Target:      targetPortHTTP2,
		ProbeStatus: Pass,
		Details:     detailsHTTP2HandshakeSuccessful,
	}
}

// probeManagementAPI tests TCP connectivity to api.cloudflare.com:443. A
// successful TCP connection (no TLS handshake required) confirms the port is
// reachable. This probe is always a soft failure: the tunnel can run without
// it, but automatic software updates will be unavailable.
func probeManagementAPI(ctx context.Context, dialer ManagementDialer) CheckResult {
	dialCtx, cancel := context.WithTimeout(ctx, perProbeDialTimeout)
	defer cancel()

	conn, err := dialer.DialContext(dialCtx, "tcp", targetAPI)
	if err != nil {
		return CheckResult{
			Type:        ProbeTypeManagementAPI,
			Component:   componentCloudflareAPI,
			Target:      targetAPI,
			ProbeStatus: Fail,
			Details:     detailsAPIConnectionFailed,
			Action:      actionAPIUnreachable,
		}
	}
	_ = conn.Close()

	return CheckResult{
		Type:        ProbeTypeManagementAPI,
		Component:   componentCloudflareAPI,
		Target:      targetAPI,
		ProbeStatus: Pass,
		Details:     detailsApiReachable,
	}
}

func skipResult(probeType ProbeType, component, target string, details string) CheckResult {
	return CheckResult{
		Type:        probeType,
		Component:   component,
		Target:      target,
		ProbeStatus: Skip,
		Details:     details,
	}
}

// newDNSCheckResult creates a DNS CheckResult with the given fields.
// Type and Component are always ProbeTypeDNS and componentDNSResolution.
func newDNSCheckResult(target string, status Status, details, action string) CheckResult {
	return CheckResult{
		Type:        ProbeTypeDNS,
		Component:   componentDNSResolution,
		Target:      target,
		ProbeStatus: status,
		Details:     details,
		Action:      action,
	}
}

// regionTargets returns the human-readable hostnames for region1 and region2
// based on the optional region flag value.
func regionTargets(region string) (string, string) {
	switch region {
	case "us":
		return region1US, region2US
	case "fed":
		return region1Fed, region2Fed
	default:
		return region1Global, region2Global
	}
}

// addrsByFamily extracts one V4 and one V6 address from a resolved CNAME group
// using allregions.NewRegion so that the IP-version preference logic matches
// production exactly. When cfg.IPVersion restricts to a single family the
// excluded family's pointer is nil.
func addrsByFamily(group []*allregions.EdgeAddr, ipVersion allregions.ConfigIPVersion) (v4, v6 *allregions.EdgeAddr) {
	if ipVersion != allregions.IPv6Only {
		v4 = allregions.NewRegion(group, allregions.IPv4Only).GetAnyAddress()
	}
	if ipVersion != allregions.IPv4Only {
		v6 = allregions.NewRegion(group, allregions.IPv6Only).GetAnyAddress()
	}
	return
}

// runDNSProbe runs probeDNS with retry and returns []ResolvedTarget.
func runDNSProbe(ctx context.Context, resolver DNSResolver, region string) []ResolvedTarget {
	var targets []ResolvedTarget
	withRetry(ctx, maxRetries, func() bool {
		targets = probeDNS(resolver, region)
		for _, t := range targets {
			if t.DNSResult.ProbeStatus == Fail {
				return false
			}
		}
		return len(targets) > 0
	})
	return targets
}

// resolveStaticEdge resolves each --edge addr individually, returning one
// ResolvedTarget per addr. Unresolvable addrs produce a Fail ResolvedTarget
// with nil Addrs so the report shows which addresses could not be reached.
func resolveStaticEdge(addrs []string, log *zerolog.Logger) []ResolvedTarget {
	targets := make([]ResolvedTarget, 0, len(addrs))
	for _, addr := range addrs {
		resolved := allregions.ResolveAddrs([]string{addr}, log)
		if len(resolved) > 0 {
			targets = append(targets, ResolvedTarget{
				Addrs:     resolved,
				DNSResult: newDNSCheckResult(addr, Pass, dnsResolvedSuccessfully, ""),
			})
		} else {
			targets = append(targets, ResolvedTarget{
				DNSResult: newDNSCheckResult(addr, Fail, dnsNoAddressesReturned, fmt.Sprintf(actionDNSFail, addr, addr)),
			})
		}
	}
	return targets
}
