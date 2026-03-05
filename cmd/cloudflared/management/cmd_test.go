package management

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/cfapi"
)

func TestParseResource_ValidResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected cfapi.ManagementResource
	}{
		{"logs", cfapi.Logs},
		{"admin", cfapi.Admin},
		{"host_details", cfapi.HostDetails},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result, err := parseResource(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseResource_InvalidResource(t *testing.T) {
	t.Parallel()

	invalid := []string{"invalid", "LOGS", "Admin", "", "metrics", "host-details"}

	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := parseResource(input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be one of")
		})
	}
}

func TestCommandStructure(t *testing.T) {
	t.Parallel()

	cmd := Command()

	assert.Equal(t, "management", cmd.Name)
	assert.True(t, cmd.Hidden)
	assert.Len(t, cmd.Subcommands, 1)

	tokenCmd := cmd.Subcommands[0]
	assert.Equal(t, "token", tokenCmd.Name)
	assert.True(t, tokenCmd.Hidden)

	// Verify required flags exist
	var hasResourceFlag bool
	for _, flag := range tokenCmd.Flags {
		if flag.Names()[0] == "resource" {
			hasResourceFlag = true
			break
		}
	}
	assert.True(t, hasResourceFlag, "token command should have --resource flag")
}
