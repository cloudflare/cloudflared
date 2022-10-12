package orchestration

import (
	"encoding/json"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

type newRemoteConfig struct {
	ingress.RemoteConfig
	// Add more fields when we support other settings in tunnel orchestration
}

type newLocalConfig struct {
	RemoteConfig       ingress.RemoteConfig
	ConfigurationFlags map[string]string `json:"__configuration_flags,omitempty"`
}

// Config is the original config as read and parsed by cloudflared.
type Config struct {
	Ingress     *ingress.Ingress
	WarpRouting ingress.WarpRoutingConfig

	// Extra settings used to configure this instance but that are not eligible for remotely management
	// ie. (--protocol, --loglevel, ...)
	ConfigurationFlags map[string]string
}

func (rc *newLocalConfig) MarshalJSON() ([]byte, error) {
	var r = struct {
		ConfigurationFlags map[string]string `json:"__configuration_flags,omitempty"`
		ingress.RemoteConfigJSON
	}{
		ConfigurationFlags: rc.ConfigurationFlags,
		RemoteConfigJSON: ingress.RemoteConfigJSON{
			// UI doesn't support top level configs, so we reconcile to individual ingress configs.
			GlobalOriginRequest: nil,
			IngressRules:        convertToUnvalidatedIngressRules(rc.RemoteConfig.Ingress),
			WarpRouting:         rc.RemoteConfig.WarpRouting.RawConfig(),
		},
	}

	return json.Marshal(r)
}

func convertToUnvalidatedIngressRules(i ingress.Ingress) []config.UnvalidatedIngressRule {
	result := make([]config.UnvalidatedIngressRule, 0)
	for _, rule := range i.Rules {
		var path string
		if rule.Path != nil {
			path = rule.Path.String()
		}

		newRule := config.UnvalidatedIngressRule{
			Hostname:      rule.Hostname,
			Path:          path,
			Service:       rule.Service.String(),
			OriginRequest: ingress.ConvertToRawOriginConfig(rule.Config),
		}

		result = append(result, newRule)
	}

	return result
}
