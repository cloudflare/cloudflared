package ingress

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// safeRelativePath is the single place that decides what may be served. It is
// tested directly, because going through ServeHTTP cannot distinguish "the rule
// rejected this" from "the file simply does not exist on this platform".
func TestSafeRelativePath(t *testing.T) {
	always := []struct {
		rel  string
		want bool
	}{
		{"index.html", true},
		{"sub/ok.txt", true},
		{"a.txt", true},
		{"", false},
		{".", false},
		{"..", false},
		{"a/../b", false},
		{".env", false},
		{".git/config", false},
		{"sub/.hidden", false},
		{"a.txt.", false},
		{"a.txt ", false},
		{"sub/a.txt. ", false},
		{"a\\b", false},
		{"\\.env", false},
		{"sub\\ok.txt", false},
	}
	for _, c := range always {
		require.Equalf(t, c.want, safeRelativePath(c.rel), "safeRelativePath(%q)", c.rel)
	}
}

func TestSafeRelativePathReservedNames(t *testing.T) {
	reserved := []string{"NUL", "nul", "CON", "PRN", "AUX", "COM1", "com9", "LPT1", "NUL.txt", "con.log"}
	for _, name := range reserved {
		got := safeRelativePath(name)
		if runtime.GOOS == "windows" {
			require.Falsef(t, got, "reserved device name %q must be rejected on Windows", name)
		} else {
			require.Truef(t, got, "%q is an ordinary name outside Windows", name)
		}
	}
	// Names that merely start with a device name are fine on every platform.
	require.True(t, safeRelativePath("console.txt"))
	require.True(t, safeRelativePath("nullable.md"))
}
