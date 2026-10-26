package management

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/credentials"
)

var buildInfo *cliutil.BuildInfo

// Init initializes the management package with build info
func Init(bi *cliutil.BuildInfo) {
	buildInfo = bi
}

// Command returns the management command with its subcommands
func Command() *cli.Command {
	return &cli.Command{
		Name:     "management",
		Usage:    "Monitor cloudflared tunnels via management API",
		Category: "Management",
		Hidden:   true,
		Subcommands: []*cli.Command{
			buildTokenSubcommand(),
		},
	}
}

// buildTokenSubcommand creates the token subcommand
func buildTokenSubcommand() *cli.Command {
	return &cli.Command{
		Name:        "token",
		Action:      cliutil.ConfiguredAction(tokenCommand),
		Usage:       "Get management access jwt for a specific resource",
		UsageText:   "cloudflared management token --resource <resource> TUNNEL_ID",
		Description: "Get management access jwt for a tunnel with specified resource permissions (logs, admin, host_details)",
		Hidden:      true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "resource",
				Usage:    "Resource type for token permissions: logs, admin, or host_details",
				Required: true,
			},
			&cli.StringFlag{
				Name:    cfdflags.OriginCert,
				Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
				EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
				Value:   credentials.FindDefaultOriginCertPath(),
			},
			&cli.StringFlag{
				Name:    cfdflags.LogLevel,
				Value:   "info",
				Usage:   "Application logging level {debug, info, warn, error, fatal}",
				EnvVars: []string{"TUNNEL_LOGLEVEL"},
			},
			cliutil.FlagLogOutput,
		},
	}
}

// tokenCommand handles the token subcommand execution
func tokenCommand(c *cli.Context) error {
	log := cliutil.CreateStderrLogger(c)

	// Parse and validate resource flag
	resourceStr := c.String("resource")
	resource, err := parseResource(resourceStr)
	if err != nil {
		return fmt.Errorf("invalid resource '%s': %w", resourceStr, err)
	}

	// Get management token
	token, err := cliutil.GetManagementToken(c, log, resource, buildInfo)
	if err != nil {
		return err
	}

	// Output JSON to stdout
	tokenResponse := struct {
		Token string `json:"token"`
	}{Token: token}

	return json.NewEncoder(os.Stdout).Encode(tokenResponse)
}

// parseResource converts resource string to ManagementResource enum
func parseResource(resource string) (cfapi.ManagementResource, error) {
	switch resource {
	case "logs":
		return cfapi.Logs, nil
	case "admin":
		return cfapi.Admin, nil
	case "host_details":
		return cfapi.HostDetails, nil
	default:
		return 0, fmt.Errorf("must be one of: logs, admin, host_details")
	}
}
