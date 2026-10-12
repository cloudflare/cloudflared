package cliutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePidFile_Success(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pidPath := filepath.Join(tempDir, "test.pid")
	logger := zerolog.Nop()

	err := WritePidFile(pidPath, &logger)
	require.NoError(t, err)

	content, err := os.ReadFile(pidPath)
	require.NoError(t, err)

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestWritePidFile_InvalidPath(t *testing.T) {
	t.Parallel()

	// An impossible path where a directory cannot be written into
	pidPath := filepath.Join("/nonexistent_dir_12345", "sub", "test.pid")
	logger := zerolog.Nop()

	err := WritePidFile(pidPath, &logger)
	require.Error(t, err)
}

func TestWritePidFile_HomedirExpansion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	pidPath := "~/test_homedir.pid"
	logger := zerolog.Nop()

	err := WritePidFile(pidPath, &logger)
	require.NoError(t, err)

	expectedPath := filepath.Join(tempHome, "test_homedir.pid")
	content, err := os.ReadFile(expectedPath)
	require.NoError(t, err)

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}
