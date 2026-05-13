package prechecks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cloudflare/backoff"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

const (
	defaultTimeout = 10 * time.Second
	maxRetries     = 2
	retryBaseDelay = 1 * time.Second
	maxRetryDelay  = 16 * time.Second
)

// RunDialers holds the injectable dependencies for Run(). Production callers build
// this with real implementations; tests supply mocks.
type RunDialers struct {
	DNSResolver      DNSResolver
	TCPDialer        TCPDialer
	QUICDialer       QUICDialer
	ManagementDialer ManagementDialer
}

// TransportResults holds the per-region results for each transport probe type.
// Each slice has one entry per DNS-resolved region, in the same order as dnsResults.
type TransportResults struct {
	QUIC          []CheckResult // one per region
	HTTP2         []CheckResult // one per region
	ManagementAPI CheckResult   // single target, no regions
}

// Collect returns all results as a slice in a consistent order for reporting:
// all QUIC rows first (one per region), then all HTTP2 rows, then Management API.
func (tr TransportResults) Collect() []CheckResult {
	results := make([]CheckResult, 0, len(tr.QUIC)+len(tr.HTTP2)+1)
	results = append(results, tr.QUIC...)
	results = append(results, tr.HTTP2...)
	results = append(results, tr.ManagementAPI)
	return results
}

// Run executes the following connectivity pre-checks:
//
//  1. DNS resolution (sequential – transport probes depend on its output).
//  2. QUIC, HTTP/2, and Management API probes run concurrently.
//
// Each failed probe is retried up to maxRetries times with exponential backoff.
// The suite is bounded by cfg.Timeout (defaultTimeout if zero).
func Run(ctx context.Context, caCert string, cfg Config, log *zerolog.Logger, runDialers RunDialers) Report {
	runID := uuid.New()

	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Build TLS configs once per protocol
	quicTLSConfig, quicTLSErr := probeTLSConfig(caCert, connection.QUIC)
	http2TLSConfig, http2TLSErr := probeTLSConfig(caCert, connection.HTTP2)

	// 1) DNS – must complete before transport probes know which addresses to dial.
	addrGroups, dnsResults := runDNSProbe(ctx, runDialers.DNSResolver, cfg.Region)

	dnsOK := !slices.ContainsFunc(dnsResults, func(r CheckResult) bool {
		return r.ProbeStatus != Pass
	})

	// 2) Run probes concurrently. Each probe type gets its own buffered channel —
	//    one send, one receive, no routing or name-parsing required.
	var results TransportResults

	mgmtCh := make(chan CheckResult)
	go func() {
		mgmtCh <- probeManagementAPIWithRetry(ctx, runDialers.ManagementDialer)
	}()

	if !dnsOK {
		// DNS failed: emit one skip row per region so the table stays consistent.
		results.QUIC = skipResultsForRegions(dnsResults, ProbeTypeQUIC, componentUDPConnectivity)
		results.HTTP2 = skipResultsForRegions(dnsResults, ProbeTypeHTTP2, componentTCPConnectivity)
	} else {
		perRegionAddrs := addrsByRegion(addrGroups, cfg.IPVersion)
		regionTargets := dnsTargets(dnsResults)

		quicCh := make(chan []CheckResult, 1)
		http2Ch := make(chan []CheckResult, 1)

		go func() {
			if quicTLSErr != nil {
				log.Warn().Err(quicTLSErr).Msg("Failed to build QUIC probe TLS config")
				quicCh <- tlsConfigErrResults(ProbeTypeQUIC, componentUDPConnectivity,
					regionTargets, fmt.Sprintf("%s: %v", detailsTLSConfigFailed, quicTLSErr), actionQUICBlocked)
				return
			}
			quicCh <- probeAllRegions(ctx, ProbeTypeQUIC, componentUDPConnectivity,
				perRegionAddrs, regionTargets,
				func(addr *allregions.EdgeAddr) CheckResult {
					return probeQUIC(ctx, quicTLSConfig, runDialers.QUICDialer, addr, log)
				})
		}()

		go func() {
			if http2TLSErr != nil {
				log.Warn().Err(http2TLSErr).Msg("Failed to build HTTP/2 probe TLS config")
				http2Ch <- tlsConfigErrResults(ProbeTypeHTTP2, componentTCPConnectivity,
					regionTargets, fmt.Sprintf("%s: %v", detailsTLSConfigFailed, http2TLSErr), actionHTTP2Blocked)
				return
			}
			http2Ch <- probeAllRegions(ctx, ProbeTypeHTTP2, componentTCPConnectivity,
				perRegionAddrs, regionTargets,
				func(addr *allregions.EdgeAddr) CheckResult {
					return probeHTTP2(ctx, http2TLSConfig, runDialers.TCPDialer, addr)
				})
		}()

		results.QUIC = <-quicCh
		results.HTTP2 = <-http2Ch
	}

	results.ManagementAPI = <-mgmtCh

	return Report{
		RunID:             runID,
		Results:           append(dnsResults, results.Collect()...),
		SuggestedProtocol: suggestProtocol(results.QUIC, results.HTTP2),
	}
}

// tlsConfigErrResults returns one Fail CheckResult per region target, used when
// TLS config construction fails before any dial is attempted.
func tlsConfigErrResults(probeType ProbeType, component string, regionTargets []string, details, action string) []CheckResult {
	results := make([]CheckResult, len(regionTargets))
	for i, target := range regionTargets {
		results[i] = CheckResult{
			Type:        probeType,
			Component:   component,
			Target:      target,
			ProbeStatus: Fail,
			Details:     details,
			Action:      action,
		}
	}
	return results
}

func runDNSProbe(ctx context.Context, resolver DNSResolver, region string) ([][]*allregions.EdgeAddr, []CheckResult) {
	var addrGroups [][]*allregions.EdgeAddr
	var dnsResults []CheckResult
	withRetry(ctx, maxRetries, func() bool {
		addrGroups, dnsResults = probeDNS(resolver, region)
		for _, r := range dnsResults {
			if r.ProbeStatus == Fail {
				return false
			}
		}
		return len(dnsResults) > 0
	})
	return addrGroups, dnsResults
}

// probeAllRegions probes each region sequentially and returns one CheckResult
// per region. Within each region, all available addresses (V4 and/or V6) are
// tried and the best result is kept.
func probeAllRegions(
	ctx context.Context,
	probeType ProbeType,
	component string,
	perRegionAddrs [][]*allregions.EdgeAddr,
	regionTargets []string,
	probeFn func(*allregions.EdgeAddr) CheckResult,
) []CheckResult {
	results := make([]CheckResult, len(perRegionAddrs))
	for i, addrs := range perRegionAddrs {
		results[i] = probeRegion(ctx, probeType, component, regionTargets[i], addrs, probeFn)
	}
	return results
}

// probeRegion probes all addresses for a single region (typically one V4 and/or
// one V6) and returns the best result. Any address passing means the region is
// reachable, so Pass beats Fail within a region.
func probeRegion(
	ctx context.Context,
	probeType ProbeType,
	component string,
	regionTarget string,
	addrs []*allregions.EdgeAddr,
	probeFn func(*allregions.EdgeAddr) CheckResult,
) CheckResult {
	if len(addrs) == 0 {
		return CheckResult{
			Type:        probeType,
			Component:   component,
			Target:      regionTarget,
			ProbeStatus: Skip,
			Details:     "No suitable address found for configured IP version",
		}
	}

	best := probeWithRetry(ctx, addrs[0], probeFn)
	for _, addr := range addrs[1:] {
		if r := probeWithRetry(ctx, addr, probeFn); r.ProbeStatus == Pass {
			best = r
		}
	}
	best.Target = regionTarget
	return best
}

// probeManagementAPIWithRetry runs the Cloudflare API reachability probe with retry.
func probeManagementAPIWithRetry(ctx context.Context, dialer ManagementDialer) CheckResult {
	var r CheckResult
	withRetry(ctx, maxRetries, func() bool {
		r = probeManagementAPI(ctx, dialer)
		return r.ProbeStatus == Pass
	})
	return r
}

// probeWithRetry calls probeFn on addr with exponential-backoff retry up to
// maxRetries times, stopping as soon as the probe passes.
func probeWithRetry(ctx context.Context, addr *allregions.EdgeAddr, probeFn func(*allregions.EdgeAddr) CheckResult) CheckResult {
	var r CheckResult
	withRetry(ctx, maxRetries, func() bool {
		r = probeFn(addr)
		return r.ProbeStatus == Pass
	})
	return r
}

// addrsByRegion returns the addresses to probe for each DNS-resolved region,
// preserving the per-region grouping. Each inner slice contains at most one V4
// and one V6 address (subject to ipVersion).
func addrsByRegion(addrGroups [][]*allregions.EdgeAddr, ipVersion allregions.ConfigIPVersion) [][]*allregions.EdgeAddr {
	perRegion := make([][]*allregions.EdgeAddr, 0, len(addrGroups))
	for _, group := range addrGroups {
		v4, v6 := addrsByFamily(group, ipVersion)
		var addrs []*allregions.EdgeAddr
		if v4 != nil {
			addrs = append(addrs, v4)
		}
		if v6 != nil {
			addrs = append(addrs, v6)
		}
		perRegion = append(perRegion, addrs)
	}
	return perRegion
}

// dnsTargets extracts the Target hostname from each DNS CheckResult so that
// transport probe rows reuse the same region hostnames.
func dnsTargets(dnsResults []CheckResult) []string {
	targets := make([]string, len(dnsResults))
	for i, r := range dnsResults {
		targets[i] = r.Target
	}
	return targets
}

// skipResultsForRegions returns one skip CheckResult per DNS region, using each
// region's hostname as the Target so the output table row aligns with its DNS row.
func skipResultsForRegions(dnsResults []CheckResult, probeType ProbeType, component string) []CheckResult {
	results := make([]CheckResult, len(dnsResults))
	for i, dns := range dnsResults {
		results[i] = skipResult(probeType, component, dns.Target)
	}
	return results
}

// worstStatus returns the most severe Status across a slice of CheckResults.
// Fail > Pass > Skip. Used to determine whether a transport type as a whole
// should be considered failed (any region failing = transport fails).
func worstStatus(results []CheckResult) Status {
	worst := Skip
	for _, r := range results {
		if severity(r.ProbeStatus) > severity(worst) {
			worst = r.ProbeStatus
		}
	}
	return worst
}

// severity maps a Status to a comparable integer so that worse outcomes rank higher.
func severity(s Status) int {
	switch s {
	case Fail:
		return 2
	case Pass:
		return 1
	case Skip:
		return 0
	default:
		return 0
	}
}

// suggestProtocol recommends QUIC when all QUIC region probes passed, HTTP/2
// when all HTTP/2 probes passed, and nil when neither transport works.
// Any region failing means the transport is treated as failed (worst wins).
func suggestProtocol(quicResults, http2Results []CheckResult) *connection.Protocol {
	if len(quicResults) > 0 && worstStatus(quicResults) == Pass {
		quic := connection.QUIC
		return &quic
	}
	if len(http2Results) > 0 && worstStatus(http2Results) == Pass {
		http2 := connection.HTTP2
		return &http2
	}
	return nil
}

// withRetry calls fn up to 1+maxAttempts times, stopping as soon as fn returns
// true. Between attempts it sleeps with exponential backoff bounded by
// maxRetryDelay, and stops early if ctx is done.
func withRetry(ctx context.Context, maxAttempts int, fn func() bool) {
	b := backoff.NewWithoutJitter(maxRetryDelay, retryBaseDelay)
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if fn() {
			return
		}
		if attempt == maxAttempts {
			break
		}
		timer := time.NewTimer(b.Duration())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
