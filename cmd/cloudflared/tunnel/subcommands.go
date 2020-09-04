package tunnel

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/tunnelstore"
)

const (
	credFileFlagAlias = "cred-file"
)

var (
	showDeletedFlag = &cli.BoolFlag{
		Name:    "show-deleted",
		Aliases: []string{"d"},
		Usage:   "Include deleted tunnels in the list",
	}
	listNameFlag = &cli.StringFlag{
		Name:    "name",
		Aliases: []string{"n"},
		Usage:   "List tunnels with the given name",
	}
	listExistedAtFlag = &cli.TimestampFlag{
		Name:    "when",
		Aliases: []string{"w"},
		Usage:   fmt.Sprintf("List tunnels that are active at the given time, expect format in RFC3339 (%s)", time.Now().Format(tunnelstore.TimeLayout)),
		Layout:  tunnelstore.TimeLayout,
	}
	listIDFlag = &cli.StringFlag{
		Name:    "id",
		Aliases: []string{"i"},
		Usage:   "List tunnel by ID",
	}
	showRecentlyDisconnected = &cli.BoolFlag{
		Name:    "show-recently-disconnected",
		Aliases: []string{"rd"},
		Usage:   "Include connections that have recently disconnected in the list",
	}
	outputFormatFlag = &cli.StringFlag{
		Name:    "output",
		Aliases: []string{"o"},
		Usage:   "Render output using given `FORMAT`. Valid options are 'json' or 'yaml'",
	}
	forceFlag = &cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage: "By default, if a tunnel is currently being run from a cloudflared, you can't " +
			"simultaneously rerun it again from a second cloudflared. The --force flag lets you " +
			"overwrite the previous tunnel. If you want to use a single hostname with multiple " +
			"tunnels, you can do so with Cloudflare's Load Balancer product.",
	}
	credentialsFileFlag = &cli.StringFlag{
		Name:    "credentials-file",
		Aliases: []string{credFileFlagAlias},
		Usage:   "File path of tunnel credentials",
	}
	forceDeleteFlag = &cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage:   "Allows you to delete a tunnel, even if it has active connections.",
	}
)

const hideSubcommands = true

func buildCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Action:    cliutil.ErrorHandler(createCommand),
		Usage:     "Create a new tunnel with given name",
		ArgsUsage: "TUNNEL-NAME",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{outputFormatFlag},
	}
}

// generateTunnelSecret as an array of 32 bytes using secure random number generator
func generateTunnelSecret() ([]byte, error) {
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	return randomBytes, err
}

func createCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel create" requires exactly 1 argument, the name of tunnel to create.`)
	}
	name := c.Args().First()

	_, err = sc.create(name)
	return errors.Wrap(err, "failed to create tunnel")
}

func tunnelFilePath(tunnelID uuid.UUID, directory string) (string, error) {
	fileName := fmt.Sprintf("%v.json", tunnelID)
	filePath := filepath.Clean(fmt.Sprintf("%s/%s", directory, fileName))
	return homedir.Expand(filePath)
}

func writeTunnelCredentials(tunnelID uuid.UUID, accountID, originCertPath string, tunnelSecret []byte, logger logger.Service) error {
	originCertDir := filepath.Dir(originCertPath)
	filePath, err := tunnelFilePath(tunnelID, originCertDir)
	if err != nil {
		return err
	}
	body, err := json.Marshal(pogs.TunnelAuth{
		AccountTag:   accountID,
		TunnelSecret: tunnelSecret,
	})
	if err != nil {
		return errors.Wrap(err, "Unable to marshal tunnel credentials to JSON")
	}
	logger.Infof("Writing tunnel credentials to %v. cloudflared chose this file based on where your origin certificate was found.", filePath)
	logger.Infof("Keep this file secret. To revoke these credentials, delete the tunnel.")
	return ioutil.WriteFile(filePath, body, 400)
}

func validFilePath(path string) bool {
	fileStat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fileStat.IsDir()
}

func buildListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Action:    cliutil.ErrorHandler(listCommand),
		Usage:     "List existing tunnels",
		ArgsUsage: " ",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{outputFormatFlag, showDeletedFlag, listNameFlag, listExistedAtFlag, listIDFlag, showRecentlyDisconnected},
	}
}

func listCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	filter := tunnelstore.NewFilter()
	if !c.Bool("show-deleted") {
		filter.NoDeleted()
	}
	if name := c.String("name"); name != "" {
		filter.ByName(name)
	}
	if existedAt := c.Timestamp("time"); existedAt != nil {
		filter.ByExistedAt(*existedAt)
	}
	if id := c.String("id"); id != "" {
		tunnelID, err := uuid.Parse(id)
		if err != nil {
			return errors.Wrapf(err, "%s is not a valid tunnel ID", id)
		}
		filter.ByTunnelID(tunnelID)
	}

	tunnels, err := sc.list(filter)
	if err != nil {
		return err
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, tunnels)
	}

	if len(tunnels) > 0 {
		fmtAndPrintTunnelList(tunnels, c.Bool("show-recently-disconnected"))
	} else {
		fmt.Println("You have no tunnels, use 'cloudflared tunnel create' to define a new tunnel")
	}
	return nil
}

func fmtAndPrintTunnelList(tunnels []*tunnelstore.Tunnel, showRecentlyDisconnected bool) {
	const (
		minWidth = 0
		tabWidth = 8
		padding  = 1
		padChar  = ' '
		flags    = 0
	)

	writer := tabwriter.NewWriter(os.Stdout, minWidth, tabWidth, padding, padChar, flags)

	// Print column headers with tabbed columns
	fmt.Fprintln(writer, "ID\tNAME\tCREATED\tCONNECTIONS\t")

	// Loop through tunnels, create formatted string for each, and print using tabwriter
	for _, t := range tunnels {
		formattedStr := fmt.Sprintf(
			"%s\t%s\t%s\t%s\t",
			t.ID,
			t.Name,
			t.CreatedAt.Format(time.RFC3339),
			fmtConnections(t.Connections, showRecentlyDisconnected),
		)
		fmt.Fprintln(writer, formattedStr)
	}

	// Write data buffered in tabwriter to output
	writer.Flush()
}

func fmtConnections(connections []tunnelstore.Connection, showRecentlyDisconnected bool) string {

	// Count connections per colo
	numConnsPerColo := make(map[string]uint, len(connections))
	for _, connection := range connections {
		if !connection.IsPendingReconnect || showRecentlyDisconnected {
			numConnsPerColo[connection.ColoName]++
		}
	}

	// Get sorted list of colos
	sortedColos := []string{}
	for coloName := range numConnsPerColo {
		sortedColos = append(sortedColos, coloName)
	}
	sort.Strings(sortedColos)

	// Map each colo to its frequency, combine into output string.
	var output []string
	for _, coloName := range sortedColos {
		output = append(output, fmt.Sprintf("%dx%s", numConnsPerColo[coloName], coloName))
	}
	return strings.Join(output, ", ")
}

func buildDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Action:    cliutil.ErrorHandler(deleteCommand),
		Usage:     "Delete existing tunnel with given IDs",
		ArgsUsage: "TUNNEL-ID",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{credentialsFileFlag, forceDeleteFlag},
	}
}

func deleteCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	if c.NArg() < 1 {
		return cliutil.UsageError(`"cloudflared tunnel delete" requires at least 1 argument, the ID or name of the tunnel to delete.`)
	}

	tunnelIDs, err := sc.findIDs(c.Args().Slice())
	if err != nil {
		return err
	}

	return sc.delete(tunnelIDs)
}

func renderOutput(format string, v interface{}) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(v)
	default:
		return errors.Errorf("Unknown output format '%s'", format)
	}
}

func buildRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Action:    cliutil.ErrorHandler(runCommand),
		Usage:     "Proxy a local web server by running the given tunnel",
		ArgsUsage: "TUNNEL-ID",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{forceFlag, credentialsFileFlag},
	}
}

func runCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel run" requires exactly 1 argument, the ID or name of the tunnel to run.`)
	}
	tunnelID, err := uuid.Parse(c.Args().First())
	if err != nil {
		return errors.Wrap(err, "error parsing tunnel ID")
	}

	return sc.run(tunnelID)
}

func buildCleanupCommand() *cli.Command {
	return &cli.Command{
		Name:      "cleanup",
		Action:    cliutil.ErrorHandler(cleanupCommand),
		Usage:     "Cleanup connections for the tunnel with given IDs",
		ArgsUsage: "TUNNEL-IDS",
		Hidden:    hideSubcommands,
	}
}

func cleanupCommand(c *cli.Context) error {
	if c.NArg() < 1 {
		return cliutil.UsageError(`"cloudflared tunnel cleanup" requires at least 1 argument, the IDs of the tunnels to cleanup connections.`)
	}

	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	tunnelIDs, err := sc.findIDs(c.Args().Slice())
	if err != nil {
		return err
	}

	return sc.cleanupConnections(tunnelIDs)
}

func buildRouteCommand() *cli.Command {
	return &cli.Command{
		Name:   "route",
		Action: cliutil.ErrorHandler(routeCommand),
		Usage:  "Define what hostname or load balancer can route to this tunnel",
		Description: `The route defines what hostname or load balancer can route to this tunnel.
	 To route a hostname: cloudflared tunnel route dns <tunnel ID> <hostname>
	 To route a load balancer: cloudflared tunnel route lb <tunnel ID> <load balancer name> <load balancer pool>
	 If you don't specify a load balancer pool, we will create a new pool called tunnel:<tunnel ID>`,
		ArgsUsage: "dns|lb TUNNEL-ID HOSTNAME [LB-POOL]",
		Hidden:    hideSubcommands,
	}
}

func dnsRouteFromArg(c *cli.Context, tunnelID uuid.UUID) (tunnelstore.Route, error) {
	const (
		userHostnameIndex = 2
		expectArgs        = 3
	)
	if c.NArg() != expectArgs {
		return nil, cliutil.UsageError("Expect %d arguments, got %d", expectArgs, c.NArg())
	}
	userHostname := c.Args().Get(userHostnameIndex)
	if userHostname == "" {
		return nil, cliutil.UsageError("The third argument should be the hostname")
	}
	return tunnelstore.NewDNSRoute(userHostname), nil
}

func lbRouteFromArg(c *cli.Context, tunnelID uuid.UUID) (tunnelstore.Route, error) {
	const (
		lbNameIndex   = 2
		lbPoolIndex   = 3
		expectMinArgs = 3
	)
	if c.NArg() < expectMinArgs {
		return nil, cliutil.UsageError("Expect at least %d arguments, got %d", expectMinArgs, c.NArg())
	}
	lbName := c.Args().Get(lbNameIndex)
	if lbName == "" {
		return nil, cliutil.UsageError("The third argument should be the load balancer name")
	}
	lbPool := c.Args().Get(lbPoolIndex)
	if lbPool == "" {
		lbPool = defaultPoolName(tunnelID)
	}

	return tunnelstore.NewLBRoute(lbName, lbPool), nil
}

func routeCommand(c *cli.Context) error {
	if c.NArg() < 2 {
		return cliutil.UsageError(`"cloudflared tunnel route" requires the first argument to be the route type(dns or lb), followed by the ID or name of the tunnel`)
	}
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	const tunnelIDIndex = 1

	routeType := c.Args().First()
	var r tunnelstore.Route
	var tunnelID uuid.UUID
	switch routeType {
	case "dns":
		tunnelID, err = sc.findID(c.Args().Get(tunnelIDIndex))
		if err != nil {
			return err
		}
		r, err = dnsRouteFromArg(c, tunnelID)
		if err != nil {
			return err
		}
	case "lb":
		tunnelID, err = sc.findID(c.Args().Get(tunnelIDIndex))
		if err != nil {
			return err
		}
		r, err = lbRouteFromArg(c, tunnelID)
		if err != nil {
			return err
		}
	default:
		return cliutil.UsageError("%s is not a recognized route type. Supported route types are dns and lb", routeType)
	}

	if err := sc.route(tunnelID, r); err != nil {
		return err
	}
	sc.logger.Infof(r.SuccessSummary())
	return nil
}

func defaultPoolName(tunnelID uuid.UUID) string {
	return fmt.Sprintf("tunnel:%v", tunnelID)
}
