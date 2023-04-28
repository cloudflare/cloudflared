package tunnel

import (
	"fmt"
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
	makeDefaultFlag = &cli.BoolFlag{
		Name:    "default",
		Aliases: []string{"d"},
		Usage: "The virtual network becomes the default one for the account. This means that all operations that " +
			"omit a virtual network will now implicitly be using this virtual network (i.e., the default one) such " +
			"as new IP routes that are created. When this flag is not set, the virtual network will not become the " +
			"default one in the account.",
	}
	newNameFlag = &cli.StringFlag{
		Name:    "name",
		Aliases: []string{"n"},
		Usage:   "The new name for the virtual network.",
	}
	newCommentFlag = &cli.StringFlag{
		Name:    "comment",
		Aliases: []string{"c"},
		Usage:   "A new comment describing the purpose of the virtual network.",
	}
	vnetForceDeleteFlag = &cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage: "Force the deletion of the virtual network even if it is being relied upon by other resources. Those" +
			"resources will either be deleted (e.g. IP Routes) or moved to the current default virutal network.",
	}
)

func buildVirtualNetworkSubcommand(hidden bool) *cli.Command {
	return &cli.Command{
		Name:      "vnet",
		Usage:     "Configure and query virtual networks to manage private IP routes with overlapping IPs.",
		UsageText: "cloudflared tunnel [--config FILEPATH] network COMMAND [arguments...]",
		Description: `cloudflared allows to manage IP routes that expose origins in your private network space via their IP directly
to clients outside (e.g. using WARP client) --- those are configurable via "cloudflared tunnel route ip" commands.
By default, all those IP routes live in the same virtual network. Managing virtual networks (e.g. by creating a
new one) becomes relevant when you have different private networks that have overlapping IPs. E.g.: if you have
a private network A running Tunnel 1, and private network B running Tunnel 2, it is possible that both Tunnels
expose the same IP space (say 10.0.0.0/8); to handle that, you have to add each IP Route (one that points to
Tunnel 1 and another that points to Tunnel 2) in different Virtual Networks. That way, if your clients are on
Virtual Network X, they will see Tunnel 1 (via Route A) and not see Tunnel 2 (since its Route B is associated
to another Virtual Network Y).`,
		Hidden: hidden,
		Subcommands: []*cli.Command{
			{
				Name:      "add",
				Action:    cliutil.ConfiguredAction(addVirtualNetworkCommand),
				Usage:     "Add a new virtual network to which IP routes can be attached",
				UsageText: "cloudflared tunnel [--config FILEPATH] network add [flags] NAME [\"comment\"]",
				Description: `Adds a new virtual network. You can then attach IP routes to this virtual network with "cloudflared tunnel route ip"
commands. By doing so, such route(s) become segregated from route(s) in another virtual networks. Note that all
routes exist within some virtual network. If you do not specify any, then the system pre-creates a default virtual
network to which all routes belong. That is fine if you do not have overlapping IPs within different physical
private networks in your infrastructure exposed via Cloudflare Tunnel. Note: if a virtual network is added as
the new default, then the previous existing default virtual network will be automatically modified to no longer
be the current default.`,
				Flags:  []cli.Flag{makeDefaultFlag},
				Hidden: hidden,
			},
			{
				Name:        "list",
				Action:      cliutil.ConfiguredAction(listVirtualNetworksCommand),
				Usage:       "Lists the virtual networks",
				UsageText:   "cloudflared tunnel [--config FILEPATH] network list [flags]",
				Description: "Lists the virtual networks based on the given filter flags.",
				Flags:       listVirtualNetworksFlags(),
				Hidden:      hidden,
			},
			{
				Name:      "delete",
				Action:    cliutil.ConfiguredAction(deleteVirtualNetworkCommand),
				Usage:     "Delete a virtual network",
				UsageText: "cloudflared tunnel [--config FILEPATH] network delete VIRTUAL_NETWORK",
				Description: `Deletes the virtual network (given its ID or name). This is only possible if that virtual network is unused. 
A virtual network may be used by IP routes or by WARP devices.`,
				Flags:  []cli.Flag{vnetForceDeleteFlag},
				Hidden: hidden,
			},
			{
				Name:      "update",
				Action:    cliutil.ConfiguredAction(updateVirtualNetworkCommand),
				Usage:     "Update a virtual network",
				UsageText: "cloudflared tunnel [--config FILEPATH] network update [flags] VIRTUAL_NETWORK",
				Description: `Updates the virtual network (given its ID or name). If this virtual network is updated to become the new
default, then the previously existing default virtual network will also be modified to no longer be the default.
You cannot update a virtual network to not be the default anymore directly. Instead, you should create a new
default or update an existing one to become the default.`,
				Flags:  []cli.Flag{newNameFlag, newCommentFlag, makeDefaultFlag},
				Hidden: hidden,
			},
		},
	}
}

func listVirtualNetworksFlags() []cli.Flag {
	flags := make([]cli.Flag, 0)
	flags = append(flags, cfapi.VnetFilterFlags...)
	flags = append(flags, outputFormatFlag)
	return flags
}

func addVirtualNetworkCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() < 1 {
		return errors.New("You must supply at least 1 argument, the name of the virtual network you wish to add.")
	}

	warningChecker := updater.StartWarningCheck(c)
	defer warningChecker.LogWarningIfAny(sc.log)

	args := c.Args()

	name := args.Get(0)

	comment := ""
	if c.NArg() >= 2 {
		comment = args.Get(1)
	}

	newVnet := cfapi.NewVirtualNetwork{
		Name:      name,
		Comment:   comment,
		IsDefault: c.Bool(makeDefaultFlag.Name),
	}
	createdVnet, err := sc.addVirtualNetwork(newVnet)

	if err != nil {
		return errors.Wrap(err, "Could not add virtual network")
	}

	extraMsg := ""
	if createdVnet.IsDefault {
		extraMsg = " (as the new default for this account) "
	}
	fmt.Printf(
		"Successfully added virtual 'network' %s with ID: %s%s\n"+
			"You can now add IP routes attached to this virtual network. See `cloudflared tunnel route ip add -help`\n",
		name, createdVnet.ID, extraMsg,
	)
	return nil
}

func listVirtualNetworksCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	warningChecker := updater.StartWarningCheck(c)
	defer warningChecker.LogWarningIfAny(sc.log)

	filter, err := cfapi.NewFromCLI(c)
	if err != nil {
		return errors.Wrap(err, "invalid flags for filtering virtual networks")
	}

	vnets, err := sc.listVirtualNetworks(filter)
	if err != nil {
		return err
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, vnets)
	}

	if len(vnets) > 0 {
		formatAndPrintVnetsList(vnets)
	} else {
		fmt.Println("No virtual networks were found for the given filter flags. You can use 'cloudflared tunnel vnet add' to add a virtual network.")
	}

	return nil
}

func deleteVirtualNetworkCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() < 1 {
		return errors.New("You must supply exactly one argument, either the ID or name of the virtual network to delete")
	}

	input := c.Args().Get(0)
	vnetId, err := getVnetId(sc, input)
	if err != nil {
		return err
	}

	forceDelete := false
	if c.IsSet(vnetForceDeleteFlag.Name) {
		forceDelete = c.Bool(vnetForceDeleteFlag.Name)
	}

	if err := sc.deleteVirtualNetwork(vnetId, forceDelete); err != nil {
		return errors.Wrap(err, "API error")
	}
	fmt.Printf("Successfully deleted virtual network '%s'\n", input)
	return nil
}

func updateVirtualNetworkCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}
	if c.NArg() != 1 {
		return errors.New(" You must supply exactly one argument, either the ID or (current) name of the virtual network to update")
	}

	input := c.Args().Get(0)
	vnetId, err := getVnetId(sc, input)
	if err != nil {
		return err
	}

	updates := cfapi.UpdateVirtualNetwork{}

	if c.IsSet(newNameFlag.Name) {
		newName := c.String(newNameFlag.Name)
		updates.Name = &newName
	}
	if c.IsSet(newCommentFlag.Name) {
		newComment := c.String(newCommentFlag.Name)
		updates.Comment = &newComment
	}
	if c.IsSet(makeDefaultFlag.Name) {
		isDefault := c.Bool(makeDefaultFlag.Name)
		updates.IsDefault = &isDefault
	}

	if err := sc.updateVirtualNetwork(vnetId, updates); err != nil {
		return errors.Wrap(err, "API error")
	}
	fmt.Printf("Successfully updated virtual network '%s'\n", input)
	return nil
}

func getVnetId(sc *subcommandContext, input string) (uuid.UUID, error) {
	val, err := uuid.Parse(input)
	if err == nil {
		return val, nil
	}

	filter := cfapi.NewVnetFilter()
	filter.WithDeleted(false)
	filter.ByName(input)

	vnets, err := sc.listVirtualNetworks(filter)
	if err != nil {
		return uuid.Nil, err
	}

	if len(vnets) != 1 {
		return uuid.Nil, fmt.Errorf("there should only be 1 non-deleted virtual network named %s", input)
	}

	return vnets[0].ID, nil
}

func formatAndPrintVnetsList(vnets []*cfapi.VirtualNetwork) {
	const (
		minWidth = 0
		tabWidth = 8
		padding  = 1
		padChar  = ' '
		flags    = 0
	)

	writer := tabwriter.NewWriter(os.Stdout, minWidth, tabWidth, padding, padChar, flags)
	defer writer.Flush()

	_, _ = fmt.Fprintln(writer, "ID\tNAME\tIS DEFAULT\tCOMMENT\tCREATED\tDELETED\t")

	for _, virtualNetwork := range vnets {
		formattedStr := virtualNetwork.TableString()
		_, _ = fmt.Fprintln(writer, formattedStr)
	}
}
