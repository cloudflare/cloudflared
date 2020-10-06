package tunnel

import (
	"fmt"
	"net/url"
	"regexp"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var (
	errNoIngressRules      = errors.New("No ingress rules were specified in the config file")
	errLastRuleNotCatchAll = errors.New("The last ingress rule must match all hostnames (i.e. it must be missing, or must be \"*\")")
)

// Each rule route traffic from a hostname/path on the public
// internet to the service running on the given URL.
type rule struct {
	// Requests for this hostname will be proxied to this rule's service.
	Hostname string

	// Path is an optional regex that can specify path-driven ingress rules.
	Path *regexp.Regexp

	// A (probably local) address. Requests for a hostname which matches this
	// rule's hostname pattern will be proxied to the service running on this
	// address.
	Service *url.URL
}

type unvalidatedRule struct {
	Hostname string
	Path     string
	Service  string
}

type ingress struct {
	Ingress []unvalidatedRule
}

func (ing ingress) validate() ([]rule, error) {
	rules := make([]rule, len(ing.Ingress))
	for i, r := range ing.Ingress {
		service, err := url.Parse(r.Service)
		if err != nil {
			return nil, err
		}

		// The last rule should catch all hostnames.
		isCatchAllRule := (r.Hostname == "" || r.Hostname == "*") && r.Path == ""
		isLastRule := i == len(ing.Ingress)-1
		if isLastRule && !isCatchAllRule {
			return nil, errLastRuleNotCatchAll
		}
		// ONLY the last rule should catch all hostnames.
		if !isLastRule && isCatchAllRule {
			return nil, errRuleShouldNotBeCatchAll{i: i, hostname: r.Hostname}
		}

		var pathRegex *regexp.Regexp
		if r.Path != "" {
			pathRegex, err = regexp.Compile(r.Path)
			if err != nil {
				return nil, errors.Wrapf(err, "Rule #%d has an invalid regex", i+1)
			}
		}

		rules[i] = rule{
			Hostname: r.Hostname,
			Service:  service,
			Path:     pathRegex,
		}
	}
	return rules, nil
}

type errRuleShouldNotBeCatchAll struct {
	i        int
	hostname string
}

func (e errRuleShouldNotBeCatchAll) Error() string {
	return fmt.Sprintf("Rule #%d is matching the hostname '%s', but "+
		"this will match every hostname, meaning the rules which follow it "+
		"will never be triggered.", e.i+1, e.hostname)
}

func parseIngress(rawYAML []byte) ([]rule, error) {
	var ing ingress
	if err := yaml.Unmarshal(rawYAML, &ing); err != nil {
		return nil, err
	}
	if len(ing.Ingress) == 0 {
		return nil, errNoIngressRules
	}
	return ing.validate()
}
