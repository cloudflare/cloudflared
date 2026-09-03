package tunnel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
