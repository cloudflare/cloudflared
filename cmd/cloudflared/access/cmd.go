package access

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/shell"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	"golang.org/x/net/idna"

	"github.com/cloudflare/cloudflared/log"
	raven "github.com/getsentry/raven-go"
	cli "gopkg.in/urfave/cli.v2"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b@sentry.io/189878"

var (
	logger         = log.CreateLogger()
	shutdownC      chan struct{}
	graceShutdownC chan struct{}
)

// Init will initialize and store vars from the main program
func Init(s, g chan struct{}) {
	shutdownC, graceShutdownC = s, g
}

// Flags return the global flags for Access related commands (hopefully none)
func Flags() []cli.Flag {
	return []cli.Flag{} // no flags yet.
}

// Commands returns all the Access related subcommands
func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:     "access",
			Category: "Access (BETA)",
			Usage:    "access <subcommand>",
			Description: `(BETA) Cloudflare Access protects internal resources by securing, authenticating and monitoring access 
			per-user and by application. With Cloudflare Access, only authenticated users with the required permissions are 
			able to reach sensitive resources. The commands provided here allow you to interact with Access protected 
			applications from the command line. This feature is considered beta. Your feedback is greatly appreciated!
			https://cfl.re/CLIAuthBeta`,
			Subcommands: []*cli.Command{
				{
					Name:   "login",
					Action: login,
					Usage:  "login <url of access application>",
					Description: `The login subcommand initiates an authentication flow with your identity provider.
					The subcommand will launch a browser. For headless systems, a url is provided.
					Once authenticated with your identity provider, the login command will generate a JSON Web Token (JWT)
					scoped to your identity, the application you intend to reach, and valid for a session duration set by your 
					administrator. cloudflared stores the token in local storage.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:   "url",
							Hidden: true,
						},
					},
				},
				{
					Name:   "curl",
					Action: curl,
					Usage:  "curl [--allow-request, -ar] <url> [<curl args>...]",
					Description: `The curl subcommand wraps curl and automatically injects the JWT into a cf-access-token
					header when using curl to reach an application behind Access.`,
					ArgsUsage:       "allow-request will allow the curl request to continue even if the jwt is not present.",
					SkipFlagParsing: true,
				},
				{
					Name:   "gitConfig",
					Action: gitConfig,
					Usage:  "gitConfig <repo_url>",
					Description: `The gitConfig subcommand configures your git to injects the JWT into a cf-access-token
					header when using git to reach an application behind Access.`,
					SkipFlagParsing: true,
				},
				{
					Name:        "token",
					Action:      generateToken,
					Usage:       "token -app=<url of access application>",
					ArgsUsage:   "url of Access application",
					Description: `The token subcommand produces a JWT which can be used to authenticate requests.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name: "app",
						},
					},
				},
				{
					Name:        "ssh",
					Action:      ssh,
					Aliases:     []string{"rdp"},
					Usage:       "",
					ArgsUsage:   "",
					Description: `The ssh subcommand sends data over a proxy to the Cloudflare edge.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:  "hostname",
							Usage: "specifics the hostname of your application.",
						},
						&cli.StringFlag{
							Name:  "url",
							Usage: "specifics the host:port to forward data to Cloudflare edge.",
						},
						&cli.StringSliceFlag{
							Name:    "header",
							Aliases: []string{"H"},
							Usage:   "specific additional headers you wish to send.",
						},
						&cli.StringSliceFlag{
							Name:    "service-token-id",
							Aliases: []string{"id"},
							Usage:   "specific an Access service token ID you wish to use.",
						},
						&cli.StringSliceFlag{
							Name:    "service-token-secret",
							Aliases: []string{"secret"},
							Usage:   "specific an Access service token secret you wish to use.",
						},
					},
				},
				{
					Name:        "ssh-config",
					Action:      sshConfig,
					Usage:       "ssh-config",
					Description: `Prints an example configuration ~/.ssh/config`,
				},
			},
		},
	}
}

// login pops up the browser window to do the actual login and JWT generation
func login(c *cli.Context) error {
	raven.SetDSN(sentryDSN)
	logger := log.CreateLogger()
	args := c.Args()
	appURL, err := url.Parse(args.First())
	if args.Len() < 1 || err != nil {
		logger.Errorf("Please provide the url of the Access application\n")
		return err
	}
	token, err := token.FetchToken(appURL)
	if err != nil {
		logger.Errorf("Failed to fetch token: %s\n", err)
		return err
	}
	fmt.Fprintf(os.Stdout, "Successfully fetched your token:\n\n%s\n\n", string(token))

	return nil
}

// curl provides a wrapper around curl, passing Access JWT along in request
func curl(c *cli.Context) error {
	raven.SetDSN(sentryDSN)
	logger := log.CreateLogger()
	args := c.Args()
	if args.Len() < 1 {
		logger.Error("Please provide the access app and command you wish to run.")
		return errors.New("incorrect args")
	}

	cmdArgs, allowRequest := parseAllowRequest(args.Slice())
	appURL, err := getAppURL(cmdArgs)
	if err != nil {
		return err
	}

	tok, err := token.GetTokenIfExists(appURL)
	if err != nil || tok == "" {
		if allowRequest {
			logger.Warn("You don't have an Access token set. Please run access token <access application> to fetch one.")
			return shell.Run("curl", cmdArgs...)
		}
		tok, err = token.FetchToken(appURL)
		if err != nil {
			logger.Error("Failed to refresh token: ", err)
			return err
		}
	}

	cmdArgs = append(cmdArgs, "-H")
	cmdArgs = append(cmdArgs, fmt.Sprintf("cf-access-token: %s", tok))
	return shell.Run("curl", cmdArgs...)
}

/* gitConfig modifies your ~/.gitconfig file to
pass Access JWT along in request
*/
func gitConfig(c *cli.Context) error {
	raven.SetDSN(sentryDSN)
	logger := log.CreateLogger()
	args := c.Args()
	if args.Len() < 1 {
		logger.Error("Please provide the access app and command you wish to run.")
		return errors.New("incorrect args")
	}

	cmdArgs := args.Slice()
	appURL, err := getAppURL(cmdArgs)
	if err != nil {
		return err
	}
	tok, err := token.GetTokenIfExists(appURL)
	if err != nil || tok == "" {
		tok, err = token.FetchToken(appURL)
		if err != nil {
			logger.Error("Failed to refresh token: ", err)
			return err
		}
	}

	gitArgs := []string{
		"config",
		"--global",
		fmt.Sprintf("http.https://%s.extraheader", appURL.Hostname()),
		fmt.Sprintf("cf-access-token:%s", tok),
	}
	return shell.Run("git", gitArgs...)
}

// token dumps provided token to stdout
func generateToken(c *cli.Context) error {
	raven.SetDSN(sentryDSN)
	appURL, err := url.Parse(c.String("app"))
	if err != nil || c.NumFlags() < 1 {
		fmt.Fprintln(os.Stderr, "Please provide a url.")
		return err
	}
	tok, err := token.GetTokenIfExists(appURL)
	if err != nil || tok == "" {
		fmt.Fprintln(os.Stderr, "Unable to find token for provided application. Please run token command to generate token.")
		return err
	}

	if _, err := fmt.Fprint(os.Stdout, tok); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to write token to stdout.")
		return err
	}
	return nil
}

// sshConfig prints an example SSH config to stdout
func sshConfig(c *cli.Context) error {
	outputMessage := "Add this configuration block to your %s/.ssh/config:\n\nHost [your hostname]\n\tProxyCommand %s access ssh --hostname %%h\n"
	logger.Printf(outputMessage, os.Getenv("HOME"), cloudflaredPath())
	return nil
}

// getAppURL will pull the appURL needed for fetching a user's Access token
func getAppURL(cmdArgs []string) (*url.URL, error) {
	if len(cmdArgs) < 1 {
		logger.Error("Please provide a valid URL as the first argument to curl.")
		return nil, errors.New("not a valid url")
	}

	u, err := processURL(cmdArgs[0])
	if err != nil {
		logger.Error("Please provide a valid URL as the first argument to curl.")
		return nil, err
	}

	return u, err
}

// parseAllowRequest will parse cmdArgs and return a copy of the args and result
// of the allow request was present
func parseAllowRequest(cmdArgs []string) ([]string, bool) {
	if len(cmdArgs) > 1 {
		if cmdArgs[0] == "--allow-request" || cmdArgs[0] == "-ar" {
			return cmdArgs[1:], true
		}
	}

	return cmdArgs, false
}

// processURL will preprocess the string (parse to a url, convert to punycode, etc).
func processURL(s string) (*url.URL, error) {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, err
	}

	if u.Host == "" {
		return nil, errors.New("not a valid host")
	}

	host, err := idna.ToASCII(u.Hostname())
	if err != nil { // we fail to convert to punycode, just return the url we parsed.
		return u, nil
	}
	if u.Port() != "" {
		u.Host = fmt.Sprintf("%s:%s", host, u.Port())
	} else {
		u.Host = host
	}

	return u, nil
}

// cloudflaredPath pulls the full path of cloudflared on disk
func cloudflaredPath() string {
	for _, p := range strings.Split(os.Getenv("PATH"), ":") {
		path := fmt.Sprintf("%s/%s", p, "cloudflared")
		if isFileThere(path) {
			return path
		}
	}
	return "cloudflared"
}

// isFileThere will check for the presence of candidate path
func isFileThere(candidate string) bool {
	fi, err := os.Stat(candidate)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() {
		return false
	}
	return true
}
