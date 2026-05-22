package prechecks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/connection"
)

// fixedRunID is used in all fixtures so golden strings are deterministic.
var fixedRunID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// Fixtures

// allPassReport is the all-checks-pass scenario: QUIC is the suggested protocol.
func allPassReport() Report {
	return Report{
		RunID:             fixedRunID,
		SuggestedProtocol: new(connection.QUIC),
		Results: []CheckResult{
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region1.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region2.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeQUIC, Component: "UDP Connectivity", Target: "Port 7844 (QUIC)", ProbeStatus: Pass, Details: detailsQUICHandshakeSuccessful},
			{Type: ProbeTypeHTTP2, Component: "TCP Connectivity", Target: "Port 7844 (HTTP/2)", ProbeStatus: Pass, Details: detailsHTTP2HandshakeSuccessful},
			{Type: ProbeTypeManagementAPI, Component: "Cloudflare API", Target: "api.cloudflare.com:443", ProbeStatus: Pass, Details: detailsApiReachable},
		},
	}
}

// quicBlockedReport is the degraded scenario: QUIC is blocked, HTTP/2 is the fallback.
// Only one transport failed so this is a warning, not a hard fail.
func quicBlockedReport() Report {
	return Report{
		RunID:             fixedRunID,
		SuggestedProtocol: new(connection.HTTP2),
		Results: []CheckResult{
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region1.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region2.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{
				Type:        ProbeTypeQUIC,
				Component:   "UDP Connectivity",
				Target:      "Port 7844 (QUIC)",
				ProbeStatus: Fail,
				Details:     detailsQUICHandshakeFailed,
				Action:      actionQUICBlocked,
			},
			{Type: ProbeTypeHTTP2, Component: "TCP Connectivity", Target: "Port 7844 (HTTP/2)", ProbeStatus: Pass, Details: detailsHTTP2HandshakeSuccessful},
			{Type: ProbeTypeManagementAPI, Component: "Cloudflare API", Target: "api.cloudflare.com:443", ProbeStatus: Pass, Details: detailsApiReachable},
		},
	}
}

// apiFailReport is the degraded scenario: all connectivity passes but the Cloudflare
// API is unreachable. The tunnel can still run; only automatic updates are unavailable.
func apiFailReport() Report {
	return Report{
		RunID:             fixedRunID,
		SuggestedProtocol: new(connection.QUIC),
		Results: []CheckResult{
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region1.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region2.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeQUIC, Component: "UDP Connectivity", Target: "Port 7844 (QUIC)", ProbeStatus: Pass, Details: detailsQUICHandshakeSuccessful},
			{Type: ProbeTypeHTTP2, Component: "TCP Connectivity", Target: "Port 7844 (HTTP/2)", ProbeStatus: Pass, Details: detailsHTTP2HandshakeSuccessful},
			{
				Type:        ProbeTypeManagementAPI,
				Component:   "Cloudflare API",
				Target:      "api.cloudflare.com:443",
				ProbeStatus: Fail,
				Details:     "Connection refused",
				Action:      "cloudflared will still run, but automatic software updates are unavailable. Ensure port 443 TCP to api.cloudflare.com is open if you want auto-updates.",
			},
		},
	}
}

// bothTransportsBlockedReport is the hard-fail scenario: both QUIC and HTTP/2 are blocked.
func bothTransportsBlockedReport() Report {
	return Report{
		RunID:             fixedRunID,
		SuggestedProtocol: nil,
		Results: []CheckResult{
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region1.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region2.v2.argotunnel.com", ProbeStatus: Pass, Details: dnsResolvedSuccessfully},
			{
				Type:        ProbeTypeQUIC,
				Component:   "UDP Connectivity",
				Target:      "Port 7844 (QUIC)",
				ProbeStatus: Fail,
				Details:     detailsQUICHandshakeFailed,
				Action:      "Allow outbound QUIC and/or TCP on port 7844 to the Cloudflare edge.",
			},
			{
				Type:        ProbeTypeHTTP2,
				Component:   "TCP Connectivity",
				Target:      "Port 7844 (HTTP/2)",
				ProbeStatus: Fail,
				Details:     detailsHTTP2BlockedOrUnreachable,
			},
			{Type: ProbeTypeManagementAPI, Component: "Cloudflare API", Target: "api.cloudflare.com:443", ProbeStatus: Pass, Details: detailsApiReachable},
		},
	}
}

// dnsFailReport is the hard-fail scenario: DNS is unresolvable, transports are skipped.
func dnsFailReport() Report {
	return Report{
		RunID:             fixedRunID,
		SuggestedProtocol: nil,
		Results: []CheckResult{
			{
				Type:        ProbeTypeDNS,
				Component:   "DNS Resolution",
				Target:      "region1.v2.argotunnel.com",
				ProbeStatus: Fail,
				Details:     "No addresses returned",
				Action:      "Ensure your DNS resolver can resolve 'region1.v2.argotunnel.com'. Run: dig A region1.v2.argotunnel.com @1.1.1.1. If that fails, contact your network administrator.",
			},
			{Type: ProbeTypeDNS, Component: "DNS Resolution", Target: "region2.v2.argotunnel.com", ProbeStatus: Fail, Details: "No addresses returned"},
			{Type: ProbeTypeQUIC, Component: "UDP Connectivity", Target: "Port 7844 (QUIC)", ProbeStatus: Skip, Details: "DNS prerequisite failed"},
			{Type: ProbeTypeHTTP2, Component: "TCP Connectivity", Target: "Port 7844 (HTTP/2)", ProbeStatus: Skip, Details: "DNS prerequisite failed"},
			{Type: ProbeTypeManagementAPI, Component: "Cloudflare API", Target: "api.cloudflare.com:443", ProbeStatus: Fail, Details: "Connection refused"},
		},
	}
}

// String() / table renderer tests

func TestString_AllPass(t *testing.T) {
	t.Parallel()
	want := "" +
		"--- CONNECTIVITY PRE-CHECKS ----------------------------------------------------\n" +
		"COMPONENT         TARGET                     STATUS  DETAILS\n" +
		"DNS Resolution    region1.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"DNS Resolution    region2.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"UDP Connectivity  Port 7844 (QUIC)           PASS    QUIC connection successful\n" +
		"TCP Connectivity  Port 7844 (HTTP/2)         PASS    HTTP/2 connection successful\n" +
		"Cloudflare API    api.cloudflare.com:443     PASS    API is reachable\n" +
		"\n" +
		"SUMMARY: Environment is healthy. cloudflared will use 'quic' as primary protocol.\n" +
		"--------------------------------------------------------------------------------\n"
	assert.Equal(t, want, allPassReport().String())
}

func TestString_QuicBlocked(t *testing.T) {
	t.Parallel()
	want := "" +
		"--- CONNECTIVITY PRE-CHECKS ----------------------------------------------------\n" +
		"COMPONENT         TARGET                     STATUS  DETAILS\n" +
		"DNS Resolution    region1.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"DNS Resolution    region2.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"UDP Connectivity  Port 7844 (QUIC)           FAIL    QUIC connection failed\n" +
		"TCP Connectivity  Port 7844 (HTTP/2)         PASS    HTTP/2 connection successful\n" +
		"Cloudflare API    api.cloudflare.com:443     PASS    API is reachable\n" +
		"WARNING: Allow outbound QUIC traffic on port 7844 or use HTTP2.\n" +
		"\n" +
		"SUMMARY: Environment ready with degraded transport. cloudflared will proceed using 'http2'.\n" +
		"--------------------------------------------------------------------------------\n"
	assert.Equal(t, want, quicBlockedReport().String())
}

func TestString_APIFail(t *testing.T) {
	t.Parallel()
	want := "" +
		"--- CONNECTIVITY PRE-CHECKS ----------------------------------------------------\n" +
		"COMPONENT         TARGET                     STATUS  DETAILS\n" +
		"DNS Resolution    region1.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"DNS Resolution    region2.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"UDP Connectivity  Port 7844 (QUIC)           PASS    QUIC connection successful\n" +
		"TCP Connectivity  Port 7844 (HTTP/2)         PASS    HTTP/2 connection successful\n" +
		"Cloudflare API    api.cloudflare.com:443     FAIL    Connection refused\n" +
		"WARNING: cloudflared will still run, but automatic software updates are unavailable. Ensure port 443 TCP to api.cloudflare.com is open if you want auto-updates.\n" +
		"\n" +
		"SUMMARY: Environment ready with degraded transport. cloudflared will proceed using 'quic'.\n" +
		"--------------------------------------------------------------------------------\n"
	assert.Equal(t, want, apiFailReport().String())
}

func TestString_BothTransportsBlocked(t *testing.T) {
	t.Parallel()
	want := "" +
		"--- CONNECTIVITY PRE-CHECKS ----------------------------------------------------\n" +
		"COMPONENT         TARGET                     STATUS  DETAILS\n" +
		"DNS Resolution    region1.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"DNS Resolution    region2.v2.argotunnel.com  PASS    DNS Resolved successfully\n" +
		"UDP Connectivity  Port 7844 (QUIC)           FAIL    QUIC connection failed\n" +
		"TCP Connectivity  Port 7844 (HTTP/2)         FAIL    HTTP/2 connection is blocked or unreachable\n" +
		"Cloudflare API    api.cloudflare.com:443     PASS    API is reachable\n" +
		"ERROR: Allow outbound QUIC and/or TCP on port 7844 to the Cloudflare edge.\n" +
		"\n" +
		"SUMMARY: Environment has critical failures. cloudflared may not be able to establish a tunnel.\n" +
		"--------------------------------------------------------------------------------\n"
	assert.Equal(t, want, bothTransportsBlockedReport().String())
}

func TestString_DNSFail(t *testing.T) {
	t.Parallel()
	want := "" +
		"--- CONNECTIVITY PRE-CHECKS ----------------------------------------------------\n" +
		"COMPONENT         TARGET                     STATUS  DETAILS\n" +
		"DNS Resolution    region1.v2.argotunnel.com  FAIL    No addresses returned\n" +
		"DNS Resolution    region2.v2.argotunnel.com  FAIL    No addresses returned\n" +
		"UDP Connectivity  Port 7844 (QUIC)           SKIP    DNS prerequisite failed\n" +
		"TCP Connectivity  Port 7844 (HTTP/2)         SKIP    DNS prerequisite failed\n" +
		"Cloudflare API    api.cloudflare.com:443     FAIL    Connection refused\n" +
		"ERROR: Ensure your DNS resolver can resolve 'region1.v2.argotunnel.com'. Run: dig A region1.v2.argotunnel.com @1.1.1.1. If that fails, contact your network administrator.\n" +
		"\n" +
		"SUMMARY: Environment has critical failures. cloudflared may not be able to establish a tunnel.\n" +
		"--------------------------------------------------------------------------------\n"
	assert.Equal(t, want, dnsFailReport().String())
}

func TestString_EmptyResults(t *testing.T) {
	t.Parallel()
	r := Report{RunID: fixedRunID, SuggestedProtocol: new(connection.QUIC)}
	out := r.String()
	// Must not panic and must still emit a valid skeleton.
	assert.Contains(t, out, "CONNECTIVITY PRE-CHECKS")
	assert.Contains(t, out, "SUMMARY:")
	assert.Contains(t, out, separator())
}

// LogEvent() / structured log renderer tests

// logEntry is a helper struct to unmarshal a single JSON log line emitted by LogEvent.
type logEntry struct {
	Level             string  `json:"level"`
	RunID             string  `json:"run_id"`
	Component         string  `json:"component"`
	Target            string  `json:"target"`
	Status            string  `json:"status"`
	Details           string  `json:"details"`
	Message           string  `json:"message"`
	HardFail          *bool   `json:"hard_fail"`
	SuggestedProtocol *string `json:"suggested_protocol"`
}

// captureLogLines runs LogEvent against a buffer-backed zerolog logger and
// returns the parsed JSON entries, one per emitted line.
func captureLogLines(t *testing.T, r Report) []logEntry {
	t.Helper()
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	r.LogEvent(&logger)

	var entries []logEntry
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e logEntry
		require.NoError(t, json.Unmarshal([]byte(line), &e), "failed to parse log line: %s", line)
		entries = append(entries, e)
	}
	return entries
}

func TestLogEvent_AllPass(t *testing.T) {
	t.Parallel()
	r := allPassReport()
	entries := captureLogLines(t, r)

	// One line per result plus one summary line.
	require.Len(t, entries, len(r.Results)+1)

	// Each result line carries the right fields.
	expected := []struct {
		component string
		target    string
		status    string
		details   string
	}{
		{"DNS Resolution", "region1.v2.argotunnel.com", "pass", dnsResolvedSuccessfully},
		{"DNS Resolution", "region2.v2.argotunnel.com", "pass", dnsResolvedSuccessfully},
		{"UDP Connectivity", "Port 7844 (QUIC)", "pass", detailsQUICHandshakeSuccessful},
		{"TCP Connectivity", "Port 7844 (HTTP/2)", "pass", detailsHTTP2HandshakeSuccessful},
		{"Cloudflare API", "api.cloudflare.com:443", "pass", detailsApiReachable},
	}
	for i, exp := range expected {
		e := entries[i]
		assert.Equal(t, "info", e.Level, "entry %d: level", i)
		assert.Equal(t, fixedRunID.String(), e.RunID, "entry %d: run_id", i)
		assert.Equal(t, exp.component, e.Component, "entry %d: component", i)
		assert.Equal(t, exp.target, e.Target, "entry %d: target", i)
		assert.Equal(t, exp.status, e.Status, "entry %d: status", i)
		assert.Equal(t, exp.details, e.Details, "entry %d: details", i)
		assert.Equal(t, logMsgPrecheck, e.Message, "entry %d: message", i)
	}

	// Summary line.
	summary := entries[len(entries)-1]
	assert.Equal(t, logMsgPrecheckComplete, summary.Message)
	assert.Equal(t, fixedRunID.String(), summary.RunID)
	require.NotNil(t, summary.HardFail)
	assert.False(t, *summary.HardFail)
	require.NotNil(t, summary.SuggestedProtocol)
	assert.Equal(t, "quic", *summary.SuggestedProtocol)
}

func TestLogEvent_QuicBlocked(t *testing.T) {
	t.Parallel()
	entries := captureLogLines(t, quicBlockedReport())

	// QUIC row (index 2) must carry status=fail and the right details.
	quic := entries[2]
	assert.Equal(t, "fail", quic.Status)
	assert.Equal(t, "UDP Connectivity", quic.Component)
	assert.Equal(t, "Port 7844 (QUIC)", quic.Target)
	assert.Equal(t, "QUIC connection failed", quic.Details)
	assert.Equal(t, fixedRunID.String(), quic.RunID)

	// Summary: not a hard fail (HTTP/2 still works), protocol falls back to http2.
	summary := entries[len(entries)-1]
	require.NotNil(t, summary.HardFail)
	assert.False(t, *summary.HardFail)
	require.NotNil(t, summary.SuggestedProtocol)
	assert.Equal(t, "http2", *summary.SuggestedProtocol)
	assert.Equal(t, fixedRunID.String(), summary.RunID)
}

func TestLogEvent_APIFail(t *testing.T) {
	t.Parallel()
	entries := captureLogLines(t, apiFailReport())

	// API row (index 4) carries status=fail and the expected details.
	api := entries[4]
	assert.Equal(t, "fail", api.Status)
	assert.Equal(t, "Cloudflare API", api.Component)
	assert.Equal(t, "api.cloudflare.com:443", api.Target)
	assert.Equal(t, "Connection refused", api.Details)
	assert.Equal(t, fixedRunID.String(), api.RunID)

	// All transport rows pass.
	assert.Equal(t, "pass", entries[2].Status)
	assert.Equal(t, "pass", entries[3].Status)

	// Summary: not a hard fail, QUIC is still the suggested protocol.
	summary := entries[len(entries)-1]
	require.NotNil(t, summary.HardFail)
	assert.False(t, *summary.HardFail)
	require.NotNil(t, summary.SuggestedProtocol)
	assert.Equal(t, "quic", *summary.SuggestedProtocol)
}

func TestLogEvent_BothTransportsBlocked(t *testing.T) {
	t.Parallel()
	entries := captureLogLines(t, bothTransportsBlockedReport())

	// Both transport rows carry status=fail.
	assert.Equal(t, "fail", entries[2].Status)
	assert.Equal(t, "QUIC connection failed", entries[2].Details)
	assert.Equal(t, "fail", entries[3].Status)
	assert.Equal(t, "HTTP/2 connection is blocked or unreachable", entries[3].Details)

	summary := entries[len(entries)-1]
	require.NotNil(t, summary.HardFail)
	assert.True(t, *summary.HardFail)
	assert.Nil(t, summary.SuggestedProtocol)
}

func TestLogEvent_DNSFail(t *testing.T) {
	t.Parallel()
	entries := captureLogLines(t, dnsFailReport())

	// Both DNS rows carry status=fail and the expected details.
	assert.Equal(t, "fail", entries[0].Status)
	assert.Equal(t, "No addresses returned", entries[0].Details)
	assert.Equal(t, "fail", entries[1].Status)
	assert.Equal(t, "No addresses returned", entries[1].Details)

	// Transport rows are skipped.
	assert.Equal(t, "skip", entries[2].Status)
	assert.Equal(t, "DNS prerequisite failed", entries[2].Details)
	assert.Equal(t, "skip", entries[3].Status)
	assert.Equal(t, "DNS prerequisite failed", entries[3].Details)

	summary := entries[len(entries)-1]
	require.NotNil(t, summary.HardFail)
	assert.True(t, *summary.HardFail)
	assert.Nil(t, summary.SuggestedProtocol)
}

func TestLogEvent_EmptyReport(t *testing.T) {
	t.Parallel()
	r := Report{RunID: fixedRunID, SuggestedProtocol: new(connection.HTTP2)}
	entries := captureLogLines(t, r)

	// No result lines, only the summary.
	require.Len(t, entries, 1)
	assert.Equal(t, logMsgPrecheckComplete, entries[0].Message)
	assert.Equal(t, fixedRunID.String(), entries[0].RunID)
	require.NotNil(t, entries[0].HardFail)
	assert.False(t, *entries[0].HardFail)
	require.NotNil(t, entries[0].SuggestedProtocol)
	assert.Equal(t, "http2", *entries[0].SuggestedProtocol)
}

// hasHardFail / hasWarn helper tests

func TestHasHardFail(t *testing.T) {
	t.Parallel()
	assert.False(t, allPassReport().hasHardFail())
	assert.False(t, quicBlockedReport().hasHardFail())
	assert.False(t, apiFailReport().hasHardFail())
	assert.True(t, bothTransportsBlockedReport().hasHardFail())
	assert.True(t, dnsFailReport().hasHardFail())
}

func TestHasWarn(t *testing.T) {
	t.Parallel()
	assert.False(t, allPassReport().hasWarn())
	assert.True(t, quicBlockedReport().hasWarn())
	assert.True(t, apiFailReport().hasWarn())
	assert.False(t, bothTransportsBlockedReport().hasWarn())
	assert.False(t, dnsFailReport().hasWarn())
}
