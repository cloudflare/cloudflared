package tunnel

import (
	"fmt"
	"net"
	"os"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
)

var (
	vnetFlag = &cli.StringFlag{
		Name:    "vnet",
		Aliases: []string{"vn"},
		Usage:   "The ID or name of the virtual network to which the route is associated to.",
	}

	errAddRoute = errors.New("You must supply exactly one argument, the ID or CIDR of the route you want to delete")
)

func buildRouteIPSubcommand() *cli.Command {
	return &cli.Command{
		Name:      "ip",
		Usage:     "Configure and query Cloudflare WARP routing to private IP networks made available through Cloudflare Tunnels.",
		UsageText: "cloudflared tunnel [--config FILEPATH] route COMMAND [arguments...]",
		Description: `cloudflared can provision routes for any IP space in your corporate network. Users enrolled in
your Cloudflare for Teams organization can reach those IPs through the Cloudflare WARP
client. You can then configure L7/L4 filtering on https://one.dash.cloudflare.com to
determine who can reach certain routes.
By default IP routes all exist within a single virtual network. If you use the same IP
space(s) in different physical private networks, all meant to be reachable via IP routes,
then you have to manage the ambiguous IP routes by associating them to virtual networks.
See "cloudflared tunnel vnet --help" for more information.`,
		Subcommands: []*cli.Command{
			{
				Name:      "add",
				Action:    cliutil.ConfiguredAction(addRouteCommand),
				Usage:     "Add a new network to the routing table reachable via a Tunnel",
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip add [flags] [CIDR] [TUNNEL] [COMMENT?]",
				Description: `Adds a network IP route space (represented as a CIDR) to your routing table.
That network IP space becomes reachable for requests egressing from a user's machine
as long as it is using Cloudflare WARP client and is enrolled in the same account
that is running the Tunnel chosen here. Further, those requests will be proxied to
the specified Tunnel, and reach an IP in the given CIDR, as long as that IP is
reachable from cloudflared.
If the CIDR exists in more than one private network, to be connected with Cloudflare
Tunnels, then you have to manage those IP routes with virtual networks (see
"cloudflared tunnel vnet --help)". In those cases, you then have to tell
which virtual network's routing table you want to add the route to with:
"cloudflared tunnel route ip add --vnet [ID/name] [CIDR] [TUNNEL]".`,
				Flags: []cli.Flag{vnetFlag},
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
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip delete [flags] [Route ID or CIDR]",
				Description: `Deletes the row for the given route ID from your routing table. That portion of your network
will no longer be reachable.`,
				Flags: []cli.Flag{vnetFlag},
			},
			{
				Name:      "get",
				Action:    cliutil.ConfiguredAction(getRouteByIPCommand),
				Usage:     "Check which row of the routing table matches a given IP.",
				UsageText: "cloudflared tunnel [--config FILEPATH] route ip get [flags] [IP]",
				Description: `Checks which row of the routing table will be used to proxy a given IP. This helps check
and validate your config. Note that if you use virtual networks, then you have
to tell which virtual network whose routing table you want to use.`,
				Flags: []cli.Flag{vnetFlag},
			},
		},
	}
}

func showRoutesFlags() []cli.Flag {
	flags := make([]cli.Flag, 0)
	flags = append(flags, cfapi.IpRouteFilterFlags...)
	flags = append(flags, outputFormatFlag)
	return flags
}

func showRoutesCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	filter, err := cfapi.NewIpRouteFilterFromCLI(c)
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
		fmt.Println("No routes were found for the given filter flags. You can use 'cloudflared tunnel route ip add' to add a route.")
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

	var vnetId *uuid.UUID
	if c.IsSet(vnetFlag.Name) {
		id, err := getVnetId(sc, c.String(vnetFlag.Name))
		if err != nil {
			return err
		}
		vnetId = &id
	}

	_, err = sc.addRoute(cfapi.NewRoute{
		Comment:  comment,
		Network:  *network,
		TunnelID: tunnelID,
		VNetID:   vnetId,
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
		return errAddRoute
	}

	var routeId uuid.UUID
	routeId, err = uuid.Parse(c.Args().First())
	if err != nil {
		_, network, err := net.ParseCIDR(c.Args().First())
		if err != nil || network == nil {
			return errAddRoute
		}

		var vnetId *uuid.UUID
		if c.IsSet(vnetFlag.Name) {
			id, err := getVnetId(sc, c.String(vnetFlag.Name))
			if err != nil {
				return err
			}
			vnetId = &id
		}

		routeId, err = sc.getRouteId(*network, vnetId)
		if err != nil {
			return err
		}
	}

	if err := sc.deleteRoute(routeId); err != nil {
		return errors.Wrap(err, "API error")
	}
	fmt.Printf("Successfully deleted route with ID %s\n", routeId)
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

	params := cfapi.GetRouteByIpParams{
		Ip: ip,
	}

	if c.IsSet(vnetFlag.Name) {
		vnetId, err := getVnetId(sc, c.String(vnetFlag.Name))
		if err != nil {
			return err
		}
		params.VNetID = &vnetId
	}

	route, err := sc.getRouteByIP(params)
	if err != nil {
		return errors.Wrap(err, "API error")
	}
	if route.IsZero() {
		fmt.Printf("No route matches the IP %s\n", ip)
	} else {
		formatAndPrintRouteList([]*cfapi.DetailedRoute{&route})
	}
	return nil
}

func formatAndPrintRouteList(routes []*cfapi.DetailedRoute) {
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
	_, _ = fmt.Fprintln(writer, "ID\tNETWORK\tVIRTUAL NET ID\tCOMMENT\tTUNNEL ID\tTUNNEL NAME\tCREATED\tDELETED\t")

	// Loop through routes, create formatted string for each, and print using tabwriter
	for _, route := range routes {
		formattedStr := route.TableString()
		_, _ = fmt.Fprintln(writer, formattedStr)
	}
}
