//go:build !windows

package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteBatchFileCRLF guards against a regression of
// https://github.com/cloudflare/cloudflared/issues/667: cmd.exe's parser is
// unreliable with GOTOs/labels in a script that has Unix line endings, so the
// rendered batch file must use CRLF throughout.
func TestWriteBatchFileCRLF(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "cloudflared.exe")
	newPath := targetPath + ".new"
	oldPath := targetPath + ".old"

	require.NoError(t, writeBatchFile(targetPath, newPath, oldPath))

	content, err := os.ReadFile(filepath.Join(dir, batchFileName))
	require.NoError(t, err)

	// No bare LF: every "\n" must be preceded by "\r".
	withoutCRLF := strings.ReplaceAll(string(content), "\r\n", "")
	require.NotContains(t, withoutCRLF, "\n", "batch file must use CRLF line endings, not bare LF")
	require.NotContains(t, withoutCRLF, "\r", "batch file must not contain a lone CR")
}

// TestWriteBatchFileContent guards against a regression of
// https://github.com/cloudflare/cloudflared/issues/667, where the update
// script reported success even though it silently failed to replace the
// binary. The script must:
//   - wait for the service to actually stop (via "net stop", not the
//     fire-and-forget "sc stop") before touching the binary that the service,
//     and this very update process, may still have open;
//   - check each rename for failure instead of unconditionally exiting 0; and
//   - restore the original binary if the update can't be completed, rather
//     than leaving the install without an executable.
func TestWriteBatchFileContent(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "cloudflared.exe")
	newPath := targetPath + ".new"
	oldPath := targetPath + ".old"

	require.NoError(t, writeBatchFile(targetPath, newPath, oldPath))

	raw, err := os.ReadFile(filepath.Join(dir, batchFileName))
	require.NoError(t, err)
	content := string(raw)

	require.Contains(t, content, "net stop cloudflared")
	require.NotContains(t, content, "sc stop cloudflared",
		"sc stop only requests a stop and returns immediately, racing the rename")

	require.Contains(t, content, `rename "`+targetPath+`" cloudflared.exe.old`)
	require.Contains(t, content, `rename "`+newPath+`" cloudflared.exe`)

	// Every rename must be checked, and a failure after the binary has
	// already been moved out of the way must restore it.
	require.Contains(t, content, "if errorlevel 1 goto fail")
	require.Contains(t, content, "if errorlevel 1 goto restore")
	require.Contains(t, content, ":restore")
	require.Contains(t, content, `rename "`+oldPath+`" cloudflared.exe`)

	// The script must clean up after itself instead of relying on the
	// (about to exit) Go process to remove it.
	require.Contains(t, content, `del "%~f0"`)
}
