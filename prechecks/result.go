package prechecks

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/rs/zerolog"
)

const (
	// tableWidth is the total character width of the separator lines.
	tableWidth = 80

	// Status names.
	passStatus    = "PASS"
	failStatus    = "FAIL"
	skipStatus    = "SKIP"
	unknownStatus = "UNKNOWN"

	// Section separators.
	sectionChar = "-"
	headerTitle = "CONNECTIVITY PRE-CHECKS"

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

	sep = " "
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

// separator returns a full-width horizontal line.
func separator() string {
	return strings.Repeat(sectionChar, tableWidth)
}

// header returns the top section title line.
func header() string {
	leftDashes := strings.Repeat(sectionChar, 3)
	rightLen := tableWidth - len(leftDashes) - len(headerTitle) - len(sep)*2
	return leftDashes + sep + headerTitle + sep + strings.Repeat(sectionChar, rightLen)
}

// renderTable uses text/tabwriter to format the results rows with
// automatically aligned columns, returning the rendered string.
func renderTable(results []CheckResult) string {
	var buf bytes.Buffer
	// minwidth=0, tabwidth=8, padding=2, padchar=' ', flags=0
	w := tabwriter.NewWriter(&buf, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "COMPONENT\tTARGET\tSTATUS\tDETAILS")
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Component, r.Target, r.ProbeStatus.statusLabel(), r.Details)
	}
	_ = w.Flush()
	return buf.String()
}

// renderActions collects all non-empty Action strings from results and returns
// the formatted warning/error block that appears between the table and SUMMARY.
// A Fail result is rendered as ERROR when the report is a hard fail, and as
// WARNING otherwise (degraded but tunnel can still run).
func renderActions(r Report) string {
	hardFail := r.hasHardFail()
	var sb strings.Builder
	for _, res := range r.Results {
		if res.Action == "" || res.ProbeStatus != Fail {
			continue
		}
		if hardFail {
			_, _ = fmt.Fprintf(&sb, "ERROR: %s\n", res.Action)
		} else {
			_, _ = fmt.Fprintf(&sb, "WARNING: %s\n", res.Action)
		}
	}
	return sb.String()
}

// summaryLine builds the SUMMARY: line based on the Report state.
func summaryLine(r Report) string {
	switch {
	case r.hasHardFail():
		return "SUMMARY: Environment has critical failures. cloudflared may not be able to establish a tunnel."
	case r.hasWarn():
		return fmt.Sprintf("SUMMARY: Environment ready with degraded transport. cloudflared will proceed using '%s'.", r.SuggestedProtocol)
	default:
		return fmt.Sprintf("SUMMARY: Environment is healthy. cloudflared will use '%s' as primary protocol.", r.SuggestedProtocol)
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

// String renders the Report as a human-readable table suitable for os.Stdout.
func (r Report) String() string {
	var sb strings.Builder

	sb.WriteString(header())
	sb.WriteString("\n")

	sb.WriteString(renderTable(r.Results))

	actions := renderActions(r)
	if actions != "" {
		sb.WriteString(actions)
	}

	sb.WriteString("\n")
	sb.WriteString(summaryLine(r))
	sb.WriteString("\n")

	sb.WriteString(separator())
	sb.WriteString("\n")

	return sb.String()
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

	logger.Info().
		Str(logFieldRunID, runID).
		Bool(logFieldHardFail, r.hasHardFail()).
		Str(logFieldSuggestedProtocol, r.SuggestedProtocol.String()).
		Msg(logMsgPrecheckComplete)
}
