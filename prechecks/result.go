package prechecks

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/rs/zerolog"
)

const (
	// Status names.
	passStatus    = "PASS"
	failStatus    = "FAIL"
	skipStatus    = "SKIP"
	unknownStatus = "UNKNOWN"

	// Log message constants.
	logMsgPrecheck         = "precheck"
	logMsgPrecheckComplete = "precheck complete"

	// Log field names.
	logFieldRunID             = "run_id"
	logFieldComponent         = "component"
	logFieldTarget            = "target"
	logFieldStatus            = "status"
	logFieldDetails           = "details"
	logFieldHardFail          = "hard_fail"
	logFieldSuggestedProtocol = "suggested_protocol"
)

// statusLabel returns the display label for a given Status.
func (s Status) statusLabel() string {
	switch s {
	case Pass:
		return passStatus
	case Fail:
		return failStatus
	case Skip:
		return skipStatus
	default:
		return unknownStatus
	}
}

// logString returns the lowercase string used in structured log fields.
func (s Status) logString() string {
	return strings.ToLower(s.String())
}

// renderTable uses text/tabwriter to format the results rows with
// automatically aligned columns, returning the rendered lines.
func renderTable(results []CheckResult) []string {
	var buf bytes.Buffer
	// minwidth=0, tabwidth=8, padding=2, padchar=' ', flags=0
	w := tabwriter.NewWriter(&buf, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "COMPONENT\tTARGET\tSTATUS\tDETAILS")
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Component, r.Target, r.ProbeStatus.statusLabel(), r.Details)
	}
	_ = w.Flush()
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
}

// renderActions collects all non-empty Action strings from results and returns
// the formatted warning/error block that appears between the table and SUMMARY.
// A Fail result is rendered as ERROR when the report is a hard fail, and as
// WARNING otherwise (degraded but tunnel can still run).
func renderActions(r Report) []string {
	hardFail := r.hasHardFail()
	actions := make([]string, 0)
	for _, res := range r.Results {
		if res.Action == "" || res.ProbeStatus != Fail {
			continue
		}
		if hardFail {
			actions = append(actions, fmt.Sprintf("ERROR: %s", res.Action))
		} else {
			actions = append(actions, fmt.Sprintf("WARNING: %s", res.Action))
		}
	}
	return actions
}

// summaryLine builds the SUMMARY: line based on the Report state.
func summaryLine(r Report) string {
	switch {
	case r.hasHardFail():
		return "SUMMARY: Environment has critical failures. cloudflared may not be able to establish a tunnel."
	case r.hasWarn():
		if r.SuggestedProtocol == nil {
			return "SUMMARY: Environment ready with degraded transport."
		}

		protocol := r.SuggestedProtocol.String()
		return fmt.Sprintf("SUMMARY: Environment ready with degraded transport. cloudflared will proceed using '%s'.", protocol)
	default:
		if r.SuggestedProtocol == nil {
			return "SUMMARY: Environment is healthy."
		}

		protocol := r.SuggestedProtocol.String()
		return fmt.Sprintf("SUMMARY: Environment is healthy. cloudflared will use '%s' as primary protocol.", protocol)
	}
}

// hasHardFail returns true when the environment cannot establish a tunnel:
//   - Any DNS probe failed, OR
//   - Both the QUIC and HTTP/2 transport probes failed.
//
// A single transport failing is not a hard fail — the other transport can be
// used as a fallback (degraded state, reported via hasWarn).
func (r Report) hasHardFail() bool {
	var quicFail, http2Fail bool
	for _, res := range r.Results {
		if res.ProbeStatus != Fail {
			continue
		}
		switch res.Type {
		case ProbeTypeDNS:
			return true
		case ProbeTypeQUIC:
			quicFail = true
		case ProbeTypeHTTP2:
			http2Fail = true
		case ProbeTypeManagementAPI:
			// Management API failure is not a hard fail
		}
	}
	return quicFail && http2Fail
}

// hasWarn returns true when the environment is degraded but functional:
//   - Exactly one transport (QUIC or HTTP/2) failed, OR
//   - The Management API is unreachable (auto-updates unavailable).
//
// Hard-fail conditions (DNS down, both transports blocked) take precedence
// and will cause hasWarn to return false.
func (r Report) hasWarn() bool {
	if r.hasHardFail() {
		return false
	}
	var quicFail, http2Fail, apiFail bool
	for _, res := range r.Results {
		if res.ProbeStatus != Fail {
			continue
		}
		switch res.Type {
		case ProbeTypeQUIC:
			quicFail = true
		case ProbeTypeHTTP2:
			http2Fail = true
		case ProbeTypeManagementAPI:
			apiFail = true
		case ProbeTypeDNS:
			// DNS failures are only relevant for hard failures
		}
	}
	return (quicFail != http2Fail) || apiFail
}

// String renders the Report as human-readable table lines suitable for logging.
func (r Report) String() []string {
	lines := renderTable(r.Results)
	lines = append(lines, renderActions(r)...)
	lines = append(lines, "", summaryLine(r))
	return lines
}

// LogEvent emits each CheckResult as a structured zerolog log line, followed by
// a final summary event. This is the JSON-logging equivalent of String().
// Every line carries run_id so all results from a single invocation can be correlated.
func (r Report) LogEvent(logger *zerolog.Logger) {
	runID := r.RunID.String()
	for _, res := range r.Results {
		logger.Info().
			Str(logFieldRunID, runID).
			Str(logFieldComponent, res.Component).
			Str(logFieldTarget, res.Target).
			Str(logFieldStatus, res.ProbeStatus.logString()).
			Str(logFieldDetails, res.Details).
			Msg(logMsgPrecheck)
	}

	if r.SuggestedProtocol != nil {
		logger.Info().
			Str(logFieldRunID, runID).
			Bool(logFieldHardFail, r.hasHardFail()).
			Str(logFieldSuggestedProtocol, r.SuggestedProtocol.String()).
			Msg(logMsgPrecheckComplete)
	} else {
		logger.Info().
			Str(logFieldRunID, runID).
			Bool(logFieldHardFail, r.hasHardFail()).
			Msg(logMsgPrecheckComplete)
	}
}
