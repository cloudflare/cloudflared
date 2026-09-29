package access

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePidFile(t *testing.T) {
	log := zerolog.New(io.Discard)
	path := filepath.Join(t.TempDir(), "cloudflared.pid")

	writePidFile(path, &log)

	contents, err := os.ReadFile(path)
	require.NoError(t, err)

	pid, err := strconv.Atoi(string(contents))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestWritePidFileTruncatesExistingFile(t *testing.T) {
	log := zerolog.New(io.Discard)
	path := filepath.Join(t.TempDir(), "cloudflared.pid")

	// a stale file from a previous run must not leave trailing digits behind
	require.NoError(t, os.WriteFile(path, []byte("999999999999"), 0o600))

	writePidFile(path, &log)

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(contents))
}

func TestWritePidFileUnwritablePathDoesNotPanic(t *testing.T) {
	log := zerolog.New(io.Discard)
	// a directory that does not exist, so os.Create fails
	path := filepath.Join(t.TempDir(), "missing", "cloudflared.pid")

	assert.NotPanics(t, func() { writePidFile(path, &log) })
	assert.NoFileExists(t, path)
}
