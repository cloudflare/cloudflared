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

// TransportResults holds the per-target results for each transport probe type.
// Each slice has one entry per resolved target group, in the same order as the
// target labels slice.
type TransportResults struct {
	QUIC          []CheckResult // one per target group
	HTTP2         []CheckResult // one per target group
	ManagementAPI CheckResult   // single target, no groups
}

// Collect returns all results as a slice in a consistent order for reporting:
// all QUIC rows first (one per target), then all HTTP2 rows, then Management API.
func (tr TransportResults) Collect() []CheckResult {
	results := make([]CheckResult, 0, len(tr.QUIC)+len(tr.HTTP2)+1)
	results = append(results, tr.QUIC...)
	results = append(results, tr.HTTP2...)
	results = append(results, tr.ManagementAPI)
	return results
}

// Run executes the following connectivity pre-checks:
//
//  1. Edge address resolution — either DNS-based SRV discovery (normal path)
//     or direct resolution of --edge addresses (static path). The static path
//     skips DNS probe rows entirely since there are no SRV records to validate.
//  2. QUIC, HTTP/2, and Management API probes run concurrently against the
//     resolved addresses.
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

	// Build TLS configs once per protocol.
	quicTLSConfig, quicTLSErr := probeTLSConfig(caCert, connection.QUIC)
	http2TLSConfig, http2TLSErr := probeTLSConfig(caCert, connection.HTTP2)

	// 1) Resolve edge addresses. Each ResolvedTarget bundles its addr group
	//    with the DNS CheckResult that labels it, keeping the two in sync.
	var resolvedTargets []ResolvedTarget
	if len(cfg.EdgeAddrs) > 0 {
		// Static path: explicit --edge addresses, one ResolvedTarget per addr.
		resolvedTargets = resolveStaticEdge(cfg.EdgeAddrs, log)
	} else {
		// Normal path: SRV-based discovery; DNS rows carry Pass or Fail status.
		resolvedTargets = runDNSProbe(ctx, runDialers.DNSResolver, cfg.Region)
	}

	// Extract parallel slices for the transport probe layer.
	// nolint:prealloc // False positive. The linter is confused by the append used when producing Report.Results
	dnsResults := make([]CheckResult, len(resolvedTargets))
	perGroupAddrs := make([][]*allregions.EdgeAddr, len(resolvedTargets))
	targetLabels := make([]string, len(resolvedTargets))
	for i, rt := range resolvedTargets {
		dnsResults[i] = rt.DNSResult
		perGroupAddrs[i] = rt.Addrs
		targetLabels[i] = rt.DNSResult.Target
	}

	// dnsOK is true when at least one target has addresses to probe.
	dnsOK := slices.ContainsFunc(resolvedTargets, func(r ResolvedTarget) bool {
		return len(r.Addrs) > 0
	})

	// 2) Run transport probes concurrently. Each probe type gets its own
	//    buffered channel — one send, one receive, no routing required.
	var results TransportResults

	mgmtCh := make(chan CheckResult)
	go func() {
		mgmtCh <- probeManagementAPIWithRetry(ctx, runDialers.ManagementDialer)
	}()

	if !dnsOK {
		// No addresses available: emit one skip row per target so the table
		// stays consistent with the DNS rows above.
		results.QUIC = skipResultsForTargets(dnsResults, ProbeTypeQUIC, componentUDPConnectivity)
		results.HTTP2 = skipResultsForTargets(dnsResults, ProbeTypeHTTP2, componentTCPConnectivity)
	} else {
		filteredAddrs := addrsByGroup(perGroupAddrs, cfg.IPVersion)

		quicCh := make(chan []CheckResult, 1)
		http2Ch := make(chan []CheckResult, 1)

		go func() {
			if quicTLSErr != nil {
				log.Warn().Err(quicTLSErr).Msg("Failed to build QUIC probe TLS config")
				quicCh <- tlsConfigErrResults(ProbeTypeQUIC, componentUDPConnectivity,
					targetLabels, fmt.Sprintf("%s: %v", detailsTLSConfigFailed, quicTLSErr), actionQUICBlocked)
				return
			}
			quicCh <- probeAllTargets(ctx, ProbeTypeQUIC, componentUDPConnectivity,
				filteredAddrs, targetLabels,
				func(addr *allregions.EdgeAddr) CheckResult {
					return probeQUIC(ctx, quicTLSConfig, runDialers.QUICDialer, addr, log)
				})
		}()

		go func() {
			if http2TLSErr != nil {
				log.Warn().Err(http2TLSErr).Msg("Failed to build HTTP/2 probe TLS config")
				http2Ch <- tlsConfigErrResults(ProbeTypeHTTP2, componentTCPConnectivity,
					targetLabels, fmt.Sprintf("%s: %v", detailsTLSConfigFailed, http2TLSErr), actionHTTP2Blocked)
				return
			}
			http2Ch <- probeAllTargets(ctx, ProbeTypeHTTP2, componentTCPConnectivity,
				filteredAddrs, targetLabels,
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

// tlsConfigErrResults returns one Fail CheckResult per target, used when
// TLS config construction fails before any dial is attempted.
func tlsConfigErrResults(probeType ProbeType, component string, targets []string, details, action string) []CheckResult {
	results := make([]CheckResult, len(targets))
	for i, target := range targets {
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

// probeAllTargets probes each target group sequentially and returns one
// CheckResult per group. Within each group, all available addresses (V4 and/or
// V6) are tried and the best result is kept.
func probeAllTargets(
	ctx context.Context,
	probeType ProbeType,
	component string,
	perGroupAddrs [][]*allregions.EdgeAddr,
	targets []string,
	probeFn func(*allregions.EdgeAddr) CheckResult,
) []CheckResult {
	results := make([]CheckResult, len(perGroupAddrs))
	for i, addrs := range perGroupAddrs {
		results[i] = probeTarget(ctx, probeType, component, targets[i], addrs, probeFn)
	}
	return results
}

// probeTarget probes all addresses for a single target group (typically one V4
// and/or one V6) and returns the best result. Any address passing means the
// target is reachable, so Pass beats Fail within a group.
func probeTarget(
	ctx context.Context,
	probeType ProbeType,
	component string,
	target string,
	addrs []*allregions.EdgeAddr,
	probeFn func(*allregions.EdgeAddr) CheckResult,
) CheckResult {
	if len(addrs) == 0 {
		return CheckResult{
			Type:        probeType,
			Component:   component,
			Target:      target,
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
	best.Target = target
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

// addrsByGroup returns the addresses to probe for each resolved target group,
// preserving the per-group structure. Each inner slice contains at most one V4
// and one V6 address (subject to ipVersion).
func addrsByGroup(addrGroups [][]*allregions.EdgeAddr, ipVersion allregions.ConfigIPVersion) [][]*allregions.EdgeAddr {
	perGroup := make([][]*allregions.EdgeAddr, 0, len(addrGroups))
	for _, group := range addrGroups {
		v4, v6 := addrsByFamily(group, ipVersion)
		var addrs []*allregions.EdgeAddr
		if v4 != nil {
			addrs = append(addrs, v4)
		}
		if v6 != nil {
			addrs = append(addrs, v6)
		}
		perGroup = append(perGroup, addrs)
	}
	return perGroup
}

// skipResultsForTargets returns one skip CheckResult per entry in results,
// using each entry's Target label so the transport row aligns with its DNS row.
func skipResultsForTargets(targets []CheckResult, probeType ProbeType, component string) []CheckResult {
	results := make([]CheckResult, len(targets))
	for i, t := range targets {
		results[i] = skipResult(probeType, component, t.Target, detailsDNSPrerequisiteFailed)
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
// true. Between attempts, it sleeps with exponential backoff bounded by
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
