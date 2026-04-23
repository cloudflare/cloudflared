package prechecks

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

// Status represents the outcome of a single connectivity pre-check.
type Status int

const (
	// Pass indicates the check completed successfully.
	Pass Status = iota
	// Fail indicates the check did not succeed. Whether this is a hard failure
	// or a degraded-but-functional state depends on which probe(s) failed — see
	// Report.hasHardFail and Report.hasWarn.
	Fail
	// Skip indicates the check was not executed because a prerequisite
	// check (typically DNS) failed first.
	Skip
)

// String returns the canonical display name for a Status value.
func (s Status) String() string {
	switch s {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	case Skip:
		return "SKIP"
	default:
		return "UNKNOWN"
	}
}

// ProbeType identifies which connectivity probe produced a CheckResult.
// It is used by hasHardFail and hasWarn to evaluate severity without
// matching against human-readable strings.
type ProbeType int

const (
	ProbeTypeDNS           ProbeType = iota // DNS resolution
	ProbeTypeQUIC                           // UDP/QUIC transport
	ProbeTypeHTTP2                          // TCP/HTTP2 transport
	ProbeTypeManagementAPI                  // Cloudflare management API
)

// CheckResult holds the outcome of one individual connectivity probe.
type CheckResult struct {
	// Type identifies which probe produced this result. Used for severity
	// classification in hasHardFail and hasWarn.
	Type ProbeType

	// Component is the human-readable probe category shown in the table header
	// column, e.g. "DNS Resolution", "QUIC Connectivity".
	Component string

	// Target is the address or resource that was probed, e.g.
	// "region1.v2.argotunnel.com" or "Port 7844 (QUIC)".
	Target string

	// ProbeStatus is the outcome of the probe.
	ProbeStatus Status

	// Details is a short description of the result shown in the table, e.g.
	// "Resolved successfully" or "Handshake failed".
	Details string

	// Action is non-empty when ProbeStatus is Fail and contains a human-readable
	// remediation instruction, e.g. "Allow outbound QUIC on port 7844."
	Action string
}

// Report aggregates all CheckResults produced by a single Run() invocation.
// Pre-checks run in parallel with tunnel initialization and are purely
// diagnostic: the Report is displayed to the user but never gates startup.
type Report struct {
	// RunID is a unique identifier for this pre-check run. It is included in
	// every structured log line so that all results from a single invocation
	// can be correlated across log aggregation systems.
	RunID uuid.UUID

	// Results contains one entry per executed probe, in the order they were
	// collected.
	Results []CheckResult

	// SuggestedProtocol is the connection protocol the pre-checks recommend
	// based on transport probe results.
	SuggestedProtocol connection.Protocol
}

// Config controls the behavior of a pre-check Run().
type Config struct {
	// Region is the optional cloudflared --region flag value. When non-empty
	// the pre-check probes the regional edge hostnames instead of the global ones.
	Region string

	// Timeout is the maximum wall-clock duration allowed for the entire
	// pre-check suite to complete.
	Timeout time.Duration

	// IPVersion controls which address families are probed for transport
	// checks. It mirrors the --edge-ip-version CLI flag so that the pre-check
	// exercises the same code paths the tunnel itself will use.
	IPVersion allregions.ConfigIPVersion
}
