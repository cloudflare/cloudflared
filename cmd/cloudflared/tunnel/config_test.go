package tunnel

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/features"
)

func TestDedup(t *testing.T) {
	expected := []string{"a", "b"}
	actual := features.Dedup([]string{"a", "b", "a"})
	require.ElementsMatch(t, expected, actual)
}
