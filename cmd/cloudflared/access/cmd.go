package access

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"golang.org/x/net/idna"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/sshgen"
	"github.com/cloudflare/cloudflared/token"
	"github.com/cloudflare/cloudflared/validation"
)

const (
	loginQuietFlag     = "quiet"
	sshHostnameFlag    = "hostname"
	sshDestinationFlag = "destination"
	sshURLFlag         = "url"
	sshHeaderFlag      = "header"
	sshTokenIDFlag     = "service-token-id"
	sshTokenSecretFlag = "service-token-secret"
	sshGenCertFlag     = "short-lived-cert"
	sshConnectTo       = "connect-to"
	sshDebugStream     = "debug-stream"
	sshConfigTemplate  = `
Add to your {{.Home}}/.ssh/config:

{{- if .ShortLivedCerts}}
Match host {{.Hostname}} exec "{{.Cloudflared}} access ssh-gen --hostname %h"
  ProxyCommand {{.Cloudflared}} access ssh --hostname %h
  IdentityFile ~/.cloudflared/%h-cf_key
  CertificateFile ~/.cloudflared/%h-cf_key-cert.pub
{{- else}}
Host {{.Hostname}}
  ProxyCommand {{.Cloudflared}} access ssh --hostname %h
{{end}}
`
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b@sentry.io/189878"

var (
	shutdownC chan struct{}
	userAgent = "DEV"
)

// Init will initialize and store vars from the main program
func Init(shutdown chan struct{}, version string) {
	shutdownC = shutdown
	userAgent = fmt.Sprintf("cloudflared/%s", version)
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
			Aliases:  []string{"forward"},
			Category: "Access",
			Usage:    "access <subcommand>",
			Description: `Cloudflare Access protects internal resources by securing, authenticating and monitoring access
			per-user and by application. With Cloudflare Access, only authenticated users with the required permissions are
			able to reach sensitive resources. The commands provided here allow you to interact with Access protected
			applications from the command line.`,
			Subcommands: []*cli.Command{
				{
					Name:   "login",
					Action: cliutil.Action(login),
					Usage:  "login <url of access application>",
					Description: `The login subcommand initiates an authentication flow with your identity provider.
					The subcommand will launch a browser. For headless systems, a url is provided.
					Once authenticated with your identity provider, the login command will generate a JSON Web Token (JWT)
					scoped to your identity, the application you intend to reach, and valid for a session duration set by your
					administrator. cloudflared stores the token in local storage.`,
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:    loginQuietFlag,
							Aliases: []string{"q"},
							Usage:   "do not print the jwt to the command line",
						},
					},
				},
				{
					Name:   "curl",
					Action: cliutil.Action(curl),
					Usage:  "curl [--allow-request, -ar] <url> [<curl args>...]",
					Description: `The curl subcommand wraps curl and automatically injects the JWT into a cf-access-token
					header when using curl to reach an application behind Access.`,
					ArgsUsage:       "allow-request will allow the curl request to continue even if the jwt is not present.",
					SkipFlagParsing: true,
				},
				{
					Name:        "token",
					Action:      cliutil.Action(generateToken),
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
					Name:        "tcp",
					Action:      cliutil.Action(ssh),
					Aliases:     []string{"rdp", "ssh", "smb"},
					Usage:       "",
					ArgsUsage:   "",
					Description: `The tcp subcommand sends data over a proxy to the Cloudflare edge.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:    sshHostnameFlag,
							Aliases: []string{"tunnel-host", "T"},
							Usage:   "specify the hostname of your application.",
							EnvVars: []string{"TUNNEL_SERVICE_HOSTNAME"},
						},
						&cli.StringFlag{
							Name:    sshDestinationFlag,
							Usage:   "specify the destination address of your SSH server.",
							EnvVars: []string{"TUNNEL_SERVICE_DESTINATION"},
						},
						&cli.StringFlag{
							Name:    sshURLFlag,
							Aliases: []string{"listener", "L"},
							Usage:   "specify the host:port to forward data to Cloudflare edge.",
							EnvVars: []string{"TUNNEL_SERVICE_URL"},
						},
						&cli.StringSliceFlag{
							Name:    sshHeaderFlag,
							Aliases: []string{"H"},
							Usage:   "specify additional headers you wish to send.",
						},
						&cli.StringFlag{
							Name:    sshTokenIDFlag,
							Aliases: []string{"id"},
							Usage:   "specify an Access service token ID you wish to use.",
							EnvVars: []string{"TUNNEL_SERVICE_TOKEN_ID"},
						},
						&cli.StringFlag{
							Name:    sshTokenSecretFlag,
							Aliases: []string{"secret"},
							Usage:   "specify an Access service token secret you wish to use.",
							EnvVars: []string{"TUNNEL_SERVICE_TOKEN_SECRET"},
						},
						&cli.StringFlag{
							Name:  logger.LogFileFlag,
							Usage: "Save application log to this file for reporting issues.",
						},
						&cli.StringFlag{
							Name:  logger.LogSSHDirectoryFlag,
							Usage: "Save application log to this directory for reporting issues.",
						},
						&cli.StringFlag{
							Name:    logger.LogSSHLevelFlag,
							Aliases: []string{"loglevel"}, //added to match the tunnel side
							Usage:   "Application logging level {debug, info, warn, error, fatal}. ",
						},
						&cli.StringFlag{
							Name:   sshConnectTo,
							Hidden: true,
							Usage:  "Connect to alternate location for testing, value is host, host:port, or sni:port:host",
						},
						&cli.Uint64Flag{
							Name:   sshDebugStream,
							Hidden: true,
							Usage:  "Writes up-to the max provided stream payloads to the logger as debug statements.",
						},
					},
				},
				{
					Name:        "ssh-config",
					Action:      cliutil.Action(sshConfig),
					Usage:       "",
					Description: `Prints an example configuration ~/.ssh/config`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:  sshHostnameFlag,
							Usage: "specify the hostname of your application.",
						},
						&cli.BoolFlag{
							Name:  sshGenCertFlag,
							Usage: "specify if you wish to generate short lived certs.",
						},
					},
				},
				{
					Name:        "ssh-gen",
					Action:      cliutil.Action(sshGen),
					Usage:       "",
					Description: `Generates a short lived certificate for given hostname`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:  sshHostnameFlag,
							Usage: "specify the hostname of your application.",
						},
					},
				},
			},
		},
	}
}

// login pops up the browser window to do the actual login and JWT generation
func login(c *cli.Context) error {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:     sentryDSN,
		Release: c.App.Version,
	})
	if err != nil {
		return err
	}

	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	args := c.Args()
	appURL, err := parseURL(args.First())
	if args.Len() < 1 || err != nil {
		log.Error().Msg("Please provide the url of the Access application")
		return err
	}

	appInfo, err := token.GetAppInfo(appURL)
	if err != nil {
		return err
	}

	if err := verifyTokenAtEdge(appURL, appInfo, c, log); err != nil {
		log.Err(err).Msg("Could not verify token")
		return err
	}

	cfdToken, err := token.GetAppTokenIfExists(appInfo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to find token for provided application.")
		return err
	} else if cfdToken == "" {
		fmt.Fprintln(os.Stderr, "token for provided application was empty.")
		return errors.New("empty application token")
	}

	if c.Bool(loginQuietFlag) {
		return nil
	}
	fmt.Fprintf(os.Stdout, "Successfully fetched your token:\n\n%s\n\n", cfdToken)

	return nil
}

// curl provides a wrapper around curl, passing Access JWT along in request
func curl(c *cli.Context) error {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:     sentryDSN,
		Release: c.App.Version,
	})
	if err != nil {
		return err
	}
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	args := c.Args()
	if args.Len() < 1 {
		log.Error().Msg("Please provide the access app and command you wish to run.")
		return errors.New("incorrect args")
	}

	cmdArgs, allowRequest := parseAllowRequest(args.Slice())
	appURL, err := getAppURL(cmdArgs, log)
	if err != nil {
		return err
	}

	appInfo, err := token.GetAppInfo(appURL)
	if err != nil {
		return err
	}

	// Verify that the existing token is still good; if not fetch a new one
	if err := verifyTokenAtEdge(appURL, appInfo, c, log); err != nil {
		log.Err(err).Msg("Could not verify token")
		return err
	}

	tok, err := token.GetAppTokenIfExists(appInfo)
	if err != nil || tok == "" {
		if allowRequest {
			log.Info().Msg("You don't have an Access token set. Please run access token <access application> to fetch one.")
			return run("curl", cmdArgs...)
		}
		tok, err = token.FetchToken(appURL, appInfo, log)
		if err != nil {
			log.Err(err).Msg("Failed to refresh token")
			return err
		}
	}

	cmdArgs = append(cmdArgs, "-H")
	cmdArgs = append(cmdArgs, fmt.Sprintf("%s: %s", carrier.CFAccessTokenHeader, tok))
	return run("curl", cmdArgs...)
}

// run kicks off a shell task and pipe the results to the respective std pipes
func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdin = os.Stdin
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}
	go func() {
		io.Copy(os.Stderr, stderr)
	}()

	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	go func() {
		io.Copy(os.Stdout, stdout)
	}()
	return c.Run()
}

// token dumps provided token to stdout
func generateToken(c *cli.Context) error {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:     sentryDSN,
		Release: c.App.Version,
	})
	if err != nil {
		return err
	}
	appURL, err := parseURL(c.String("app"))
	if err != nil || c.NumFlags() < 1 {
		fmt.Fprintln(os.Stderr, "Please provide a url.")
		return err
	}

	appInfo, err := token.GetAppInfo(appURL)
	if err != nil {
		return err
	}
	tok, err := token.GetAppTokenIfExists(appInfo)
	if err != nil || tok == "" {
		fmt.Fprintln(os.Stderr, "Unable to find token for provided application. Please run login command to generate token.")
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
	genCertBool := c.Bool(sshGenCertFlag)
	hostname := c.String(sshHostnameFlag)
	if hostname == "" {
		hostname = "[your hostname]"
	}

	type config struct {
		Home            string
		ShortLivedCerts bool
		Hostname        string
		Cloudflared     string
	}

	t := template.Must(template.New("sshConfig").Parse(sshConfigTemplate))
	return t.Execute(os.Stdout, config{Home: os.Getenv("HOME"), ShortLivedCerts: genCertBool, Hostname: hostname, Cloudflared: cloudflaredPath()})
}

// sshGen generates a short lived certificate for provided hostname
func sshGen(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	// get the hostname from the cmdline and error out if its not provided
	rawHostName := c.String(sshHostnameFlag)
	hostname, err := validation.ValidateHostname(rawHostName)
	if err != nil || rawHostName == "" {
		return cli.ShowCommandHelp(c, "ssh-gen")
	}

	originURL, err := parseURL(hostname)
	if err != nil {
		return err
	}

	// this fetchToken function mutates the appURL param. We should refactor that
	fetchTokenURL := &url.URL{}
	*fetchTokenURL = *originURL

	appInfo, err := token.GetAppInfo(fetchTokenURL)
	if err != nil {
		return err
	}
	cfdToken, err := token.FetchTokenWithRedirect(fetchTokenURL, appInfo, log)
	if err != nil {
		return err
	}

	if err := sshgen.GenerateShortLivedCertificate(originURL, cfdToken); err != nil {
		return err
	}

	return nil
}

// getAppURL will pull the request URL needed for fetching a user's Access token
func getAppURL(cmdArgs []string, log *zerolog.Logger) (*url.URL, error) {
	if len(cmdArgs) < 1 {
		log.Error().Msg("Please provide a valid URL as the first argument to curl.")
		return nil, errors.New("not a valid url")
	}

	u, err := processURL(cmdArgs[0])
	if err != nil {
		log.Error().Msg("Please provide a valid URL as the first argument to curl.")
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
	path, err := os.Executable()
	if err == nil && isFileThere(path) {
		return path
	}

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

// verifyTokenAtEdge checks for a token on disk, or generates a new one.
// Then makes a request to to the origin with the token to ensure it is valid.
// Returns nil if token is valid.
func verifyTokenAtEdge(appUrl *url.URL, appInfo *token.AppInfo, c *cli.Context, log *zerolog.Logger) error {
	headers := parseRequestHeaders(c.StringSlice(sshHeaderFlag))
	if c.IsSet(sshTokenIDFlag) {
		headers.Add(cfAccessClientIDHeader, c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Add(cfAccessClientSecretHeader, c.String(sshTokenSecretFlag))
	}
	options := &carrier.StartOptions{AppInfo: appInfo, OriginURL: appUrl.String(), Headers: headers}

	if valid, err := isTokenValid(options, log); err != nil {
		return err
	} else if valid {
		return nil
	}

	if err := token.RemoveTokenIfExists(appInfo); err != nil {
		return err
	}

	if valid, err := isTokenValid(options, log); err != nil {
		return err
	} else if !valid {
		return errors.New("failed to verify token")
	}

	return nil
}

// isTokenValid makes a request to the origin and returns true if the response was not a 302.
func isTokenValid(options *carrier.StartOptions, log *zerolog.Logger) (bool, error) {
	req, err := carrier.BuildAccessRequest(options, log)
	if err != nil {
		return false, errors.Wrap(err, "Could not create access request")
	}
	req.Header.Set("User-Agent", userAgent)

	query := req.URL.Query()
	query.Set("cloudflared_token_check", "true")
	req.URL.RawQuery = query.Encode()

	// Do not follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Second * 5,
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	// A redirect to login means the token was invalid.
	return !carrier.IsAccessResponse(resp), nil
}
