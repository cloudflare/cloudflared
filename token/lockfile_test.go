package token

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLockFileStale_DeadProcess(t *testing.T) {
	// write a lock file with a PID that cannot exist (e.g., max int32)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	content := lockContent{PID: 2147483647, StartTime: 1000000000000}
	data, err := json.Marshal(content)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))

	stale, _, err := isLockFileStale(path)
	require.NoError(t, err)
	assert.True(t, stale)
}

func TestIsLockFileStale_LiveProcess(t *testing.T) {
	// write a lock file with our own PID and start time
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	content, err := newSelfLockContent()
	require.NoError(t, err)
	data, err := json.Marshal(content)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))

	stale, readBack, err := isLockFileStale(path)
	require.NoError(t, err)
	assert.False(t, stale)
	assert.Equal(t, content.PID, readBack.PID)
	assert.Equal(t, content.StartTime, readBack.StartTime)
}

func TestIsLockFileStale_EmptyFile(t *testing.T) {
	// backward compat: old lock files are empty
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	require.NoError(t, os.WriteFile(path, []byte{}, 0600))

	stale, _, err := isLockFileStale(path)
	require.NoError(t, err)
	assert.True(t, stale)
}

func TestIsLockFileStale_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))

	stale, _, err := isLockFileStale(path)
	require.NoError(t, err)
	assert.True(t, stale)
}

func TestReadAuthURL_Exists(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	url := "https://example.com/cdn-cgi/access/cli?token=abc123"
	require.NoError(t, os.WriteFile(tokenPath+".url", []byte(url), 0600))

	assert.Equal(t, url, readAuthURL(tokenPath))
}

func TestReadAuthURL_NotExists(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")

	assert.Empty(t, readAuthURL(tokenPath))
}

func TestTryCreateLockFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	err := tryCreateLockFile(path)
	require.NoError(t, err)

	// verify the file contains valid JSON with our PID
	data, err := os.ReadFile(path) // nolint: gosec
	require.NoError(t, err)
	var content lockContent
	require.NoError(t, json.Unmarshal(data, &content))
	assert.Equal(t, int32(os.Getpid()), content.PID) // nolint: gosec
	assert.Positive(t, content.StartTime)
}

func TestTryCreateLockFile_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	require.NoError(t, tryCreateLockFile(path))

	// second create should fail with "already exists"
	err := tryCreateLockFile(path)
	require.Error(t, err)
	assert.True(t, os.IsExist(err))
}

func TestNewSelfLockContent(t *testing.T) {
	content, err := newSelfLockContent()
	require.NoError(t, err)
	assert.Equal(t, int32(os.Getpid()), content.PID) // nolint: gosec
	assert.Positive(t, content.StartTime)
}

func TestReleaseLockFileAllowsReacquire(t *testing.T) {
	// Regression test for the `access login` self-deadlock (issue #1692):
	// the token lock was held for the whole process lifetime, so a second
	// fetch in the same process (after an invalid token was removed) waited
	// forever on its own lock. After release, the same process can acquire
	// the lock again.
	log := zerolog.Nop()
	path := filepath.Join(t.TempDir(), "token")

	require.NoError(t, acquireLockFile(path, &log))
	releaseLockFile(path, &log)
	require.NoError(t, acquireLockFile(path, &log), "must be able to re-acquire after release")
	releaseLockFile(path, &log)
}

func TestReleaseLockFileDoesNotRemoveOtherProcessLock(t *testing.T) {
	// Releasing must not remove a lock owned by another process.
	log := zerolog.Nop()
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	lockPath := path + ".lock"

	other := lockContent{PID: 2147483647, StartTime: 1000000000000}
	data, err := json.Marshal(other)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, data, 0600))

	releaseLockFile(path, &log)
	_, err = os.Stat(lockPath)
	require.NoError(t, err, "another process's lock must not be removed")
}
