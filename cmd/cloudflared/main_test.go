package main

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as the helper-process entry point: when
// GO_WANT_HELPER_PROCESS is set, the test binary re-executes main() so tests
// can assert on the process exit code, which plain unit tests cannot.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Regression test for https://github.com/cloudflare/cloudflared/issues/1606:
// an invalid TUNNEL_GRACE_PERIOD value used to make cloudflared exit 0 with no
// output, because runApp discarded the error from app.Run. It must now print
// the parse error and exit non-zero.
func TestRunAppInvalidGracePeriodEnvExitsNonZero(t *testing.T) {
	//nolint:gosec // G204: re-executes this test binary (os.Args[0]) with a fixed command line to assert the exit code
	cmd := exec.Command(os.Args[0], "tunnel", "run")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"TUNNEL_GRACE_PERIOD=10",
	)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "expected a non-zero exit code, got: %s", out)
	assert.NotZero(t, exitErr.ExitCode())
	assert.Contains(t, string(out), "grace-period")
}
