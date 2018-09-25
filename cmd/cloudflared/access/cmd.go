package access

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/shell"
	"golang.org/x/net/idna"

	"github.com/cloudflare/cloudflared/log"
	raven "github.com/getsentry/raven-go"
	cli "gopkg.in/urfave/cli.v2"
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b@sentry.io/189878"

// Flags return the global flags for Access related commands (hopefully none)
func Flags() []cli.Flag {
	return []cli.Flag{} // no flags yet.
}

// Commands returns all the Access related subcommands
func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:     "access",
			Category: "Access",
			Usage:    "access <subcommand>",
			Description: `Cloudflare Access protects internal resources by securing, authenticating and monitoring access 
			per-user and by application. With Cloudflare Access, only authenticated users with the required permissions are 
			able to reach sensitive resources. The commands provided here allow you to interact with Access protected 
			applications from the command line.`,
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
					Usage:  "curl <args>",
					Description: `The curl subcommand wraps curl and automatically injects the JWT into a cf-access-token
					header when using curl to reach an application behind Access.`,
					ArgsUsage:       "allow-request will allow the curl request to continue even if the jwt is not present.",
					SkipFlagParsing: true,
				},
				{
					Name:        "token",
					Action:      token,
					Usage:       "token -app=<url of access application>",
					ArgsUsage:   "url of Access application",
					Description: `The token subcommand produces a JWT which can be used to authenticate requests.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name: "app",
						},
					},
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
	if _, err := fetchToken(c, appURL); err != nil {
		logger.Errorf("Failed to fetch token: %s\n", err)
		return err
	}

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

	cmdArgs, appURL, allowRequest, err := buildCurlCmdArgs(args.Slice())
	if err != nil {
		return err
	}

	token, err := getTokenIfExists(appURL)
	if err != nil || token == "" {
		if allowRequest {
			logger.Warn("You don't have an Access token set. Please run access token <access application> to fetch one.")
			return shell.Run("curl", cmdArgs...)
		}
		token, err = fetchToken(c, appURL)
		if err != nil {
			logger.Error("Failed to refresh token: ", err)
			return err
		}
	}

	cmdArgs = append(cmdArgs, "-H")
	cmdArgs = append(cmdArgs, fmt.Sprintf("cf-access-token: %s", token))
	return shell.Run("curl", cmdArgs...)
}

// token dumps provided token to stdout
func token(c *cli.Context) error {
	raven.SetDSN(sentryDSN)
	appURL, err := url.Parse(c.String("app"))
	if err != nil || c.NumFlags() < 1 {
		fmt.Fprintln(os.Stderr, "Please provide a url.")
		return err
	}
	token, err := getTokenIfExists(appURL)
	if err != nil || token == "" {
		fmt.Fprintln(os.Stderr, "Unable to find token for provided application. Please run token command to generate token.")
		return err
	}

	if _, err := fmt.Fprint(os.Stdout, token); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to write token to stdout.")
		return err
	}
	return nil
}

// processURL will preprocess the string (parse to a url, convert to punycode, etc).
func processURL(s string) (*url.URL, error) {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, err
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

// buildCurlCmdArgs will build the curl cmd args
func buildCurlCmdArgs(cmdArgs []string) ([]string, *url.URL, bool, error) {
	allowRequest, iAllowRequest := false, 0
	var appURL *url.URL
	for i, arg := range cmdArgs {
		if arg == "-allow-request" || arg == "-ar" {
			iAllowRequest = i
			allowRequest = true
		}

		u, err := processURL(arg)
		if err == nil {
			appURL = u
			cmdArgs[i] = appURL.String()
		}
	}

	if appURL == nil {
		logger.Error("Please provide a valid URL.")
		return cmdArgs, appURL, allowRequest, errors.New("invalid url")
	}

	if allowRequest {
		// remove from cmdArgs
		cmdArgs[iAllowRequest] = cmdArgs[len(cmdArgs)-1]
		cmdArgs = cmdArgs[:len(cmdArgs)-1]
	}

	return cmdArgs, appURL, allowRequest, nil
}
