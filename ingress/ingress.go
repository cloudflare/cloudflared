package ingress

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var (
	ErrNoIngressRules             = errors.New("No ingress rules were specified in the config file")
	errLastRuleNotCatchAll        = errors.New("The last ingress rule must match all hostnames (i.e. it must be missing, or must be \"*\")")
	errBadWildcard                = errors.New("Hostname patterns can have at most one wildcard character (\"*\") and it can only be used for subdomains, e.g. \"*.example.com\"")
	errNoIngressRulesMatch        = errors.New("The URL didn't match any ingress rules")
	ErrURLIncompatibleWithIngress = errors.New("You can't set the --url flag (or $TUNNEL_URL) when using multiple-origin ingress rules")
)

// Each rule route traffic from a hostname/path on the public
// internet to the service running on the given URL.
type Rule struct {
	// Requests for this hostname will be proxied to this rule's service.
	Hostname string

	// Path is an optional regex that can specify path-driven ingress rules.
	Path *regexp.Regexp

	// A (probably local) address. Requests for a hostname which matches this
	// rule's hostname pattern will be proxied to the service running on this
	// address.
	Service *url.URL
}

func (r Rule) String() string {
	var out strings.Builder
	if r.Hostname != "" {
		out.WriteString("\thostname: ")
		out.WriteString(r.Hostname)
		out.WriteRune('\n')
	}
	if r.Path != nil {
		out.WriteString("\tpath: ")
		out.WriteString(r.Path.String())
		out.WriteRune('\n')
	}
	out.WriteString("\tservice: ")
	out.WriteString(r.Service.String())
	return out.String()
}

func (r *Rule) Matches(hostname, path string) bool {
	hostMatch := r.Hostname == "" || r.Hostname == "*" || matchHost(r.Hostname, hostname)
	pathMatch := r.Path == nil || r.Path.MatchString(path)
	return hostMatch && pathMatch
}

// FindMatchingRule returns the index of the Ingress Rule which matches the given
// hostname and path. This function assumes the last rule matches everything,
// which is the case if the rules were instantiated via the ingress#Validate method
func FindMatchingRule(hostname, path string, rules []Rule) int {
	for i, rule := range rules {
		if rule.Matches(hostname, path) {
			return i
		}
	}
	return len(rules) - 1
}

func matchHost(ruleHost, reqHost string) bool {
	if ruleHost == reqHost {
		return true
	}

	// Validate hostnames that use wildcards at the start
	if strings.HasPrefix(ruleHost, "*.") {
		toMatch := strings.TrimPrefix(ruleHost, "*.")
		return strings.HasSuffix(reqHost, toMatch)
	}
	return false
}

type unvalidatedRule struct {
	Hostname string
	Path     string
	Service  string
}

type ingress struct {
	Ingress []unvalidatedRule
	Url     string
}

func (ing ingress) validate() ([]Rule, error) {
	if ing.Url != "" {
		return nil, ErrURLIncompatibleWithIngress
	}
	rules := make([]Rule, len(ing.Ingress))
	for i, r := range ing.Ingress {
		service, err := url.Parse(r.Service)
		if err != nil {
			return nil, err
		}
		if service.Scheme == "" || service.Hostname() == "" {
			return nil, fmt.Errorf("The service %s must have a scheme and a hostname", r.Service)
		}

		if service.Path != "" {
			return nil, fmt.Errorf("%s is an invalid address, ingress rules don't support proxying to a different path on the origin service. The path will be the same as the eyeball request's path.", r.Service)
		}

		// Ensure that there are no wildcards anywhere except the first character
		// of the hostname.
		if strings.LastIndex(r.Hostname, "*") > 0 {
			return nil, errBadWildcard
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

		rules[i] = Rule{
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

func ParseIngress(rawYAML []byte) ([]Rule, error) {
	var ing ingress
	if err := yaml.Unmarshal(rawYAML, &ing); err != nil {
		return nil, err
	}
	if len(ing.Ingress) == 0 {
		return nil, ErrNoIngressRules
	}
	return ing.validate()
}

// RuleCommand checks which ingress rule matches the given request URL.
func RuleCommand(rules []Rule, requestURL *url.URL) error {
	if requestURL.Hostname() == "" {
		return fmt.Errorf("%s is malformed and doesn't have a hostname", requestURL)
	}
	i := FindMatchingRule(requestURL.Hostname(), requestURL.Path, rules)
	fmt.Printf("Matched rule #%d\n", i+1)
	fmt.Println(rules[i].String())
	return nil
}
