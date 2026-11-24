package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWixPathEnvValueHasNoTrailingDot ensures the Windows installer does not
// add a dot to the end of the PATH entry it creates for cloudflared.
func TestWixPathEnvValueHasNoTrailingDot(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../wix.json")
	require.NoError(t, err)

	var cfg struct {
		Env struct {
			Vars []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"vars"`
		} `json:"env"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg))

	var found bool
	for _, v := range cfg.Env.Vars {
		if v.Name != "CFDPATH" {
			continue
		}
		found = true
		assert.Equal(t, "[INSTALLDIR]", v.Value)
	}
	require.True(t, found, "CFDPATH environment variable not found in wix.json")
}
