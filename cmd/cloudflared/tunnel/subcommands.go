package tunnel

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"golang.org/x/net/idna"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/tunnelstore"
)

const (
	allSortByOptions  = "name, id, createdAt, deletedAt, numConnections"
	CredFileFlagAlias = "cred-file"
	CredFileFlag      = "credentials-file"

	LogFieldTunnelID = "tunnelID"
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
		Usage:   "List tunnels with the given `NAME`",
	}
	listExistedAtFlag = &cli.TimestampFlag{
		Name:        "when",
		Aliases:     []string{"w"},
		Usage:       "List tunnels that are active at the given `TIME` in RFC3339 format",
		Layout:      tunnelstore.TimeLayout,
		DefaultText: fmt.Sprintf("current time, %s", time.Now().Format(tunnelstore.TimeLayout)),
	}
	listIDFlag = &cli.StringFlag{
		Name:    "id",
		Aliases: []string{"i"},
		Usage:   "List tunnel by `ID`",
	}
	showRecentlyDisconnected = &cli.BoolFlag{
		Name:    "show-recently-disconnected",
		Aliases: []string{"rd"},
		Usage:   "Include connections that have recently disconnected in the list",
	}
	outputFormatFlag = altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "output",
		Aliases: []string{"o"},
		Usage:   "Render output using given `FORMAT`. Valid options are 'json' or 'yaml'",
	})
	sortByFlag = &cli.StringFlag{
		Name:    "sort-by",
		Value:   "name",
		Usage:   fmt.Sprintf("Sorts the list of tunnels by the given field. Valid options are {%s}", allSortByOptions),
		EnvVars: []string{"TUNNEL_LIST_SORT_BY"},
	}
	invertSortFlag = &cli.BoolFlag{
		Name:    "invert-sort",
		Usage:   "Inverts the sort order of the tunnel list.",
		EnvVars: []string{"TUNNEL_LIST_INVERT_SORT"},
	}
	forceFlag = altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage: "By default, if a tunnel is currently being run from a cloudflared, you can't " +
			"simultaneously rerun it again from a second cloudflared. The --force flag lets you " +
			"overwrite the previous tunnel. If you want to use a single hostname with multiple " +
			"tunnels, you can do so with Cloudflare's Load Balancer product.",
	})
	credentialsFileFlag = altsrc.NewStringFlag(&cli.StringFlag{
		Name:    CredFileFlag,
		Aliases: []string{CredFileFlagAlias},
		Usage:   "Filepath at which to read/write the tunnel credentials",
		EnvVars: []string{"TUNNEL_CRED_FILE"},
	})
	forceDeleteFlag = &cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage:   "Allows you to delete a tunnel, even if it has active connections.",
		EnvVars: []string{"TUNNEL_RUN_FORCE_OVERWRITE"},
	}
	selectProtocolFlag = altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "protocol",
		Value:   "h2mux",
		Aliases: []string{"p"},
		Usage:   fmt.Sprintf("Protocol implementation to connect with Cloudflare's edge network. %s", connection.AvailableProtocolFlagMessage),
		EnvVars: []string{"TUNNEL_TRANSPORT_PROTOCOL"},
		Hidden:  true,
	})
)

func buildCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Action:    cliutil.ErrorHandler(createCommand),
		Usage:     "Create a new tunnel with given name",
		UsageText: "cloudflared tunnel [tunnel command options] create [subcommand options] NAME",
		Description: `Creates a tunnel, registers it with Cloudflare edge and generates credential file used to run this tunnel.
  Use "cloudflared tunnel route" subcommand to map a DNS name to this tunnel and "cloudflared tunnel run" to start the connection.

  For example, to create a tunnel named 'my-tunnel' run:

  $ cloudflared tunnel create my-tunnel`,
		Flags:              []cli.Flag{outputFormatFlag, credentialsFileFlag},
		CustomHelpTemplate: commandHelpTemplate(),
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

	_, err = sc.create(name, c.String(CredFileFlag))
	return errors.Wrap(err, "failed to create tunnel")
}

func tunnelFilePath(tunnelID uuid.UUID, directory string) (string, error) {
	fileName := fmt.Sprintf("%v.json", tunnelID)
	filePath := filepath.Clean(fmt.Sprintf("%s/%s", directory, fileName))
	return homedir.Expand(filePath)
}

// If an `outputFile` is given, write the credentials there.
// Otherwise, write it to the same directory as the originCert,
// with the filename `<tunnel id>.json`.
func writeTunnelCredentials(
	originCertPath, outputFile string,
	credentials *connection.Credentials,
) (filePath string, err error) {
	filePath = outputFile
	if outputFile == "" {
		originCertDir := filepath.Dir(originCertPath)
		filePath, err = tunnelFilePath(credentials.TunnelID, originCertDir)
	}
	if err != nil {
		return "", err
	}
	// Write the name and ID to the file too
	body, err := json.Marshal(credentials)
	if err != nil {
		return "", errors.Wrap(err, "Unable to marshal tunnel credentials to JSON")
	}
	return filePath, ioutil.WriteFile(filePath, body, 400)
}

func buildListCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Action:      cliutil.ErrorHandler(listCommand),
		Usage:       "List existing tunnels",
		UsageText:   "cloudflared tunnel [tunnel command options] list [subcommand options]",
		Description: "cloudflared tunnel list will display all active tunnels, their created time and associated connections. Use -d flag to include deleted tunnels. See the list of options to filter the list",
		Flags: []cli.Flag{
			outputFormatFlag,
			showDeletedFlag,
			listNameFlag,
			listExistedAtFlag,
			listIDFlag,
			showRecentlyDisconnected,
			sortByFlag,
			invertSortFlag,
		},
		CustomHelpTemplate: commandHelpTemplate(),
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

	// Sort the tunnels
	sortBy := c.String("sort-by")
	invalidSortField := false
	sort.Slice(tunnels, func(i, j int) bool {
		cmp := func() bool {
			switch sortBy {
			case "name":
				return tunnels[i].Name < tunnels[j].Name
			case "id":
				return tunnels[i].ID.String() < tunnels[j].ID.String()
			case "createdAt":
				return tunnels[i].CreatedAt.Unix() < tunnels[j].CreatedAt.Unix()
			case "deletedAt":
				return tunnels[i].DeletedAt.Unix() < tunnels[j].DeletedAt.Unix()
			case "numConnections":
				return len(tunnels[i].Connections) < len(tunnels[j].Connections)
			default:
				invalidSortField = true
				return tunnels[i].Name < tunnels[j].Name
			}
		}()
		if c.Bool("invert-sort") {
			return !cmp
		}
		return cmp
	})
	if invalidSortField {
		sc.log.Error().Msgf("%s is not a valid sort field. Valid sort fields are %s. Defaulting to 'name'.", sortBy, allSortByOptions)
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, tunnels)
	}

	if len(tunnels) > 0 {
		formatAndPrintTunnelList(tunnels, c.Bool("show-recently-disconnected"))
	} else {
		fmt.Println("You have no tunnels, use 'cloudflared tunnel create' to define a new tunnel")
	}
	return nil
}

func formatAndPrintTunnelList(tunnels []*tunnelstore.Tunnel, showRecentlyDisconnected bool) {
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
	_, _ = fmt.Fprintln(writer, "ID\tNAME\tCREATED\tCONNECTIONS\t")

	// Loop through tunnels, create formatted string for each, and print using tabwriter
	for _, t := range tunnels {
		formattedStr := fmt.Sprintf(
			"%s\t%s\t%s\t%s\t",
			t.ID,
			t.Name,
			t.CreatedAt.Format(time.RFC3339),
			fmtConnections(t.Connections, showRecentlyDisconnected),
		)
		_, _ = fmt.Fprintln(writer, formattedStr)
	}
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
		Name:               "delete",
		Action:             cliutil.ErrorHandler(deleteCommand),
		Usage:              "Delete existing tunnel by UUID or name",
		UsageText:          "cloudflared tunnel [tunnel command options] delete [subcommand options] TUNNEL",
		Description:        "cloudflared tunnel delete will delete tunnels with the given tunnel UUIDs or names. A tunnel cannot be deleted if it has active connections. To delete the tunnel unconditionally, use -f flag.",
		Flags:              []cli.Flag{credentialsFileFlag, forceDeleteFlag},
		CustomHelpTemplate: commandHelpTemplate(),
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
	flags := []cli.Flag{
		forceFlag,
		credentialsFileFlag,
		selectProtocolFlag,
	}
	flags = append(flags, configureProxyFlags(false)...)
	return &cli.Command{
		Name:      "run",
		Action:    cliutil.ErrorHandler(runCommand),
		Before:    SetFlagsFromConfigFile,
		Usage:     "Proxy a local web server by running the given tunnel",
		UsageText: "cloudflared tunnel [tunnel command options] run [subcommand options] [TUNNEL]",
		Description: `Runs the tunnel identified by name or UUUD, creating highly available connections
  between your server and the Cloudflare edge. You can provide name or UUID of tunnel to run either as the
  last command line argument or in the configuration file using "tunnel: TUNNEL".

  This command requires the tunnel credentials file created when "cloudflared tunnel create" was run,
  however it does not need access to cert.pem from "cloudflared login" if you identify the tunnel by UUID.
  If you experience other problems running the tunnel, "cloudflared tunnel cleanup" may help by removing
  any old connection records.
`,
		Flags:              flags,
		CustomHelpTemplate: commandHelpTemplate(),
	}
}

func runCommand(c *cli.Context) error {
	sc, err := newSubcommandContext(c)
	if err != nil {
		return err
	}

	if c.NArg() > 1 {
		return cliutil.UsageError(`"cloudflared tunnel run" accepts only one argument, the ID or name of the tunnel to run.`)
	}
	tunnelRef := c.Args().First()
	if tunnelRef == "" {
		// see if tunnel id was in the config file
		tunnelRef = config.GetConfiguration().TunnelID
		if tunnelRef == "" {
			return cliutil.UsageError(`"cloudflared tunnel run" requires the ID or name of the tunnel to run as the last command line argument or in the configuration file.`)
		}
	}

	return runNamedTunnel(sc, tunnelRef)
}

func runNamedTunnel(sc *subcommandContext, tunnelRef string) error {
	tunnelID, err := sc.findID(tunnelRef)
	if err != nil {
		return errors.Wrap(err, "error parsing tunnel ID")
	}

	sc.log.Info().Str(LogFieldTunnelID, tunnelID.String()).Msg("Starting tunnel")

	return sc.run(tunnelID)
}

func buildCleanupCommand() *cli.Command {
	return &cli.Command{
		Name:               "cleanup",
		Action:             cliutil.ErrorHandler(cleanupCommand),
		Usage:              "Cleanup tunnel connections",
		UsageText:          "cloudflared tunnel [tunnel command options] cleanup [subcommand options] TUNNEL",
		Description:        "Delete connections for tunnels with the given UUIDs or names.",
		CustomHelpTemplate: commandHelpTemplate(),
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
		Name:      "route",
		Action:    cliutil.ErrorHandler(routeCommand),
		Usage:     "Define what hostname or load balancer can route to this tunnel",
		UsageText: "cloudflared tunnel [tunnel command options] route [subcommand options] dns|lb TUNNEL HOSTNAME [LB-POOL]",
		Description: `The route defines what hostname or load balancer will proxy requests to this tunnel.

   To route a hostname by creating a CNAME to tunnel's address:
      cloudflared tunnel route dns <tunnel ID> <hostname>
   To use this tunnel as a load balancer origin, creating pool and load balancer if necessary:
      cloudflared tunnel route lb <tunnel ID> <load balancer name> <load balancer pool>`,
		CustomHelpTemplate: commandHelpTemplate(),
		Subcommands: []*cli.Command{
			buildRouteIPSubcommand(),
		},
	}
}

func dnsRouteFromArg(c *cli.Context) (tunnelstore.Route, error) {
	const (
		userHostnameIndex = 2
		expectedNArgs     = 3
	)
	if c.NArg() != expectedNArgs {
		return nil, cliutil.UsageError("Expected %d arguments, got %d", expectedNArgs, c.NArg())
	}
	userHostname := c.Args().Get(userHostnameIndex)
	if userHostname == "" {
		return nil, cliutil.UsageError("The third argument should be the hostname")
	} else if !validateHostname(userHostname, true) {
		return nil, errors.Errorf("%s is not a valid hostname", userHostname)
	}
	return tunnelstore.NewDNSRoute(userHostname), nil
}

func lbRouteFromArg(c *cli.Context) (tunnelstore.Route, error) {
	const (
		lbNameIndex   = 2
		lbPoolIndex   = 3
		expectedNArgs = 4
	)
	if c.NArg() != expectedNArgs {
		return nil, cliutil.UsageError("Expected %d arguments, got %d", expectedNArgs, c.NArg())
	}
	lbName := c.Args().Get(lbNameIndex)
	if lbName == "" {
		return nil, cliutil.UsageError("The third argument should be the load balancer name")
	} else if !validateHostname(lbName, true) {
		return nil, errors.Errorf("%s is not a valid load balancer name", lbName)
	}

	lbPool := c.Args().Get(lbPoolIndex)
	if lbPool == "" {
		return nil, cliutil.UsageError("The fourth argument should be the pool name")
	} else if !validateName(lbPool, false) {
		return nil, errors.Errorf("%s is not a valid pool name", lbPool)
	}

	return tunnelstore.NewLBRoute(lbName, lbPool), nil
}

var nameRegex = regexp.MustCompile("^[_a-zA-Z0-9][-_.a-zA-Z0-9]*$")
var hostNameRegex = regexp.MustCompile("^[*_a-zA-Z0-9][-_.a-zA-Z0-9]*$")

func validateName(s string, allowWildcardSubdomain bool) bool {
	if allowWildcardSubdomain {
		return hostNameRegex.MatchString(s)
	}
	return nameRegex.MatchString(s)
}

func validateHostname(s string, allowWildcardSubdomain bool) bool {
	// Slightly stricter than PunyCodeProfile
	idnaProfile := idna.New(
		idna.ValidateLabels(true),
		idna.VerifyDNSLength(true))

	puny, err := idnaProfile.ToASCII(s)
	return err == nil && validateName(puny, allowWildcardSubdomain)
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
	var route tunnelstore.Route
	var tunnelID uuid.UUID
	switch routeType {
	case "dns":
		tunnelID, err = sc.findID(c.Args().Get(tunnelIDIndex))
		if err != nil {
			return err
		}
		route, err = dnsRouteFromArg(c)
		if err != nil {
			return err
		}
	case "lb":
		tunnelID, err = sc.findID(c.Args().Get(tunnelIDIndex))
		if err != nil {
			return err
		}
		route, err = lbRouteFromArg(c)
		if err != nil {
			return err
		}
	default:
		return cliutil.UsageError("%s is not a recognized route type. Supported route types are dns and lb", routeType)
	}

	res, err := sc.route(tunnelID, route)
	if err != nil {
		return err
	}

	sc.log.Info().Str(LogFieldTunnelID, tunnelID.String()).Msg(res.SuccessSummary())
	return nil
}

func commandHelpTemplate() string {
	var parentFlagsHelp string
	for _, f := range configureCloudflaredFlags(false) {
		parentFlagsHelp += fmt.Sprintf(" %s\n\t", f)
	}
	for _, f := range configureLoggingFlags(false) {
		parentFlagsHelp += fmt.Sprintf(" %s\n\t", f)
	}
	const template = `NAME:
	{{.HelpName}} - {{.Usage}}

USAGE:
	{{.UsageText}}

DESCRIPTION:
	{{.Description}}

TUNNEL COMMAND OPTIONS:
	%s
SUBCOMMAND OPTIONS:
	{{range .VisibleFlags}}{{.}}
	{{end}}
`
	return fmt.Sprintf(template, parentFlagsHelp)
}
