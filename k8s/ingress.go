package k8s

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/config"
)

// GenerateIngressRules converts a slice of discovered Kubernetes ServiceInfo
// into cloudflared-compatible UnvalidatedIngressRule entries. The caller is
// responsible for appending a catch-all rule.
func GenerateIngressRules(services []ServiceInfo, log *zerolog.Logger) []config.UnvalidatedIngressRule {
	rules := make([]config.UnvalidatedIngressRule, 0, len(services))

	for _, svc := range services {
		originURL := svc.OriginURL()
		rule := config.UnvalidatedIngressRule{
			Hostname: svc.Hostname,
			Service:  originURL,
			Path:     svc.Path,
		}

		// Apply per-service origin request overrides from annotations.
		if svc.NoTLSVerify {
			noTLS := true
			rule.OriginRequest.NoTLSVerify = &noTLS
		}
		if svc.OriginServerName != "" {
			rule.OriginRequest.OriginServerName = &svc.OriginServerName
		}

		log.Info().
			Str("service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)).
			Str("hostname", svc.Hostname).
			Str("origin", originURL).
			Msg("Generated ingress rule from Kubernetes service")

		rules = append(rules, rule)
	}

	return rules
}

// MergeWithExistingRules takes user-defined ingress rules and the auto-discovered
// Kubernetes rules and produces a combined set. Kubernetes-generated rules are
// prepended so that they take priority, but user-defined catch-all rules are
// always kept at the end.
func MergeWithExistingRules(
	existing []config.UnvalidatedIngressRule,
	k8sRules []config.UnvalidatedIngressRule,
) []config.UnvalidatedIngressRule {
	if len(k8sRules) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return k8sRules
	}

	// Separate the catch-all rule (last rule) from the rest.
	var catchAll *config.UnvalidatedIngressRule
	rest := existing
	if len(existing) > 0 {
		last := existing[len(existing)-1]
		if isCatchAll(last) {
			catchAll = &last
			rest = existing[:len(existing)-1]
		}
	}

	// Deduplicate: remove any K8s rule that duplicates an existing user rule.
	existingSet := make(map[string]struct{}, len(rest))
	for _, r := range rest {
		existingSet[r.Hostname+"#"+r.Path] = struct{}{}
	}

	merged := make([]config.UnvalidatedIngressRule, 0, len(rest)+len(k8sRules)+1)
	// User rules first (higher priority for user-specified).
	merged = append(merged, rest...)
	// Then K8s rules.
	for _, kr := range k8sRules {
		key := kr.Hostname + "#" + kr.Path
		if _, dup := existingSet[key]; !dup {
			merged = append(merged, kr)
		}
	}
	// Append catch-all.
	if catchAll != nil {
		merged = append(merged, *catchAll)
	} else {
		// Always ensure there's a catch-all rule.
		merged = append(merged, config.UnvalidatedIngressRule{
			Service: "http_status:503",
		})
	}

	return merged
}

func isCatchAll(r config.UnvalidatedIngressRule) bool {
	return (r.Hostname == "" || r.Hostname == "*") && r.Path == ""
}
