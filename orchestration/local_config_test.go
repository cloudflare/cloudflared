package orchestration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

func TestConvertLocalConfigToJSON(t *testing.T) {
	connectTimeout := config.CustomDuration{Duration: 30 * time.Second}
	tlsTimeout := config.CustomDuration{Duration: 10 * time.Second}

	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "example.com",
				Service:  "http://localhost:8080",
			},
			{
				Hostname: "*",
				Service:  "http://localhost:8081",
			},
		},
		WarpRouting: config.WarpRoutingConfig{
			ConnectTimeout: &connectTimeout,
		},
		OriginRequest: config.OriginRequestConfig{
			ConnectTimeout: &connectTimeout,
			TLSTimeout:     &tlsTimeout,
		},
	}

	jsonData, err := ConvertLocalConfigToJSON(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, jsonData)

	var remoteConfig ingress.RemoteConfig
	err = json.Unmarshal(jsonData, &remoteConfig)
	require.NoError(t, err)

	require.Len(t, remoteConfig.Ingress.Rules, 2)
	require.Equal(t, "example.com", remoteConfig.Ingress.Rules[0].Hostname)
	require.Equal(t, "*", remoteConfig.Ingress.Rules[1].Hostname)
}

func TestConvertLocalConfigToJSON_EmptyIngress(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress:  []config.UnvalidatedIngressRule{},
	}

	jsonData, err := ConvertLocalConfigToJSON(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, jsonData)

	var localJSON LocalConfigJSON
	err = json.Unmarshal(jsonData, &localJSON)
	require.NoError(t, err)
	require.Empty(t, localJSON.IngressRules)
}

func TestValidateLocalConfig_Valid(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "example.com",
				Service:  "http://localhost:8080",
			},
			{
				Service: "http_status:404",
			},
		},
	}

	err := ValidateLocalConfig(cfg)
	require.NoError(t, err)
}

func TestValidateLocalConfig_WildcardCatchAll(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "example.com",
				Service:  "http://localhost:8080",
			},
			{
				Hostname: "*",
				Service:  "http_status:404",
			},
		},
	}

	err := ValidateLocalConfig(cfg)
	require.NoError(t, err)
}

func TestValidateLocalConfig_MissingCatchAll(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "example.com",
				Service:  "http://localhost:8080",
			},
		},
	}

	err := ValidateLocalConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catch-all")
}

func TestValidateLocalConfig_EmptyIngress(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress:  []config.UnvalidatedIngressRule{},
	}

	err := ValidateLocalConfig(cfg)
	require.NoError(t, err)
}

func TestValidateLocalConfig_InvalidService(t *testing.T) {
	cfg := &config.Configuration{
		TunnelID: "test-tunnel-id",
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "example.com",
				Service:  "not-a-valid-url",
			},
		},
	}

	err := ValidateLocalConfig(cfg)
	require.Error(t, err)
}

func TestReadLocalConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	configContent := `
tunnel: test-tunnel-id
ingress:
  - hostname: example.com
    service: http://localhost:8080
  - service: http_status:404
warp-routing:
  connectTimeout: 5s
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	cfg, err := ReadLocalConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, "test-tunnel-id", cfg.TunnelID)
	require.Len(t, cfg.Ingress, 2)
	require.Equal(t, "example.com", cfg.Ingress[0].Hostname)
	require.NotNil(t, cfg.WarpRouting.ConnectTimeout)
	require.Equal(t, 5*time.Second, cfg.WarpRouting.ConnectTimeout.Duration)
}

func TestReadLocalConfig_FileNotFound(t *testing.T) {
	_, err := ReadLocalConfig("/nonexistent/path/config.yaml")
	require.Error(t, err)
}

func TestReadLocalConfig_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0o600)
	require.NoError(t, err)

	_, err = ReadLocalConfig(configPath)
	require.Error(t, err)
}
