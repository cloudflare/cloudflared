#!/usr/bin/env python
"""
Integration tests for cloudflared connectivity pre-checks (TUN-10391).

Scope
-----
These tests verify the end-to-end behavior of cloudflared pre-checks:
- that the human-readable table written to stdout has the correct structure
  and content,
- that structured JSON log lines are emitted with the expected fields, and
- that running the `diag` subcommand against a live tunnel instance produces a
  zip archive that contains prechecks.json.

They do NOT cover every failure mode of the precheck logic — those are owned
by the unit tests in prechecks/checker_test.go which use mock dialers.

At the integration level the only reliable way to induce specific failure modes
without real firewall intervention is:

  - --edge <unreachable>: StaticEdgeDNSResolver resolves the literal IP
    directly (DNS row = PASS), then both QUIC and HTTP/2 probes time out
    -> hard fail (both transports blocked).
    This does NOT exercise the DNS-failure -> transport-skip path.

DNS failure and Management API failure cannot be triggered via CLI flags alone;
they require network-level intervention outside the component-test harness.

stdout design
-------------
fmt.Println(report.String()) runs inside a goroutine that is started
concurrently with the tunnel.  We poll a --logfile for the "precheck complete"
sentinel before leaving the `with` block, ensuring the goroutine has finished.
We then call cfd.terminate().  After the `with` block exits, the process is
dead and all output has been captured by CloudflaredProcess's background reader
thread (stderr is merged into stdout).  We read the accumulated lines from
cfd.stdout_lines.
"""

import json
import os
import re
import subprocess
import time
import zipfile as zipfilemod

from constants import METRICS_PORT
from util import LOGGER, start_cloudflared, wait_tunnel_ready

# stdout table constants
TABLE_WIDTH = 80
HEADER_LINE = "--- CONNECTIVITY PRE-CHECKS " + "-" * (TABLE_WIDTH - len("--- CONNECTIVITY PRE-CHECKS ") - 1)
COL_HEADER  = "COMPONENT"       # first token of the column-header row
SEPARATOR   = "-" * TABLE_WIDTH

# Component names (probes.go: componentXxx)
COMP_DNS  = "DNS Resolution"
COMP_QUIC = "UDP Connectivity"
COMP_H2   = "TCP Connectivity"
COMP_API  = "Cloudflare API"

# Target labels used in the rendered table.
#
# probeRegion() (checker.go:216) always overwrites the Target field of
# whatever CheckResult the inner probe function returns with the regionTarget
# hostname, so QUIC and HTTP/2 rows carry the same region hostname as the
# corresponding DNS row — not the "Port 7844 (QUIC/HTTP2)" strings that
# targetPortQUIC/targetPortHTTP2 define.  Those port-label constants are only
# used in the empty-addrs SKIP branch and inside action message strings.
TARGET_API     = "api.cloudflare.com:443"
TARGET_REGION1 = "region1.v2.argotunnel.com"
TARGET_REGION2 = "region2.v2.argotunnel.com"

# Details strings (probes.go: detailsXxx)
DETAILS_DNS_RESOLVED          = "DNS Resolved successfully"
DETAILS_QUIC_OK               = "QUIC connection successful"
DETAILS_HTTP2_OK              = "HTTP/2 connection successful"
DETAILS_API_OK                = "API is reachable"
DETAILS_QUIC_FAIL             = "QUIC connection failed"
DETAILS_HTTP2_FAIL            = "HTTP/2 connection is blocked or unreachable"

# Status labels (result.go: xyzStatus)
PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"

# Action prefixes (result.go: renderActions)
PREFIX_ERROR   = "ERROR: "
PREFIX_WARNING = "WARNING: "

# Action messages (probes.go: actionXxx)
ACTION_QUIC_BLOCKED = "Allow outbound QUIC traffic on port 7844 or use HTTP2."
ACTION_HTTP2_BLOCKED = "Allow outbound TCP on port 7844."

# Exact summary lines (result.go: summaryLine)
SUMMARY_HEALTHY  = "SUMMARY: Environment is healthy. cloudflared will use 'quic' as primary protocol."
SUMMARY_CRITICAL = "SUMMARY: Environment has critical failures. cloudflared may not be able to establish a tunnel."

# structured log constants (result.go)

LOG_MSG_PRECHECK          = "precheck"
LOG_MSG_PRECHECK_COMPLETE = "precheck complete"
STATUS_PASS_LOG           = "pass"

UNREACHABLE_EDGE = "192.0.2.1:7844"

# cloudflared dial timeout per probe: 5 s, up to 2 retries -> ~15 s total.
PRECHECK_POLL_TIMEOUT_SECS  = 15
PRECHECK_POLL_INTERVAL_SECS = 1

# ---------- helpers ----------

def _poll_log_file_for_precheck_complete(log_file: str, timeout: float) -> list[dict]:
    """
    Poll a JSON log file until a 'precheck complete' line appears or timeout
    expires.  Returns all precheck-related log lines found.

    cloudflared's --logfile writes one JSON object per line.  Polling keeps
    the test fast on healthy networks and still tolerates slow CI hosts.

    We re-read from the beginning of the file on every poll because the file
    is append-only, small, and tracking a byte offset would add complexity with
    no meaningful performance benefit for a ~15 s total window.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        lines = _read_precheck_log_lines_from_file(log_file)
        if any(l.get("message") == LOG_MSG_PRECHECK_COMPLETE for l in lines):
            return lines
        time.sleep(PRECHECK_POLL_INTERVAL_SECS)
    return _read_precheck_log_lines_from_file(log_file)


def _read_precheck_log_lines_from_file(log_file: str) -> list[dict]:
    """Parse all precheck-related JSON log lines from a --logfile path."""
    result = []
    try:
        with open(log_file, "r") as f:
            for raw_line in f:
                raw_line = raw_line.strip()
                if not raw_line:
                    continue
                try:
                    obj = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                msg = obj.get("message") or obj.get("msg", "")
                if msg in (LOG_MSG_PRECHECK, LOG_MSG_PRECHECK_COMPLETE):
                    result.append(obj)
    except FileNotFoundError:
        pass
    return result


# stdout table parse
class TableRow:
    """One data row parsed from the rendered precheck table."""
    def __init__(self, component: str, target: str, status: str, details: str):
        self.component = component
        self.target    = target
        self.status    = status
        self.details   = details

    def __repr__(self):
        return f"TableRow({self.component!r}, {self.target!r}, {self.status!r}, {self.details!r})"


def _parse_table(stdout: str) -> list[TableRow]:
    """
    Parse the data rows from a precheck table in stdout.

    text/tabwriter uses padding=2, so columns are separated by two or more
    spaces.  We skip the column-header row and stop at blank lines, SUMMARY,
    separator, ERROR, or WARNING lines.
    """
    rows = []
    in_data = False
    for line in stdout.splitlines():
        if line.startswith("COMPONENT"):
            in_data = True
            continue
        if not in_data:
            continue
        if (line == "" or line.startswith("SUMMARY") or line.startswith("---")
                or line.startswith("ERROR") or line.startswith("WARNING")):
            in_data = False
            continue
        parts = re.split(r"  +", line.rstrip())
        if len(parts) >= 3:
            rows.append(TableRow(
                component=parts[0],
                target=parts[1],
                status=parts[2],
                details=parts[3] if len(parts) >= 4 else "",
            ))
    return rows


def _rows_for(rows: list[TableRow], component: str) -> list[TableRow]:
    return [r for r in rows if r.component == component]


# log assertions

def _assert_precheck_summary_log(
    log_lines: list[dict],
    *,
    hard_fail: bool,
    suggested_protocol: str | None = None,
):
    """Assert the 'precheck complete' summary log line has the expected fields."""
    summary_lines = [l for l in log_lines if l.get("message") == LOG_MSG_PRECHECK_COMPLETE]
    assert len(summary_lines) == 1, \
        f"Expected exactly one '{LOG_MSG_PRECHECK_COMPLETE}' log line; got {summary_lines}"
    summary = summary_lines[0]

    assert summary.get("hard_fail") is hard_fail, \
        f"Expected hard_fail={hard_fail} in summary log: {summary}"

    if suggested_protocol is not None:
        assert summary.get("suggested_protocol") == suggested_protocol, \
            (f"Expected suggested_protocol={suggested_protocol!r}; "
             f"got {summary.get('suggested_protocol')!r}")


# ---------- Tests ----------

class TestPrechecksHappyPath:
    """
    On a healthy connection all probes pass.  We assert:
      - the full table structure (header, column header, separator)
      - every row's component, target, status, and details
      - no ERROR/WARNING action lines
      - the exact summary line
      - the structured log summary (hard_fail=false, suggested_protocol=quic)
    """

    def test_prechecks_pass_on_healthy_connection(self, tmp_path, component_tests_config):
        log_file = str(tmp_path / "cloudflared.log")
        config = component_tests_config({"logfile": log_file})

        with start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=["tunnel", "--ha-connections", "1"],
            cfd_args=["run"],
            new_process=True,
            capture_output=True,
        ) as cfd:
            wait_tunnel_ready(tunnel_url=config.get_url(), require_min_connections=1)
            # Poll the log file for the sentinel before signalling the process.
            log_lines = _poll_log_file_for_precheck_complete(
                log_file, timeout=PRECHECK_POLL_TIMEOUT_SECS
            )
            # Signal shutdown.
            cfd.terminate()

        # The process is now dead.  All output was captured by the background
        # reader thread into cfd.stdout_lines (stderr is merged into stdout).
        stdout = b"".join(cfd.stdout_lines).decode(errors="replace")

        LOGGER.debug(f"[happy-path] stdout:\n{stdout}")
        LOGGER.debug(f"[happy-path] log_lines:\n{log_lines}")

        # ── table structure ──────────────────────────────────────────────────
        # stderr is merged into stdout so log lines precede the table.
        assert HEADER_LINE in stdout, \
            f"Expected header line in output;\ngot:\n{stdout}"
        assert COL_HEADER in stdout, \
            f"Expected column header row in output;\ngot:\n{stdout}"
        assert SEPARATOR in stdout, \
            f"Expected closing separator in output;\ngot:\n{stdout}"

        # ── row content ──────────────────────────────────────────────────────
        rows = _parse_table(stdout)
        assert len(rows) == 7, \
            f"Expected 7 rows (2 DNS + 2 QUIC + 2 HTTP/2 + 1 API); got {len(rows)}: {rows}"

        dns_rows = _rows_for(rows, COMP_DNS)
        assert len(dns_rows) == 2, f"Expected 2 DNS rows; got {dns_rows}"
        assert dns_rows[0].target == TARGET_REGION1
        assert dns_rows[1].target == TARGET_REGION2
        for r in dns_rows:
            assert r.status  == PASS,             f"DNS row not PASS: {r}"
            assert r.details == DETAILS_DNS_RESOLVED, f"DNS row details wrong: {r}"

        quic_rows = _rows_for(rows, COMP_QUIC)
        assert len(quic_rows) == 2, f"Expected 2 QUIC rows; got {quic_rows}"
        assert quic_rows[0].target == TARGET_REGION1, f"QUIC row[0] target wrong: {quic_rows[0]}"
        assert quic_rows[1].target == TARGET_REGION2, f"QUIC row[1] target wrong: {quic_rows[1]}"
        for r in quic_rows:
            assert r.status  == PASS,            f"QUIC row not PASS: {r}"
            assert r.details == DETAILS_QUIC_OK, f"QUIC row details wrong: {r}"

        h2_rows = _rows_for(rows, COMP_H2)
        assert len(h2_rows) == 2, f"Expected 2 HTTP/2 rows; got {h2_rows}"
        assert h2_rows[0].target == TARGET_REGION1, f"HTTP/2 row[0] target wrong: {h2_rows[0]}"
        assert h2_rows[1].target == TARGET_REGION2, f"HTTP/2 row[1] target wrong: {h2_rows[1]}"
        for r in h2_rows:
            assert r.status  == PASS,             f"HTTP/2 row not PASS: {r}"
            assert r.details == DETAILS_HTTP2_OK, f"HTTP/2 row details wrong: {r}"

        api_rows = _rows_for(rows, COMP_API)
        assert len(api_rows) == 1, f"Expected 1 API row; got {api_rows}"
        assert api_rows[0].target  == TARGET_API,    f"API row target wrong: {api_rows[0]}"
        assert api_rows[0].status  == PASS,          f"API row not PASS: {api_rows[0]}"
        assert api_rows[0].details == DETAILS_API_OK, f"API row details wrong: {api_rows[0]}"

        # ── no action lines ──────────────────────────────────────────────────
        assert PREFIX_ERROR   not in stdout, f"Unexpected ERROR action:\n{stdout}"
        assert PREFIX_WARNING not in stdout, f"Unexpected WARNING action:\n{stdout}"

        # ── exact summary line ───────────────────────────────────────────────
        assert SUMMARY_HEALTHY in stdout, \
            f"Expected healthy summary;\ngot:\n{stdout}"

        # ── structured log ───────────────────────────────────────────────────
        assert len(log_lines) > 0, \
            "Expected at least one structured precheck log line in log file"
        for line in log_lines:
            if line.get("message") == LOG_MSG_PRECHECK:
                assert line.get("status") == STATUS_PASS_LOG, \
                    f"Expected status=pass in precheck log line: {line}"
        _assert_precheck_summary_log(log_lines, hard_fail=False, suggested_protocol="quic")


class TestPrechecksHardFail:
    """
    When --edge points at an unreachable IP, StaticEdgeDNSResolver resolves
    the literal address directly (DNS row = PASS), but both transport probes
    time out -> hard fail.  We assert:
      - the full table structure
      - DNS row: PASS (the literal IP was resolved)
      - QUIC row: FAIL with correct details + ERROR action
      - HTTP/2 row: FAIL with correct details + ERROR action
      - API row: PASS (api.cloudflare.com:443 is independently reachable)
      - the exact critical summary line
      - the structured log summary (hard_fail=true)

    This test does NOT call wait_tunnel_ready because the tunnel will not
    connect to the unreachable address.
    """

    def test_prechecks_hard_fail_when_edge_unreachable(self, tmp_path, component_tests_config):
        log_file = str(tmp_path / "cloudflared.log")
        config = component_tests_config({"logfile": log_file})

        with start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=[
                "tunnel",
                "--ha-connections", "1",
                "--edge", UNREACHABLE_EDGE,
            ],
            cfd_args=["run"],
            new_process=True,
            capture_output=True,
        ) as cfd:
            log_lines = _poll_log_file_for_precheck_complete(
                log_file, timeout=PRECHECK_POLL_TIMEOUT_SECS
            )
            cfd.terminate()

        stdout = b"".join(cfd.stdout_lines).decode(errors="replace")

        LOGGER.debug(f"[hard-fail] stdout:\n{stdout}")
        LOGGER.debug(f"[hard-fail] log_lines:\n{log_lines}")

        # ── table structure ──────────────────────────────────────────────────
        # stderr is merged into stdout so log lines precede the table.
        assert HEADER_LINE in stdout, \
            f"Expected header line in output;\ngot:\n{stdout}"
        assert COL_HEADER in stdout, \
            f"Expected column header row in output;\ngot:\n{stdout}"
        assert SEPARATOR in stdout, \
            f"Expected closing separator in output;\ngot:\n{stdout}"

        # ── row content ──────────────────────────────────────────────────────
        rows = _parse_table(stdout)
        assert len(rows) == 4, \
            f"Expected 4 rows (1 DNS + 1 QUIC + 1 HTTP/2 + 1 API); got {len(rows)}: {rows}"

        dns_rows = _rows_for(rows, COMP_DNS)
        assert len(dns_rows) == 1, f"Expected 1 DNS row; got {dns_rows}"
        assert dns_rows[0].target  == UNREACHABLE_EDGE
        assert dns_rows[0].status  == PASS,                 f"DNS row not PASS: {dns_rows[0]}"
        assert dns_rows[0].details == DETAILS_DNS_RESOLVED, f"DNS row details wrong: {dns_rows[0]}"

        quic_rows = _rows_for(rows, COMP_QUIC)
        assert len(quic_rows) == 1, f"Expected 1 QUIC row; got {quic_rows}"
        assert quic_rows[0].target  == UNREACHABLE_EDGE,   f"QUIC row target wrong: {quic_rows[0]}"
        assert quic_rows[0].status  == FAIL,              f"QUIC row not FAIL: {quic_rows[0]}"
        assert quic_rows[0].details == DETAILS_QUIC_FAIL, f"QUIC row details wrong: {quic_rows[0]}"

        h2_rows = _rows_for(rows, COMP_H2)
        assert len(h2_rows) == 1, f"Expected 1 HTTP/2 row; got {h2_rows}"
        assert h2_rows[0].target  == UNREACHABLE_EDGE,     f"HTTP/2 row target wrong: {h2_rows[0]}"
        assert h2_rows[0].status  == FAIL,                f"HTTP/2 row not FAIL: {h2_rows[0]}"
        assert h2_rows[0].details == DETAILS_HTTP2_FAIL,  f"HTTP/2 row details wrong: {h2_rows[0]}"

        api_rows = _rows_for(rows, COMP_API)
        assert len(api_rows) == 1, f"Expected 1 API row; got {api_rows}"
        assert api_rows[0].target  == TARGET_API,     f"API row target wrong: {api_rows[0]}"
        assert api_rows[0].status  == PASS,           f"API row not PASS: {api_rows[0]}"
        assert api_rows[0].details == DETAILS_API_OK, f"API row details wrong: {api_rows[0]}"

        assert f"{PREFIX_ERROR}{ACTION_QUIC_BLOCKED}"  in stdout, \
            f"Expected QUIC ERROR action;\ngot:\n{stdout}"
        assert f"{PREFIX_ERROR}{ACTION_HTTP2_BLOCKED}" in stdout, \
            f"Expected HTTP/2 ERROR action;\ngot:\n{stdout}"

        assert SUMMARY_CRITICAL in stdout, \
            f"Expected critical summary;\ngot:\n{stdout}"

        _assert_precheck_summary_log(log_lines, hard_fail=True, suggested_protocol=None)


class TestPreChecksDiag:
    """
    Verify that `cloudflared tunnel diag` includes prechecks.json in the
    diagnostic zip archive produced against a live tunnel instance.

    The precheck job in diagnostic.go is gated on noDiagNetwork; we do NOT
    pass --no-diag-network so prechecks.json must be present.  We skip the
    heavier collectors (logs, metrics, system, runtime) to keep the test fast.

    The diag subcommand writes the zip to its current working directory.  We
    run it with cwd=tmp_path so the archive lands there and is cleaned up
    automatically by pytest.  We resolve config.cloudflared_binary to an
    absolute path before changing cwd, because the binary path may be relative
    to the original working directory.
    """

    def test_diag_contains_prechecks_json(self, tmp_path, component_tests_config):
        config = component_tests_config()
        binary = os.path.abspath(config.cloudflared_binary)

        with start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=["tunnel", "--ha-connections", "1"],
            cfd_args=["run"],
            new_process=True,
            capture_output=True,
        ) as cfd:
            wait_tunnel_ready(tunnel_url=config.get_url(), require_min_connections=1)

            # Run the diag subcommand as a one-shot process against the
            # already-running instance.  We skip log/metrics/system/runtime
            # collectors; the network collector (which runs prechecks) is left
            # enabled.
            diag_result = subprocess.run(
                [
                    binary,
                    "tunnel",
                    "diag",
                    "--metrics", f"localhost:{METRICS_PORT}",
                    "--no-diag-logs",
                    "--no-diag-metrics",
                    "--no-diag-system",
                    "--no-diag-runtime",
                ],
                cwd=str(tmp_path),
                capture_output=True,
                timeout=60,
            )

            cfd.terminate()

        diag_stdout = diag_result.stdout.decode(errors="replace")
        diag_stderr = diag_result.stderr.decode(errors="replace")
        LOGGER.debug(f"[diag] stdout:\n{diag_stdout}")
        LOGGER.debug(f"[diag] stderr:\n{diag_stderr}")

        assert diag_result.returncode == 0, (
            f"cloudflared tunnel diag exited with code {diag_result.returncode}\n"
            f"stdout:\n{diag_stdout}\nstderr:\n{diag_stderr}"
        )

        # Locate the zip file written to tmp_path by the diag command.
        zip_files = list(tmp_path.glob("cloudflared-diag-*.zip"))
        assert len(zip_files) == 1, \
            f"Expected exactly one cloudflared-diag-*.zip in {tmp_path}; found {zip_files}"

        zip_path = zip_files[0]
        with zipfilemod.ZipFile(zip_path) as zf:
            names = zf.namelist()
            LOGGER.debug(f"[diag] zip contents: {names}")

            assert "prechecks.json" in names, \
                f"Expected prechecks.json in diag zip; got: {names}"

            # Must be valid JSON containing at least the RunID field that
            # prechecks.Run() always sets.
            with zf.open("prechecks.json") as fh:
                data = json.load(fh)

        assert "RunID" in data, \
            f"Expected RunID key in prechecks.json; got keys: {list(data.keys())}"
