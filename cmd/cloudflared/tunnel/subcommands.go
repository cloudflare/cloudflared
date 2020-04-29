package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/certutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelstore"
)

var (
	outputFormatFlag = &cli.StringFlag{
		Name:    "output",
		Aliases: []string{"o"},
		Usage:   "Render output using given `FORMAT`. Valid options are 'json' or 'yaml'",
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

func createTunnel(c *cli.Context) error {
	if c.NArg() != 1 {
		return cliutil.UsageError(`"cloudflared tunnel create" requires exactly 1 argument, the name of tunnel to create.`)
	}
	name := c.Args().First()

	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	client, err := newTunnelstoreClient(c, logger)
	if err != nil {
		return err
	}

	tunnel, err := client.CreateTunnel(name)
	if err != nil {
		return errors.Wrap(err, "Error creating a new tunnel")
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, &tunnel)
	}

	logger.Infof("Created tunnel %s with id %s", tunnel.Name, tunnel.ID)
	return nil
}

func buildListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Action:    cliutil.ErrorHandler(listTunnels),
		Usage:     "List existing tunnels",
		ArgsUsage: " ",
		Hidden:    hideSubcommands,
		Flags:     []cli.Flag{outputFormatFlag},
	}
}

func listTunnels(c *cli.Context) error {
	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	client, err := newTunnelstoreClient(c, logger)
	if err != nil {
		return err
	}

	tunnels, err := client.ListTunnels()
	if err != nil {
		return errors.Wrap(err, "Error listing tunnels")
	}

	if outputFormat := c.String(outputFormatFlag.Name); outputFormat != "" {
		return renderOutput(outputFormat, tunnels)
	}
	if len(tunnels) > 0 {
		const listFormat = "%-40s%-40s%s\n"
		fmt.Printf(listFormat, "ID", "NAME", "CREATED")
		for _, t := range tunnels {
			fmt.Printf(listFormat, t.ID, t.Name, t.CreatedAt.Format(time.RFC3339))
		}
	} else {
		fmt.Println("You have no tunnels, use 'cloudflared tunnel create' to define a new tunnel")
	}

	return nil
}

func buildDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Action:    cliutil.ErrorHandler(deleteTunnel),
		Usage:     "Delete existing tunnel with given ID",
		ArgsUsage: "TUNNEL-ID",
		Hidden:    hideSubcommands,
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

	client, err := newTunnelstoreClient(c, logger)
	if err != nil {
		return err
	}

	if err := client.DeleteTunnel(id); err != nil {
		return errors.Wrapf(err, "Error deleting tunnel %s", id)
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

func newTunnelstoreClient(c *cli.Context, logger logger.Service) (tunnelstore.Client, error) {
	originCertPath, err := findOriginCert(c, logger)
	if err != nil {
		return nil, errors.Wrap(err, "Error locating origin cert")
	}

	blocks, err := readOriginCert(originCertPath, logger)
	if err != nil {
		return nil, errors.Wrapf(err, "Can't read origin cert from %s", originCertPath)
	}

	cert, err := certutil.DecodeOriginCert(blocks)
	if err != nil {
		return nil, errors.Wrap(err, "Error decoding origin cert")
	}

	if cert.AccountID == "" {
		return nil, errors.Errorf(`Origin certificate needs to be refreshed before creating new tunnels.\nDelete %s and run "cloudflared login" to obtain a new cert.`, originCertPath)
	}

	client := tunnelstore.NewRESTClient(c.String("api-url"), cert.AccountID, cert.ServiceKey, logger)

	return client, nil
}
