package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

const ingressDataJSONArgvFlagName = "json"

var ingressDataJSONArgv = &cli.StringFlag{
	Name:    ingressDataJSONArgvFlagName,
	Aliases: []string{"j"},
	Usage:   `Accepts a config in the form of json (as a command line argument) rather than read from a file`,
	EnvVars: []string{"TUNNEL_INGRESS_VALIDATE_JSON"},
}

func buildIngressSubcommand() *cli.Command {
	return &cli.Command{
		Name:      "ingress",
		Category:  "Tunnel",
		Usage:     "Validate and test cloudflared tunnel's ingress configuration",
		UsageText: "cloudflared tunnel [--config FILEPATH] ingress COMMAND [arguments...]",
		Hidden:    true,
		Description: ` Cloudflared lets you route traffic from the internet to multiple different addresses on your
		origin. Multiple-origin routing is configured by a set of rules. Each rule matches traffic
		by its hostname or path, and routes it to an address. These rules are configured under the
		'ingress' key of your config.yaml, for example:

		ingress:
		  - hostname: www.example.com
		    service: https://localhost:8000
		  - hostname: *.example.xyz
		    path: /[a-zA-Z]+.html
		    service: https://localhost:8001
		  - hostname: *
		    service: https://localhost:8002

		To ensure cloudflared can route all incoming requests, the last rule must be a catch-all
		rule that matches all traffic. You can validate these rules with the 'ingress validate'
		command, and test which rule matches a particular URL with 'ingress rule <URL>'.

		Multiple-origin routing is incompatible with the --url flag.`,
		Subcommands: []*cli.Command{buildValidateIngressCommand(), buildTestURLCommand()},
	}
}

func buildValidateIngressCommand() *cli.Command {
	return &cli.Command{
		Name:        "validate",
		Action:      cliutil.ConfiguredActionWithWarnings(validateIngressCommand),
		Usage:       "Validate the ingress configuration ",
		UsageText:   "cloudflared tunnel [--config FILEPATH] ingress validate",
		Description: "Validates the configuration file, ensuring your ingress rules are OK.",
		Flags:       []cli.Flag{ingressDataJSONArgv},
	}
}

func buildTestURLCommand() *cli.Command {
	return &cli.Command{
		Name:      "rule",
		Action:    cliutil.ConfiguredAction(testURLCommand),
		Usage:     "Check which ingress rule matches a given request URL",
		UsageText: "cloudflared tunnel [--config FILEPATH] ingress rule URL",
		ArgsUsage: "URL",
		Description: "Check which ingress rule matches a given request URL. " +
			"Ingress rules match a request's hostname and path. Hostname is " +
			"optional and is either a full hostname like `www.example.com` or a " +
			"hostname with a `*` for its subdomains, e.g. `*.example.com`. Path " +
			"is optional and matches a regular expression, like `/[a-zA-Z0-9_]+.html`",
	}
}

// validateIngressCommand check the syntax of the ingress rules in the cloudflared config
func validateIngressCommand(c *cli.Context, warnings string) error {
	return validateIngress(c, warnings, config.GetConfiguration(), c.App.Reader)
}

// validateIngress calls validateIngressCommand with its sources of
// configuration (the process-wide config file settings and stdin) injected.
func validateIngress(c *cli.Context, warnings string, globalConf *config.Configuration, configStdin io.Reader) error {
	conf, err := getConfiguration(c, globalConf, configStdin)
	if err != nil {
		return err
	}

	if _, err := ingress.ParseIngress(conf); err != nil {
		return errors.Wrap(err, "Validation failed")
	}
	if c.IsSet("url") {
		return ingress.ErrURLIncompatibleWithIngress
	}
	if warnings != "" {
		fmt.Println("Warning: unused keys detected in your config file. Here is a list of unused keys:")
		fmt.Println(warnings)
		return nil
	}
	fmt.Println("OK")
	return nil
}

// Gets the cloudflared configuration from the correct source depending on CLI
// flags. This function will use:
//
// - A config provided via the CLI if invoked as "tunnel ingress validate --json <config literal>"
// - The passed in globalConf, if non-nil (this is present if cloudflared is invoked with "tunnel --config <file> subcommand")
// - A config provided via stdin otherwise
func getConfiguration(c *cli.Context, globalConf *config.Configuration, configStdin io.Reader) (*config.Configuration, error) {
	var conf *config.Configuration

	// Option 1) --json <config literal>
	if c.IsSet(ingressDataJSONArgvFlagName) {
		ingressJSON := c.String(ingressDataJSONArgvFlagName)
		fmt.Println("Validating rules from cmdline flag --json")
		err := json.Unmarshal([]byte(ingressJSON), &conf)
		return conf, err
	}

	// Option 2) Global configuration (Normally injected as the --config passed to "cloudflared tunnel")
	if globalConf.Source() != "" {
		fmt.Println("Validating rules from ", globalConf.Source())
		return globalConf, nil
	}

	// Option 3) Read from standard input
	fmt.Println("Configuration not provided via a file or CLI, reading config as JSON from standard input")
	ingressJSON, err := io.ReadAll(configStdin)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}

	err = json.Unmarshal(ingressJSON, &conf)
	return conf, err
}

// testURLCommand checks which ingress rule matches the given URL.
func testURLCommand(c *cli.Context) error {
	requestArg := c.Args().First()
	if requestArg == "" {
		return errors.New("cloudflared tunnel rule expects a single argument, the URL to test")
	}

	requestURL, err := url.Parse(requestArg)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL", requestArg)
	}
	if requestURL.Hostname() == "" && requestURL.Scheme == "" {
		return fmt.Errorf("%s doesn't have a hostname, consider adding a scheme", requestArg)
	}

	conf := config.GetConfiguration()
	fmt.Println("Using rules from", conf.Source())
	ing, err := ingress.ParseIngress(conf)
	if err != nil {
		return errors.Wrap(err, "Validation failed")
	}

	_, i := ing.FindMatchingRule(requestURL.Hostname(), requestURL.Path)
	fmt.Printf("Matched rule #%d\n", i)
	fmt.Println(ing.Rules[i].MultiLineString())
	return nil
}
