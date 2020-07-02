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
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/certutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/origin"
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
)

const hideSubcommands = true

func buildCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Action:    cliutil.ErrorHandler(createTunnel),
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

func createTunnel(c *cli.Context) error {
	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel create" requires exactly 1 argument, the name of tunnel to create.`)
	}
	name := c.Args().First()

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	tunnelSecret, err := generateTunnelSecret()
	if err != nil {
		return err
	}

	cert, originCertPath, err := getOriginCertFromContext(c, logger)
	if err != nil {
		return err
	}
	client := newTunnelstoreClient(c, cert, logger)

	tunnel, err := client.CreateTunnel(name, tunnelSecret)
	if err != nil {
		return errors.Wrap(err, "Error creating a new tunnel")
	}

	if writeFileErr := writeTunnelCredentials(tunnel.ID, cert.AccountID, originCertPath, tunnelSecret, logger); err != nil {
		var errorLines []string
		errorLines = append(errorLines, fmt.Sprintf("Your tunnel '%v' was created with ID %v. However, cloudflared couldn't write to the tunnel credentials file at %v.json.", tunnel.Name, tunnel.ID, tunnel.ID))
		errorLines = append(errorLines, fmt.Sprintf("The file-writing error is: %v", writeFileErr))
		if deleteErr := client.DeleteTunnel(tunnel.ID); deleteErr != nil {
			errorLines = append(errorLines, fmt.Sprintf("Cloudflared tried to delete the tunnel for you, but encountered an error. You should use `cloudflared tunnel delete %v` to delete the tunnel yourself, because the tunnel can't be run without the tunnelfile.", tunnel.ID))
			errorLines = append(errorLines, fmt.Sprintf("The delete tunnel error is: %v", deleteErr))
		} else {
			errorLines = append(errorLines, fmt.Sprintf("The tunnel was deleted, because the tunnel can't be run without the tunnelfile"))
		}
		errorMsg := strings.Join(errorLines, "\n")
		return errors.New(errorMsg)
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, &tunnel)
	}

	logger.Infof("Created tunnel %s with id %s", tunnel.Name, tunnel.ID)
	return nil
}

func tunnelFilePath(tunnelID, directory string) (string, error) {
	fileName := fmt.Sprintf("%v.json", tunnelID)
	filePath := filepath.Clean(fmt.Sprintf("%s/%s", directory, fileName))
	return homedir.Expand(filePath)
}

func writeTunnelCredentials(tunnelID, accountID, originCertPath string, tunnelSecret []byte, logger logger.Service) error {
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

func readTunnelCredentials(c *cli.Context, tunnelID string, logger logger.Service) (*pogs.TunnelAuth, error) {
	filePath, err := tunnelCredentialsPath(c, tunnelID, logger)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't read tunnel credentials from %v", filePath)
	}

	var auth pogs.TunnelAuth
	if err = json.Unmarshal(body, &auth); err != nil {
		return nil, err
	}
	return &auth, nil
}

func tunnelCredentialsPath(c *cli.Context, tunnelID string, logger logger.Service) (string, error) {
	if filePath := c.String("credentials-file"); filePath != "" {
		if validFilePath(filePath) {
			return filePath, nil
		}
	}

	// Fallback to look for tunnel credentials in the origin cert directory
	if originCertPath, err := findOriginCert(c, logger); err == nil {
		originCertDir := filepath.Dir(originCertPath)
		if filePath, err := tunnelFilePath(tunnelID, originCertDir); err == nil {
			if validFilePath(filePath) {
				return filePath, nil
			}
		}
	}

	// Last resort look under default config directories
	for _, configDir := range config.DefaultConfigDirs {
		if filePath, err := tunnelFilePath(tunnelID, configDir); err == nil {
			if validFilePath(filePath) {
				return filePath, nil
			}
		}
	}
	return "", fmt.Errorf("Tunnel credentials file not found")
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
		Action:    cliutil.ErrorHandler(listTunnels),
		Usage:     "List existing tunnels",
		ArgsUsage: " ",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{outputFormatFlag, showDeletedFlag},
	}
}

func listTunnels(c *cli.Context) error {
	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	cert, _, err := getOriginCertFromContext(c, logger)
	if err != nil {
		return err
	}
	client := newTunnelstoreClient(c, cert, logger)

	allTunnels, err := client.ListTunnels()
	if err != nil {
		return errors.Wrap(err, "Error listing tunnels")
	}

	var tunnels []tunnelstore.Tunnel
	if c.Bool("show-deleted") {
		tunnels = allTunnels
	} else {
		for _, tunnel := range allTunnels {
			if tunnel.DeletedAt.IsZero() {
				tunnels = append(tunnels, tunnel)
			}
		}
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, tunnels)
	}

	if len(tunnels) > 0 {
		fmtAndPrintTunnelList(tunnels)
	} else {
		fmt.Println("You have no tunnels, use 'cloudflared tunnel create' to define a new tunnel")
	}

	return nil
}

func fmtAndPrintTunnelList(tunnels []tunnelstore.Tunnel) {
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
		formattedStr := fmt.Sprintf("%s\t%s\t%s\t%s\t", t.ID, t.Name, t.CreatedAt.Format(time.RFC3339), fmtConnections(t.Connections))
		fmt.Fprintln(writer, formattedStr)
	}

	// Write data buffered in tabwriter to output
	writer.Flush()
}

func fmtConnections(connections []tunnelstore.Connection) string {

	// Count connections per colo
	numConnsPerColo := make(map[string]uint, len(connections))
	for _, connection := range connections {
		numConnsPerColo[connection.ColoName]++
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
		Action:    cliutil.ErrorHandler(deleteTunnel),
		Usage:     "Delete existing tunnel with given ID",
		ArgsUsage: "TUNNEL-ID",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{credentialsFileFlag},
	}
}

func deleteTunnel(c *cli.Context) error {
	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel delete" requires exactly 1 argument, the ID of the tunnel to delete.`)
	}
	id := c.Args().First()

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	cert, _, err := getOriginCertFromContext(c, logger)
	if err != nil {
		return err
	}
	client := newTunnelstoreClient(c, cert, logger)

	if err := client.DeleteTunnel(id); err != nil {
		return errors.Wrapf(err, "Error deleting tunnel %s", id)
	}

	tunnelCredentialsPath, err := tunnelCredentialsPath(c, id, logger)
	if err != nil {
		logger.Infof("Cannot locate tunnel credentials to delete, error: %v. Please delete the file manually", err)
		return nil
	}

	if err = os.Remove(tunnelCredentialsPath); err != nil {
		logger.Infof("Cannot delete tunnel credentials, error: %v. Please delete the file manually", err)
	}

	return nil
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

func newTunnelstoreClient(c *cli.Context, cert *certutil.OriginCert, logger logger.Service) tunnelstore.Client {
	client := tunnelstore.NewRESTClient(c.String("api-url"), cert.AccountID, cert.ServiceKey, logger)
	return client
}

func getOriginCertFromContext(c *cli.Context, logger logger.Service) (cert *certutil.OriginCert, originCertPath string, err error) {
	originCertPath, err = findOriginCert(c, logger)
	if err != nil {
		return nil, "", errors.Wrap(err, "Error locating origin cert")
	}
	blocks, err := readOriginCert(originCertPath, logger)
	if err != nil {
		return nil, "", errors.Wrapf(err, "Can't read origin cert from %s", originCertPath)
	}

	cert, err = certutil.DecodeOriginCert(blocks)
	if err != nil {
		return nil, "", errors.Wrap(err, "Error decoding origin cert")
	}

	if cert.AccountID == "" {
		return nil, "", errors.Errorf(`Origin certificate needs to be refreshed before creating new tunnels.\nDelete %s and run "cloudflared login" to obtain a new cert.`, originCertPath)
	}
	return cert, originCertPath, nil
}

func buildRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Action:    cliutil.ErrorHandler(runTunnel),
		Usage:     "Proxy a local web server by running the given tunnel",
		ArgsUsage: "TUNNEL-ID",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{forceFlag, credentialsFileFlag},
	}
}

func runTunnel(c *cli.Context) error {
	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel run" requires exactly 1 argument, the ID of the tunnel to run.`)
	}
	id := c.Args().First()
	tunnelID, err := uuid.Parse(id)
	if err != nil {
		return errors.Wrap(err, "error parsing tunnel ID")
	}

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	credentials, err := readTunnelCredentials(c, id, logger)
	if err != nil {
		return err
	}
	logger.Debugf("Read credentials for %v", credentials.AccountTag)
	return StartServer(c, version, shutdownC, graceShutdownC, &origin.NamedTunnelConfig{Auth: *credentials, ID: tunnelID})
}

func buildCleanupCommand() *cli.Command {
	return &cli.Command{
		Name:      "cleanup",
		Action:    cliutil.ErrorHandler(cleanupConnections),
		Usage:     "Cleanup connections for the tunnel with given IDs",
		ArgsUsage: "TUNNEL-IDS",
		Hidden:    hideSubcommands,
	}
}

func cleanupConnections(c *cli.Context) error {
	if c.NArg() < 1 {
		return cliutil.UsageError(`"cloudflared tunnel cleanup" requires at least 1 argument, the IDs of the tunnels to cleanup connections.`)
	}

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	cert, _, err := getOriginCertFromContext(c, logger)
	if err != nil {
		return err
	}
	client := newTunnelstoreClient(c, cert, logger)

	for i := 0; i < c.NArg(); i++ {
		id := c.Args().Get(i)
		logger.Infof("Cleanup connection for tunnel %s", id)
		if err := client.CleanupConnections(id); err != nil {
			logger.Errorf("Error cleaning up connections for tunnel %s, error :%v", id, err)
		}
	}

	return nil
}
