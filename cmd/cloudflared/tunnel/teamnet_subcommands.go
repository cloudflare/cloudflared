package tunnel

import (
	"fmt"
	"net"
	"os"
	"text/tabwriter"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/teamnet"

	"github.com/urfave/cli/v2"
)

func buildRouteIPSubcommand() *cli.Command {
	return &cli.Command{
		Name:      "ip",
		Usage:     "Configure and query Cloudflare WARP routing to services or private networks available through this tunnel.",
		UsageText: "cloudflared tunnel [--config FILEPATH] route COMMAND [arguments...]",
		Description: `cloudflared can provision private routes from any IP space to origins in your corporate network.
Users enrolled in your Cloudflare for Teams organization can reach those routes through the
Cloudflare WARP client. You can also build rules to determine who can reach certain routes.`,
		Subcommands: []*cli.Command{
			{
				Name:      "add",
				Action:    cliutil.ConfiguredAction(addRouteCommand),
				Usage:     "Add any new network to the routing table reachable via the tunnel",
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip add [CIDR] [TUNNEL] [COMMENT?]",
				Description: `Adds any network route space (represented as a CIDR) to your routing table.
That network space becomes reachable for requests egressing from a user's machine
as long as it is using Cloudflare WARP client and is enrolled in the same account
that is running the tunnel chosen here. Further, those requests will be proxied to
the specified tunnel, and reach an IP in the given CIDR, as long as that IP is
reachable from the tunnel.`,
			},
			{
				Name:        "show",
				Aliases:     []string{"list"},
				Action:      cliutil.ConfiguredAction(showRoutesCommand),
				Usage:       "Show the routing table",
				UsageText:   "cloudflared tunnel [--config FILEPATH] route ip show [flags]",
				Description: `Shows your organization private routing table. You can use flags to filter the results.`,
				Flags:       showRoutesFlags(),
			},
			{
				Name:      "delete",
				Action:    cliutil.ConfiguredAction(deleteRouteCommand),
				Usage:     "Delete a row from your organization's private routing table",
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip delete [CIDR]",
				Description: `Deletes the row for a given CIDR from your routing table. That portion
of your network will no longer be reachable by the WARP clients.`,
			},
			{
				Name:      "get",
				Action:    cliutil.ConfiguredAction(getRouteByIPCommand),
				Usage:     "Check which row of the routing table matches a given IP.",
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip get [IP]",
				Description: `Checks which row of the routing table will be used to proxy a given IP.
				This helps check and validate your config.`,
			},
		},
	}
}

func showRoutesFlags() []cli.Flag {
	flags := make([]cli.Flag, 0)
	flags = append(flags, teamnet.FilterFlags...)
	flags = append(flags, outputFormatFlag)
	return flags
}

func showRoutesCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	filter, err := teamnet.NewFromCLI(c)
	if err != nil {
		return errors.Wrap(err, "invalid config for routing filters")
	}

	warningChecker := updater.StartWarningCheck(c)
	defer warningChecker.LogWarningIfAny(sc.log)

	routes, err := sc.listRoutes(filter)
	if err != nil {
		return err
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, routes)
	}

	if len(routes) > 0 {
		formatAndPrintRouteList(routes)
	} else {
		fmt.Println("You have no routes, use 'cloudflared tunnel route ip add' to add a route")
	}

	return nil
}

func addRouteCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() < 2 {
		return errors.New("You must supply at least 2 arguments, first the network you wish to route (in CIDR form e.g. 1.2.3.4/32) and then the tunnel ID to proxy with")
	}
	args := c.Args()
	_, network, err := net.ParseCIDR(args.Get(0))
	if err != nil {
		return errors.Wrap(err, "Invalid network CIDR")
	}
	if network == nil {
		return errors.New("Invalid network CIDR")
	}
	tunnelRef := args.Get(1)
	tunnelID, err := sc.findID(tunnelRef)
	if err != nil {
		return errors.Wrap(err, "Invalid tunnel")
	}
	comment := ""
	if c.NArg() >= 3 {
		comment = args.Get(2)
	}
	_, err = sc.addRoute(teamnet.NewRoute{
		Comment:  comment,
		Network:  *network,
		TunnelID: tunnelID,
	})
	if err != nil {
		return errors.Wrap(err, "API error")
	}
	fmt.Printf("Successfully added route for %s over tunnel %s\n", network, tunnelID)
	return nil
}

func deleteRouteCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() != 1 {
		return errors.New("You must supply exactly one argument, the network whose route you want to delete (in CIDR form e.g. 1.2.3.4/32)")
	}
	_, network, err := net.ParseCIDR(c.Args().First())
	if err != nil {
		return errors.Wrap(err, "Invalid network CIDR")
	}
	if network == nil {
		return errors.New("Invalid network CIDR")
	}
	if err := sc.deleteRoute(*network); err != nil {
		return errors.Wrap(err, "API error")
	}
	fmt.Printf("Successfully deleted route for %s\n", network)
	return nil
}

func getRouteByIPCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() != 1 {
		return errors.New("You must supply exactly one argument, an IP whose route will be queried (e.g. 1.2.3.4 or 2001:0db8:::7334)")
	}

	ipInput := c.Args().First()
	ip := net.ParseIP(ipInput)
	if ip == nil {
		return fmt.Errorf("Invalid IP %s", ipInput)
	}
	route, err := sc.getRouteByIP(ip)
	if err != nil {
		return errors.Wrap(err, "API error")
	}
	if route.IsZero() {
		fmt.Printf("No route matches the IP %s\n", ip)
	} else {
		formatAndPrintRouteList([]*teamnet.DetailedRoute{&route})
	}
	return nil
}

func formatAndPrintRouteList(routes []*teamnet.DetailedRoute) {
	const (
		minWidth = 0
		tabWidth = 8
		padding  = 1
		padChar  = ' '
		flags    = 0
	)

	writer := tabwriter.NewWriter(os.Stdout, minWidth, tabWidth, padding, padChar, flags)
	defer writer.Flush()

	// Print column headers with tabbed columns
	_, _ = fmt.Fprintln(writer, "NETWORK\tCOMMENT\tTUNNEL ID\tTUNNEL NAME\tCREATED\tDELETED\t")

	// Loop through routes, create formatted string for each, and print using tabwriter
	for _, route := range routes {
		formattedStr := route.TableString()
		_, _ = fmt.Fprintln(writer, formattedStr)
	}
}
