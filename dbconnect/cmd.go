package dbconnect

import (
	"context"
	"log"
	"net"
	"strconv"

	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"
)

// Cmd is the entrypoint command for dbconnect.
//
// The tunnel package is responsible for appending this to tunnel.Commands().
func Cmd() *cli.Command {
	return &cli.Command{
		Category:  "Database Connect (ALPHA) - Deprecated",
		Name:      "db-connect",
		Usage:     "deprecated: Access your SQL database from Cloudflare Workers or the browser",
		ArgsUsage: " ",
		Description: `
		This feature has been deprecated. 
		Please see: 

		cloudflared access tcp --help 

		for setting up database connections to the cloudflare edge.
		

		Creates a connection between your database and the Cloudflare edge.
		Now you can execute SQL commands anywhere you can send HTTPS requests.

		Connect your database with any of the following commands, you can also try the "playground" without a database:

			cloudflared db-connect --hostname sql.mysite.com --url postgres://user:pass@localhost?sslmode=disable \
			                       --auth-domain mysite.cloudflareaccess.com --application-aud my-access-policy-tag
			cloudflared db-connect --hostname sql-dev.mysite.com --url mysql://localhost --insecure
			cloudflared db-connect --playground
		
		Requests should be authenticated using Cloudflare Access, learn more about how to enable it here:

			https://developers.cloudflare.com/access/service-auth/service-token/
		`,
		Flags: []cli.Flag{
			altsrc.NewStringFlag(&cli.StringFlag{
				Name:    "url",
				Usage:   "URL to the database (eg. postgres://user:pass@localhost?sslmode=disable)",
				EnvVars: []string{"TUNNEL_URL"},
			}),
			altsrc.NewStringFlag(&cli.StringFlag{
				Name:    "hostname",
				Usage:   "Hostname to accept commands over HTTPS (eg. sql.mysite.com)",
				EnvVars: []string{"TUNNEL_HOSTNAME"},
			}),
			altsrc.NewStringFlag(&cli.StringFlag{
				Name:    "auth-domain",
				Usage:   "Cloudflare Access authentication domain for your account (eg. mysite.cloudflareaccess.com)",
				EnvVars: []string{"TUNNEL_ACCESS_AUTH_DOMAIN"},
			}),
			altsrc.NewStringFlag(&cli.StringFlag{
				Name:    "application-aud",
				Usage:   "Cloudflare Access application \"AUD\" to verify JWTs from requests",
				EnvVars: []string{"TUNNEL_ACCESS_APPLICATION_AUD"},
			}),
			altsrc.NewBoolFlag(&cli.BoolFlag{
				Name:    "insecure",
				Usage:   "Disable authentication, the database will be open to the Internet",
				Value:   false,
				EnvVars: []string{"TUNNEL_ACCESS_INSECURE"},
			}),
			altsrc.NewBoolFlag(&cli.BoolFlag{
				Name:    "playground",
				Usage:   "Run a temporary, in-memory SQLite3 database for testing",
				Value:   false,
				EnvVars: []string{"TUNNEL_HELLO_WORLD"},
			}),
			altsrc.NewStringFlag(&cli.StringFlag{
				Name:    "loglevel",
				Value:   "debug", // Make it more verbose than the tunnel default 'info'.
				EnvVars: []string{"TUNNEL_LOGLEVEL"},
				Hidden:  true,
			}),
		},
		Before: CmdBefore,
		Action: CmdAction,
		Hidden: true,
	}
}

// CmdBefore runs some validation checks before running the command.
func CmdBefore(c *cli.Context) error {
	// Show the help text is no flags are specified.
	if c.NumFlags() == 0 {
		return cli.ShowSubcommandHelp(c)
	}

	// Hello-world and playground are synonymous with each other,
	// unset hello-world to prevent tunnel from initializing the hello package.
	if c.IsSet("hello-world") {
		c.Set("playground", "true")
		c.Set("hello-world", "false")
	}

	// Unix-socket database urls are supported, but the logic is the same as url.
	if c.IsSet("unix-socket") {
		c.Set("url", c.String("unix-socket"))
		c.Set("unix-socket", "")
	}

	// When playground mode is enabled, run with an in-memory database.
	if c.IsSet("playground") {
		c.Set("url", "sqlite3::memory:?cache=shared")
		c.Set("insecure", strconv.FormatBool(!c.IsSet("auth-domain") && !c.IsSet("application-aud")))
	}

	// At this point, insecure configurations are valid.
	if c.Bool("insecure") {
		return nil
	}

	// Ensure that secure configurations specify a hostname, domain, and tag for JWT validation.
	if !c.IsSet("hostname") || !c.IsSet("auth-domain") || !c.IsSet("application-aud") {
		log.Fatal("must specify --hostname, --auth-domain, and --application-aud unless you want to run in --insecure mode")
	}

	return nil
}

// CmdAction starts the Proxy and sets the url in cli.Context to point to the Proxy address.
func CmdAction(c *cli.Context) error {
	// STOR-612: sync with context in tunnel daemon.
	ctx := context.Background()

	var proxy *Proxy
	var err error
	if c.Bool("insecure") {
		proxy, err = NewInsecureProxy(ctx, c.String("url"))
	} else {
		proxy, err = NewSecureProxy(ctx, c.String("url"), c.String("auth-domain"), c.String("application-aud"))
	}

	if err != nil {
		log.Fatal(err)
		return err
	}

	listenerC := make(chan net.Listener)
	defer close(listenerC)

	// Since the Proxy should only talk to the tunnel daemon, find the next available
	// localhost port and start to listen to requests.
	go func() {
		err := proxy.Start(ctx, "127.0.0.1:", listenerC)
		if err != nil {
			log.Fatal(err)
		}
	}()

	// Block until the the Proxy is online, retreive its address, and change the url to point to it.
	// This is effectively "handing over" control to the tunnel package so it can run the tunnel daemon.
	c.Set("url", "https://"+(<-listenerC).Addr().String())

	return nil
}
