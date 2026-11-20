package access

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePidFile(t *testing.T) {
	log := zerolog.Nop()

	t.Run("writes current PID to file", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "test.pid")

		writePidFile(pidFile, &log)

		content, err := os.ReadFile(pidFile)
		require.NoError(t, err)

		pid, err := strconv.Atoi(string(content))
		require.NoError(t, err)
		assert.Equal(t, os.Getpid(), pid)
	})

	t.Run("handles invalid path gracefully", func(t *testing.T) {
		// Should not panic on a path that can't be created
		writePidFile("/nonexistent/directory/test.pid", &log)
	})
}

func TestRemovePidFile(t *testing.T) {
	log := zerolog.Nop()

	t.Run("removes existing pid file", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "test.pid")

		writePidFile(pidFile, &log)
		assert.FileExists(t, pidFile)

		removePidFile(pidFile, &log)
		assert.NoFileExists(t, pidFile)
	})

	t.Run("handles missing file gracefully", func(t *testing.T) {
		// Should not panic when removing a file that doesn't exist
		removePidFile("/tmp/nonexistent-cloudflared-test.pid", &log)
	})
}
