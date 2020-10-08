package tunnel

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"regexp"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"
)

var (
	errNoIngressRules      = errors.New("No ingress rules were specified in the config file")
	errLastRuleNotCatchAll = errors.New("The last ingress rule must match all hostnames (i.e. it must be missing, or must be \"*\")")
	errBadWildcard         = errors.New("Hostname patterns can have at most one wildcard character (\"*\") and it can only be used for subdomains, e.g. \"*.example.com\"")
	errNoIngressRulesMatch = errors.New("The URL didn't match any ingress rules")
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

func (r rule) String() string {
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

func (r rule) matches(requestURL *url.URL) bool {
	hostMatch := r.Hostname == "" || r.Hostname == "*" || matchHost(r.Hostname, requestURL.Hostname())
	pathMatch := r.Path == nil || r.Path.MatchString(requestURL.Path)
	return hostMatch && pathMatch
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
}

func (ing ingress) validate() ([]rule, error) {
	rules := make([]rule, len(ing.Ingress))
	for i, r := range ing.Ingress {
		service, err := url.Parse(r.Service)
		if err != nil {
			return nil, err
		}
		if service.Scheme == "" || service.Hostname() == "" {
			return nil, fmt.Errorf("The service %s must have a scheme and a hostname", r.Service)
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

func ingressContext(c *cli.Context) ([]rule, error) {
	configFilePath := c.String("config")
	if configFilePath == "" {
		return nil, config.ErrNoConfigFile
	}
	fmt.Printf("Reading from config file %s\n", configFilePath)
	configBytes, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}
	rules, err := parseIngress(configBytes)
	return rules, err
}

// Validates the ingress rules in the cloudflared config file
func validateCommand(c *cli.Context) error {
	_, err := ingressContext(c)
	if err != nil {
		fmt.Println(err.Error())
		return errors.New("Validation failed")
	}
	fmt.Println("OK")
	return nil
}

func buildValidateCommand() *cli.Command {
	return &cli.Command{
		Name:        "validate",
		Action:      cliutil.ErrorHandler(validateCommand),
		Usage:       "Validate the ingress configuration ",
		UsageText:   "cloudflared tunnel [--config FILEPATH] ingress validate",
		Description: "Validates the configuration file, ensuring your ingress rules are OK.",
	}
}

// Checks which ingress rule matches the given URL.
func ruleCommand(c *cli.Context) error {
	rules, err := ingressContext(c)
	if err != nil {
		return err
	}
	requestArg := c.Args().First()
	if requestArg == "" {
		return errors.New("cloudflared tunnel rule expects a single argument, the URL to test")
	}
	requestURL, err := url.Parse(requestArg)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL", requestArg)
	}
	if requestURL.Hostname() == "" {
		return fmt.Errorf("%s is malformed and doesn't have a hostname", requestArg)
	}
	for i, r := range rules {
		if r.matches(requestURL) {
			fmt.Printf("Matched rule #%d\n", i+1)
			fmt.Println(r.String())
			return nil
		}
	}
	return errNoIngressRulesMatch
}

func buildRuleCommand() *cli.Command {
	return &cli.Command{
		Name:      "rule",
		Action:    cliutil.ErrorHandler(ruleCommand),
		Usage:     "Check which ingress rule matches a given request URL",
		UsageText: "cloudflared [--config CONFIGFILE] tunnel ingress rule URL",
		ArgsUsage: "URL",
		Description: "Check which ingress rule matches a given request URL. " +
			"Ingress rules match a request's hostname and path. Hostname is " +
			"optional and is either a full hostname like `www.example.com` or a " +
			"hostname with a `*` for its subdomains, e.g. `*.example.com`. Path " +
			"is optional and matches a regular expression, like `/[a-zA-Z0-9_]+.html`",
	}
}
