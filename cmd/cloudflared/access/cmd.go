package access

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/shell"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/sshgen"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/pkg/errors"
	"golang.org/x/net/idna"

	"github.com/getsentry/raven-go"
	"gopkg.in/urfave/cli.v2"
)

const (
	sshHostnameFlag     = "hostname"
	sshDestinationFlag  = "destination"
	sshURLFlag          = "url"
	sshHeaderFlag       = "header"
	sshTokenIDFlag      = "service-token-id"
	sshTokenSecretFlag  = "service-token-secret"
	sshGenCertFlag      = "short-lived-cert"
	sshLogDirectoryFlag = "log-directory"
	sshLogLevelFlag     = "log-level"
	sshConfigTemplate   = `
Add to your {{.Home}}/.ssh/config:

Host {{.Hostname}}
{{- if .ShortLivedCerts}}
  ProxyCommand bash -c '{{.Cloudflared}} access ssh-gen --hostname %h; ssh -tt %r@cfpipe-{{.Hostname}} >&2 <&1' 

Host cfpipe-{{.Hostname}}
  HostName {{.Hostname}}
  ProxyCommand {{.Cloudflared}} access ssh --hostname %h
  IdentityFile ~/.cloudflared/{{.Hostname}}-cf_key
  CertificateFile ~/.cloudflared/{{.Hostname}}-cf_key-cert.pub
{{- else}}
  ProxyCommand {{.Cloudflared}} access ssh --hostname %h
{{end}}
`
)

const sentryDSN = "https://56a9c9fa5c364ab28f34b14f35ea0f1b@sentry.io/189878"

var (
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
					Action: cliutil.ErrorHandler(login),
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
					Action: cliutil.ErrorHandler(curl),
					Usage:  "curl [--allow-request, -ar] <url> [<curl args>...]",
					Description: `The curl subcommand wraps curl and automatically injects the JWT into a cf-access-token
					header when using curl to reach an application behind Access.`,
					ArgsUsage:       "allow-request will allow the curl request to continue even if the jwt is not present.",
					SkipFlagParsing: true,
				},
				{
					Name:        "token",
					Action:      cliutil.ErrorHandler(generateToken),
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
					Action:      cliutil.ErrorHandler(ssh),
					Aliases:     []string{"rdp", "ssh", "smb"},
					Usage:       "",
					ArgsUsage:   "",
					Description: `The tcp subcommand sends data over a proxy to the Cloudflare edge.`,
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:    sshHostnameFlag,
							Aliases: []string{"tunnel-host", "T"},
							Usage:   "specify the hostname of your application.",
						},
						&cli.StringFlag{
							Name:  sshDestinationFlag,
							Usage: "specify the destination address of your SSH server.",
						},
						&cli.StringFlag{
							Name:    sshURLFlag,
							Aliases: []string{"listener", "L"},
							Usage:   "specify the host:port to forward data to Cloudflare edge.",
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
						},
						&cli.StringFlag{
							Name:    sshTokenSecretFlag,
							Aliases: []string{"secret"},
							Usage:   "specify an Access service token secret you wish to use.",
						},
						&cli.StringFlag{
							Name:    sshLogDirectoryFlag,
							Aliases: []string{"logfile"}, //added to match the tunnel side
							Usage:   "Save application log to this directory for reporting issues.",
						},
						&cli.StringFlag{
							Name:    sshLogLevelFlag,
							Aliases: []string{"loglevel"}, //added to match the tunnel side
							Usage:   "Application logging level {fatal, error, info, debug}. ",
						},
					},
				},
				{
					Name:        "ssh-config",
					Action:      cliutil.ErrorHandler(sshConfig),
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
					Action:      cliutil.ErrorHandler(sshGen),
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
	if err := raven.SetDSN(sentryDSN); err != nil {
		return err
	}

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	args := c.Args()
	rawURL := ensureURLScheme(args.First())
	appURL, err := url.Parse(rawURL)
	if args.Len() < 1 || err != nil {
		logger.Errorf("Please provide the url of the Access application\n")
		return err
	}
	if err := verifyTokenAtEdge(appURL, c, logger); err != nil {
		logger.Errorf("Could not verify token: %s", err)
		return err
	}

	cfdToken, err := token.GetTokenIfExists(appURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to find token for provided application.")
		return err
	} else if cfdToken == "" {
		fmt.Fprintln(os.Stderr, "token for provided application was empty.")
		return errors.New("empty application token")
	}
	fmt.Fprintf(os.Stdout, "Successfully fetched your token:\n\n%s\n\n", cfdToken)

	return nil
}

// ensureURLScheme prepends a URL with https:// if it doesnt have a scheme. http:// URLs will not be converted.
func ensureURLScheme(url string) string {
	url = strings.Replace(strings.ToLower(url), "http://", "https://", 1)
	if !strings.HasPrefix(url, "https://") {
		url = fmt.Sprintf("https://%s", url)

	}
	return url
}

// curl provides a wrapper around curl, passing Access JWT along in request
func curl(c *cli.Context) error {
	if err := raven.SetDSN(sentryDSN); err != nil {
		return err
	}
	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	args := c.Args()
	if args.Len() < 1 {
		logger.Error("Please provide the access app and command you wish to run.")
		return errors.New("incorrect args")
	}

	cmdArgs, allowRequest := parseAllowRequest(args.Slice())
	appURL, err := getAppURL(cmdArgs, logger)
	if err != nil {
		return err
	}

	tok, err := token.GetTokenIfExists(appURL)
	if err != nil || tok == "" {
		if allowRequest {
			logger.Info("You don't have an Access token set. Please run access token <access application> to fetch one.")
			return shell.Run("curl", cmdArgs...)
		}
		tok, err = token.FetchToken(appURL, logger)
		if err != nil {
			logger.Errorf("Failed to refresh token: %s", err)
			return err
		}
	}

	cmdArgs = append(cmdArgs, "-H")
	cmdArgs = append(cmdArgs, fmt.Sprintf("%s: %s", h2mux.CFAccessTokenHeader, tok))
	return shell.Run("curl", cmdArgs...)
}

// token dumps provided token to stdout
func generateToken(c *cli.Context) error {
	if err := raven.SetDSN(sentryDSN); err != nil {
		return err
	}
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
	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	// get the hostname from the cmdline and error out if its not provided
	rawHostName := c.String(sshHostnameFlag)
	hostname, err := validation.ValidateHostname(rawHostName)
	if err != nil || rawHostName == "" {
		return cli.ShowCommandHelp(c, "ssh-gen")
	}

	originURL, err := url.Parse(ensureURLScheme(hostname))
	if err != nil {
		return err
	}

	// this fetchToken function mutates the appURL param. We should refactor that
	fetchTokenURL := &url.URL{}
	*fetchTokenURL = *originURL
	cfdToken, err := token.FetchTokenWithRedirect(fetchTokenURL, logger)
	if err != nil {
		return err
	}

	if err := sshgen.GenerateShortLivedCertificate(originURL, cfdToken); err != nil {
		return err
	}

	return nil
}

// getAppURL will pull the appURL needed for fetching a user's Access token
func getAppURL(cmdArgs []string, logger logger.Service) (*url.URL, error) {
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

// verifyTokenAtEdge checks for a token on disk, or generates a new one.
// Then makes a request to to the origin with the token to ensure it is valid.
// Returns nil if token is valid.
func verifyTokenAtEdge(appUrl *url.URL, c *cli.Context, logger logger.Service) error {
	headers := buildRequestHeaders(c.StringSlice(sshHeaderFlag))
	if c.IsSet(sshTokenIDFlag) {
		headers.Add(h2mux.CFAccessClientIDHeader, c.String(sshTokenIDFlag))
	}
	if c.IsSet(sshTokenSecretFlag) {
		headers.Add(h2mux.CFAccessClientSecretHeader, c.String(sshTokenSecretFlag))
	}
	options := &carrier.StartOptions{OriginURL: appUrl.String(), Headers: headers}

	if valid, err := isTokenValid(options, logger); err != nil {
		return err
	} else if valid {
		return nil
	}

	if err := token.RemoveTokenIfExists(appUrl); err != nil {
		return err
	}

	if valid, err := isTokenValid(options, logger); err != nil {
		return err
	} else if !valid {
		return errors.New("failed to verify token")
	}

	return nil
}

// isTokenValid makes a request to the origin and returns true if the response was not a 302.
func isTokenValid(options *carrier.StartOptions, logger logger.Service) (bool, error) {
	req, err := carrier.BuildAccessRequest(options, logger)
	if err != nil {
		return false, errors.Wrap(err, "Could not create access request")
	}

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
