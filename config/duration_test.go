package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomDurationJSON(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		json     string
		duration time.Duration
	}{
		{"0", 0},
		{"0.000000001", time.Nanosecond},
		{"0.5", 500 * time.Millisecond},
		{"1.001", 1001 * time.Millisecond},
		{"-0.5", -500 * time.Millisecond},
		{"1", time.Second},
		{"30", 30 * time.Second},
		{"3600", time.Hour},
	}
	for _, tc := range testCases {
		t.Run(tc.json, func(t *testing.T) {
			t.Parallel()
			var decoded CustomDuration
			err := json.Unmarshal([]byte(tc.json), &decoded)
			require.NoError(t, err)
			require.Equal(t, tc.duration, decoded.Duration)

			data, err := json.Marshal(CustomDuration{Duration: tc.duration})
			require.NoError(t, err)
			require.JSONEq(t, tc.json, string(data))
		})
	}
}

func TestCustomDurationRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`null`, `"1s"`, `1e100`, `-1e100`, `9223372037`, `-9223372037`} {
		t.Run(data, func(t *testing.T) {
			t.Parallel()
			var decoded CustomDuration
			err := json.Unmarshal([]byte(data), &decoded)
			require.Error(t, err)
		})
	}
}
