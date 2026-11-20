package diagnostic_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/diagnostic"
)

func TestPipeCommandOutputToFile(t *testing.T) {
	t.Parallel()

	const largeOutput = 160835
	tests := []struct {
		name        string
		stdoutBytes int
		stderrBytes int
		exitCode    int
	}{
		{name: "empty output"},
		{name: "small output on both streams", stdoutBytes: 13, stderrBytes: 17},
		{name: "large stdout only", stdoutBytes: largeOutput},
		{name: "large stderr only", stderrBytes: largeOutput},
		{name: "large output on both streams", stdoutBytes: largeOutput, stderrBytes: largeOutput},
		{name: "nonzero exit with large stderr", stdoutBytes: 13, stderrBytes: largeOutput, exitCode: 7},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output := createLogOutputFile(t)
			executable, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			// nolint: gosec // Only the current test binary and controlled test arguments are executed.
			command := exec.CommandContext(
				ctx,
				executable,
				"-test.run=^TestPipeCommandOutputToFileHelperProcess$",
				"--",
				strconv.Itoa(test.stdoutBytes),
				strconv.Itoa(test.stderrBytes),
				strconv.Itoa(test.exitCode),
			)
			command.Env = append(os.Environ(), "CLOUDFLARED_TEST_LOG_COLLECTOR=1")

			information, err := diagnostic.PipeCommandOutputToFile(command, output)
			require.NoError(t, ctx.Err(), "log collection must not block until the deadline")
			if test.exitCode != 0 {
				var exitError *exec.ExitError
				require.ErrorAs(t, err, &exitError)
				assert.Equal(t, test.exitCode, exitError.ExitCode())
				assert.Nil(t, information)
			} else {
				require.NoError(t, err)
				assert.Equal(t, diagnostic.NewLogInformation(output.Name(), true, false), information)
			}

			contents, err := os.ReadFile(output.Name())
			require.NoError(t, err)
			assert.Len(t, contents, test.stdoutBytes+test.stderrBytes)
			assert.Equal(t, test.stdoutBytes, bytes.Count(contents, []byte("o")))
			assert.Equal(t, test.stderrBytes, bytes.Count(contents, []byte("e")))
		})
	}
}

func TestPipeCommandOutputToFileStartFailure(t *testing.T) {
	t.Parallel()

	// nolint: gosec // The missing executable is inside a test-owned directory.
	command := exec.CommandContext(t.Context(), filepath.Join(t.TempDir(), "missing-command"))
	information, err := diagnostic.PipeCommandOutputToFile(command, createLogOutputFile(t))
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Nil(t, information)
}

func TestPipeCommandOutputToFileCanceledContext(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// nolint: gosec // Only the current test binary is executed.
	command := exec.CommandContext(ctx, executable)
	information, err := diagnostic.PipeCommandOutputToFile(command, createLogOutputFile(t))
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, information)
}

func createLogOutputFile(t *testing.T) *os.File {
	t.Helper()

	output, err := os.CreateTemp(t.TempDir(), "cloudflared-logs-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, output.Close())
	})
	return output
}

// Run the test binary as a log-producing command without requiring Docker or a shell.
func TestPipeCommandOutputToFileHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv("CLOUDFLARED_TEST_LOG_COLLECTOR") != "1" {
		return
	}

	require.Len(t, os.Args, 6)
	stdoutBytes, err := strconv.Atoi(os.Args[3])
	require.NoError(t, err)
	stderrBytes, err := strconv.Atoi(os.Args[4])
	require.NoError(t, err)
	exitCode, err := strconv.Atoi(os.Args[5])
	require.NoError(t, err)

	_, err = io.WriteString(os.Stderr, strings.Repeat("e", stderrBytes))
	require.NoError(t, err)
	_, err = io.WriteString(os.Stdout, strings.Repeat("o", stdoutBytes))
	require.NoError(t, err)
	os.Exit(exitCode)
}
