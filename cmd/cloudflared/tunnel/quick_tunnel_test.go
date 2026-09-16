package tunnel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"rsc.io/qr"
)

func TestQuickTunnelURLDisplayLinesNormalizeURL(t *testing.T) {
	t.Parallel()

	lines := quickTunnelURLDisplayLines("example.trycloudflare.com")

	require.Len(t, lines, 2)
	assert.Equal(t, "Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):", lines[0])
	assert.Equal(t, "https://example.trycloudflare.com", lines[1])
}

func TestQuickTunnelURLDisplayLinesPreserveHTTPSURL(t *testing.T) {
	t.Parallel()

	lines := quickTunnelURLDisplayLines("https://example.trycloudflare.com")

	require.Len(t, lines, 2)
	assert.Equal(t, "https://example.trycloudflare.com", lines[1])
}

func TestQuickTunnelQRCodeLinesUseCompactTerminalBlocks(t *testing.T) {
	t.Parallel()

	lines, err := quickTunnelQRCodeLines("example.trycloudflare.com")

	require.NoError(t, err)
	require.NotEmpty(t, lines)
	qrOutput := strings.Join(lines, "\n")
	assert.Contains(t, qrOutput, "▀")
	assert.Contains(t, qrOutput, "▄")
	assert.NotContains(t, qrOutput, "https://example.trycloudflare.com")
}

func TestQuickTunnelQRCodeLinesKeepScanQuietZone(t *testing.T) {
	t.Parallel()

	lines, err := quickTunnelQRCodeLines("example.trycloudflare.com")

	require.NoError(t, err)
	require.Greater(t, len(lines), 4)
	assert.Empty(t, strings.TrimSpace(lines[0]))
	assert.Empty(t, strings.TrimSpace(lines[1]))
	assert.Empty(t, strings.TrimSpace(lines[len(lines)-2]))
	assert.Empty(t, strings.TrimSpace(lines[len(lines)-1]))
	assert.NotEmpty(t, strings.TrimSpace(lines[2]))
	for _, line := range lines[2 : len(lines)-2] {
		assert.True(t, strings.HasPrefix(line, "    "))
	}
}

func TestQuickTunnelQRCodeLinesReturnsErrorForURLTooLong(t *testing.T) {
	t.Parallel()

	// A URL longer than the largest QR version can encode.
	longURL := strings.Repeat("a", 10000)

	_, err := quickTunnelQRCodeLines(longURL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create quick Tunnel QR code")
}

func TestRenderHalfBlockQRCodeMatchesSourceBitmap(t *testing.T) {
	t.Parallel()

	url := "https://example.trycloudflare.com"
	code, err := qr.Encode(url, qr.L)
	require.NoError(t, err)

	quietZone := 2
	lines := renderHalfBlockQRCode(code, quietZone)
	require.NotEmpty(t, lines)

	// Reconstruct a per-module bitmap from the half-block terminal output
	// and compare it to the original QR code.
	for row, line := range lines {
		yTop := row*2 - quietZone
		yBottom := yTop + 1
		col := 0
		for _, r := range line {
			x := col - quietZone
			switch r {
			case '█':
				assert.True(t, code.Black(x, yTop), "expected black at (%d,%d)", x, yTop)
				assert.True(t, code.Black(x, yBottom), "expected black at (%d,%d)", x, yBottom)
			case '▀':
				assert.True(t, code.Black(x, yTop), "expected black at (%d,%d)", x, yTop)
				assert.False(t, code.Black(x, yBottom), "expected white at (%d,%d)", x, yBottom)
			case '▄':
				assert.False(t, code.Black(x, yTop), "expected white at (%d,%d)", x, yTop)
				assert.True(t, code.Black(x, yBottom), "expected black at (%d,%d)", x, yBottom)
			case ' ':
				assert.False(t, code.Black(x, yTop), "expected white at (%d,%d)", x, yTop)
				assert.False(t, code.Black(x, yBottom), "expected white at (%d,%d)", x, yBottom)
			default:
				t.Fatalf("unexpected rune %q at row %d col %d", r, row, col)
			}
			col++
		}
	}
}

func TestBuildQuickTunnelRequestBody_PublicMode(t *testing.T) {
	t.Parallel()

	body, err := buildQuickTunnelRequestBody(false)
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestBuildQuickTunnelRequestBody_ProtectedMode(t *testing.T) {
	t.Parallel()

	body, err := buildQuickTunnelRequestBody(true)
	require.NoError(t, err)

	var result map[string]string
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, quickTunnelAuthModeOTP, result[quickTunnelAuthModeField])
}

func TestFormatQuickTunnelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		errors   []QuickTunnelError
		expected string
	}{
		{
			name:     "empty",
			errors:   []QuickTunnelError{},
			expected: "",
		},
		{
			name:     "single error",
			errors:   []QuickTunnelError{{Code: 1, Message: "first error"}},
			expected: "[1] first error",
		},
		{
			name: "multiple errors",
			errors: []QuickTunnelError{
				{Code: 1, Message: "first error"},
				{Code: 2, Message: "second error"},
			},
			expected: "[1] first error; [2] second error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatQuickTunnelErrors(tt.errors))
		})
	}
}
