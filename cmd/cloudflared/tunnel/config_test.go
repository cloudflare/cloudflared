package tunnel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDedup(t *testing.T) {
	expected := []string{"a", "b"}
	actual := dedup([]string{"a", "b", "a"})
	require.ElementsMatch(t, expected, actual)
}
