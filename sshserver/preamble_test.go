package sshserver

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestHasPort(t *testing.T) {
	type testCase struct {
		input          string
		expectedOutput string
	}

	tests := []testCase{
		{"localhost", "localhost:22"},
		{"other.addr:22", "other.addr:22"},
		{"[2001:db8::1]:8080", "[2001:db8::1]:8080"},
		{"[::1]", "[::1]:22"},
		{"2001:0db8:3c4d:0015:0000:0000:1a2f:1234", "[2001:0db8:3c4d:0015:0000:0000:1a2f:1234]:22"},
		{"::1", "[::1]:22"},
	}

	for _, test := range tests {
		out, err := canonicalizeDest(test.input)
		require.Nil(t, err)
		assert.Equal(t, test.expectedOutput, out)
	}
}
