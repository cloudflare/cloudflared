package orchestration

import (
	"encoding/json"
	"os"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

// LocalConfigJSON represents the JSON format expected by Orchestrator.UpdateConfig.
// It mirrors ingress.RemoteConfigJSON structure.
type LocalConfigJSON struct {
	GlobalOriginRequest *config.OriginRequestConfig     `json:"originRequest,omitempty"`
	IngressRules        []config.UnvalidatedIngressRule `json:"ingress"`
	WarpRouting         config.WarpRoutingConfig        `json:"warp-routing"`
}

// ReadLocalConfig reads and parses the local YAML configuration file.
func ReadLocalConfig(configPath string) (*config.Configuration, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open config file %s", configPath)
	}
	defer file.Close()

	var cfg config.Configuration
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to parse YAML config file %s", configPath)
	}

	return &cfg, nil
}

// ConvertLocalConfigToJSON converts local YAML configuration to JSON format
// expected by Orchestrator.UpdateConfig.
func ConvertLocalConfigToJSON(cfg *config.Configuration) ([]byte, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}

	localJSON := LocalConfigJSON{
		GlobalOriginRequest: &cfg.OriginRequest,
		IngressRules:        cfg.Ingress,
		WarpRouting:         cfg.WarpRouting,
	}

	data, err := json.Marshal(localJSON)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal config to JSON")
	}

	return data, nil
}

// ValidateLocalConfig validates the local configuration by attempting to parse
// ingress rules. Returns nil if valid.
func ValidateLocalConfig(cfg *config.Configuration) error {
	_, err := ConvertAndValidateLocalConfig(cfg)
	return err
}

// ConvertAndValidateLocalConfig converts local config to JSON and validates it
// in a single pass. Returns JSON bytes if valid, error otherwise.
func ConvertAndValidateLocalConfig(cfg *config.Configuration) ([]byte, error) {
	data, err := ConvertLocalConfigToJSON(cfg)
	if err != nil {
		return nil, err
	}

	// Skip validation if no ingress rules
	if len(cfg.Ingress) == 0 {
		return data, nil
	}

	// Validate catch-all rule exists (last rule must have empty hostname or "*")
	lastRule := cfg.Ingress[len(cfg.Ingress)-1]
	if lastRule.Hostname != "" && lastRule.Hostname != "*" {
		return nil, errors.New("ingress rules must end with a catch-all rule (empty hostname or '*')")
	}

	// Validate by attempting to parse as RemoteConfig
	var remoteConfig ingress.RemoteConfig
	if err := json.Unmarshal(data, &remoteConfig); err != nil {
		return nil, errors.Wrap(err, "invalid ingress configuration")
	}

	return data, nil
}
