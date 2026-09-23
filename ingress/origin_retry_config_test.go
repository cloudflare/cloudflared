package ingress

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/config"
)

func TestOriginConnectRetryConfig(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var ing Ingress
			if format == "yaml" {
				var cfg config.Configuration
				err := yaml.Unmarshal([]byte(`
originRequest:
  connectRetryTimeout: 2s
ingress:
  - hostname: inherited.example.com
    service: unix:/tmp/app.sock
  - hostname: overridden.example.com
    service: http://localhost:8000
    originRequest:
      connectRetryTimeout: 500ms
  - service: unix+tls:/tmp/app.sock
    originRequest:
      connectRetryTimeout: 0s
`), &cfg)
				require.NoError(t, err)
				ing, err = ParseIngress(&cfg)
				require.NoError(t, err)
			} else {
				var cfg RemoteConfig
				err := json.Unmarshal([]byte(`{"originRequest":{"connectRetryTimeout":2},"ingress":[
{"hostname":"inherited.example.com","service":"unix:/tmp/app.sock"},
{"hostname":"overridden.example.com","service":"http://localhost:8000","originRequest":{"connectRetryTimeout":0.5}},
{"service":"unix+tls:/tmp/app.sock","originRequest":{"connectRetryTimeout":0}}]}`), &cfg)
				require.NoError(t, err)
				ing = cfg.Ingress
			}
			require.Equal(t, 2*time.Second, ing.Defaults.ConnectRetryTimeout.Duration)
			require.Len(t, ing.Rules, 3)
			for i, timeout := range []time.Duration{2 * time.Second, 500 * time.Millisecond, 0} {
				require.Equal(t, timeout, ing.Rules[i].Config.ConnectRetryTimeout.Duration)
			}
		})
	}
}

func TestOriginConnectRetryInvalidConfig(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		yaml string
		json string
	}{
		{
			name: "negative global timeout",
			yaml: "originRequest: {connectRetryTimeout: -1s}\ningress: [{service: 'unix:/tmp/app.sock'}]",
			json: `{"originRequest":{"connectRetryTimeout":-1},"ingress":[{"service":"unix:/tmp/app.sock"}]}`,
		},
		{
			name: "negative rule timeout",
			yaml: "ingress: [{service: 'unix:/tmp/app.sock', originRequest: {connectRetryTimeout: -1s}}]",
			json: `{"ingress":[{"service":"unix:/tmp/app.sock","originRequest":{"connectRetryTimeout":-1}}]}`,
		},
		{
			name: "override does not hide invalid global timeout",
			yaml: "originRequest: {connectRetryTimeout: -1s}\ningress: [{service: 'unix:/tmp/app.sock', originRequest: {connectRetryTimeout: 0s}}]",
			json: `{"originRequest":{"connectRetryTimeout":-1},"ingress":[{"service":"unix:/tmp/app.sock","originRequest":{"connectRetryTimeout":0}}]}`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Run("yaml", func(t *testing.T) {
				t.Parallel()
				var cfg config.Configuration
				err := yaml.Unmarshal([]byte(tc.yaml), &cfg)
				require.NoError(t, err)
				_, err = ParseIngress(&cfg)
				require.ErrorContains(t, err, "connectRetryTimeout must not be negative")
			})
			t.Run("json", func(t *testing.T) {
				t.Parallel()
				var cfg RemoteConfig
				err := json.Unmarshal([]byte(tc.json), &cfg)
				require.ErrorContains(t, err, "connectRetryTimeout must not be negative")
			})
		})
	}
}
