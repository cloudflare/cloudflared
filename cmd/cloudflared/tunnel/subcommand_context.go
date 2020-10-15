package tunnel

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/certutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/tunnelstore"
)

// subcommandContext carries structs shared between subcommands, to reduce number of arguments needed to
// pass between subcommands, and make sure they are only initialized once
type subcommandContext struct {
	c      *cli.Context
	logger logger.Service

	// These fields should be accessed using their respective Getter
	tunnelstoreClient tunnelstore.Client
	userCredential    *userCredential

	isUIEnabled bool
}

func newSubcommandContext(c *cli.Context) (*subcommandContext, error) {
	isUIEnabled := c.IsSet(uiFlag) && c.String("name") != ""

	// If UI is enabled, terminal log output should be disabled -- log should be written into a UI log window instead
	logger, err := createLogger(c, false, isUIEnabled)
	if err != nil {
		return nil, errors.Wrap(err, "error setting up logger")
	}

	return &subcommandContext{
		c:           c,
		logger:      logger,
		isUIEnabled: isUIEnabled,
	}, nil
}

type userCredential struct {
	cert     *certutil.OriginCert
	certPath string
}

func (sc *subcommandContext) client() (tunnelstore.Client, error) {
	if sc.tunnelstoreClient != nil {
		return sc.tunnelstoreClient, nil
	}
	credential, err := sc.credential()
	if err != nil {
		return nil, err
	}
	userAgent := fmt.Sprintf("cloudflared/%s", version)
	client, err := tunnelstore.NewRESTClient(sc.c.String("api-url"), credential.cert.AccountID, credential.cert.ZoneID, credential.cert.ServiceKey, userAgent, sc.logger)
	if err != nil {
		return nil, err
	}
	sc.tunnelstoreClient = client
	return client, nil
}

func (sc *subcommandContext) credential() (*userCredential, error) {
	if sc.userCredential == nil {
		originCertPath, err := findOriginCert(sc.c, sc.logger)
		if err != nil {
			return nil, errors.Wrap(err, "Error locating origin cert")
		}
		blocks, err := readOriginCert(originCertPath, sc.logger)
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

		sc.userCredential = &userCredential{
			cert:     cert,
			certPath: originCertPath,
		}
	}
	return sc.userCredential, nil
}

func (sc *subcommandContext) readTunnelCredentials(tunnelID uuid.UUID) (*pogs.TunnelAuth, error) {
	filePath, err := sc.tunnelCredentialsPath(tunnelID)
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

func (sc *subcommandContext) tunnelCredentialsPath(tunnelID uuid.UUID) (string, error) {
	if filePath := sc.c.String("credentials-file"); filePath != "" {
		if validFilePath(filePath) {
			return filePath, nil
		}
	}

	// Fallback to look for tunnel credentials in the origin cert directory
	if originCertPath, err := findOriginCert(sc.c, sc.logger); err == nil {
		originCertDir := filepath.Dir(originCertPath)
		if filePath, err := tunnelFilePath(tunnelID, originCertDir); err == nil {
			if validFilePath(filePath) {
				return filePath, nil
			}
		}
	}

	// Last resort look under default config directories
	for _, configDir := range config.DefaultConfigSearchDirectories() {
		if filePath, err := tunnelFilePath(tunnelID, configDir); err == nil {
			if validFilePath(filePath) {
				return filePath, nil
			}
		}
	}
	return "", fmt.Errorf("Tunnel credentials file not found")
}

// getConfigFileTunnelRef returns tunnel UUID or name set in the configuration file
func (sc *subcommandContext) getConfigFileTunnelRef() (string, error) {
	if src, err := config.GetConfigFileSource(sc.c, sc.logger); err == nil {
		if tunnelRef, err := src.String("tunnel"); err != nil {
			return "", errors.Wrapf(err, "invalid tunnel ID or name")
		} else {
			return tunnelRef, nil
		}
	}
	return "", nil
}

func (sc *subcommandContext) create(name string) (*tunnelstore.Tunnel, error) {
	client, err := sc.client()
	if err != nil {
		return nil, errors.Wrap(err, "couldn't create client to talk to Argo Tunnel backend")
	}

	tunnelSecret, err := generateTunnelSecret()
	if err != nil {
		return nil, errors.Wrap(err, "couldn't generate the secret for your new tunnel")
	}

	tunnel, err := client.CreateTunnel(name, tunnelSecret)
	if err != nil {
		return nil, errors.Wrap(err, "Create Tunnel API call failed")
	}

	credential, err := sc.credential()
	if err != nil {
		return nil, err
	}
	if writeFileErr := writeTunnelCredentials(tunnel.ID, credential.cert.AccountID, credential.certPath, tunnelSecret, sc.logger); err != nil {
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
		return nil, errors.New(errorMsg)
	}

	if outputFormat := sc.c.String(outputFormatFlag.Name); outputFormat != "" {
		return nil, renderOutput(outputFormat, &tunnel)
	}

	sc.logger.Infof("Created tunnel %s with id %s", tunnel.Name, tunnel.ID)
	return tunnel, nil
}

func (sc *subcommandContext) list(filter *tunnelstore.Filter) ([]*tunnelstore.Tunnel, error) {
	client, err := sc.client()
	if err != nil {
		return nil, err
	}
	return client.ListTunnels(filter)
}

func (sc *subcommandContext) delete(tunnelIDs []uuid.UUID) error {
	forceFlagSet := sc.c.Bool("force")

	client, err := sc.client()
	if err != nil {
		return err
	}

	for _, id := range tunnelIDs {
		tunnel, err := client.GetTunnel(id)
		if err != nil {
			return errors.Wrapf(err, "Can't get tunnel information. Please check tunnel id: %s", tunnel.ID)
		}

		// Check if tunnel DeletedAt field has already been set
		if !tunnel.DeletedAt.IsZero() {
			return fmt.Errorf("Tunnel %s has already been deleted", tunnel.ID)
		}
		// Check if tunnel has existing connections and if force flag is set, cleanup connections
		if len(tunnel.Connections) > 0 {
			if !forceFlagSet {
				return fmt.Errorf("You can not delete tunnel %s because it has active connections. To see connections run the 'list' command. If you believe the tunnel is not active, you can use a -f / --force flag with this command.", id)
			}

			if err := client.CleanupConnections(tunnel.ID); err != nil {
				return errors.Wrapf(err, "Error cleaning up connections for tunnel %s", tunnel.ID)
			}
		}

		if err := client.DeleteTunnel(tunnel.ID); err != nil {
			return errors.Wrapf(err, "Error deleting tunnel %s", tunnel.ID)
		}

		tunnelCredentialsPath, err := sc.tunnelCredentialsPath(tunnel.ID)
		if err != nil {
			sc.logger.Infof("Cannot locate tunnel credentials to delete, error: %v. Please delete the file manually", err)
			return nil
		}

		if err = os.Remove(tunnelCredentialsPath); err != nil {
			sc.logger.Infof("Cannot delete tunnel credentials, error: %v. Please delete the file manually", err)
		}
	}
	return nil
}

func (sc *subcommandContext) run(tunnelID uuid.UUID) error {
	credentials, err := sc.readTunnelCredentials(tunnelID)
	if err != nil {
		return err
	}

	return StartServer(
		sc.c,
		version,
		shutdownC,
		graceShutdownC,
		&origin.NamedTunnelConfig{Auth: *credentials, ID: tunnelID},
		sc.logger,
		sc.isUIEnabled,
	)
}

func (sc *subcommandContext) cleanupConnections(tunnelIDs []uuid.UUID) error {
	client, err := sc.client()
	if err != nil {
		return err
	}
	for _, tunnelID := range tunnelIDs {
		sc.logger.Infof("Cleanup connection for tunnel %s", tunnelID)
		if err := client.CleanupConnections(tunnelID); err != nil {
			sc.logger.Errorf("Error cleaning up connections for tunnel %v, error :%v", tunnelID, err)
		}
	}
	return nil
}

func (sc *subcommandContext) route(tunnelID uuid.UUID, r tunnelstore.Route) (tunnelstore.RouteResult, error) {
	client, err := sc.client()
	if err != nil {
		return nil, err
	}

	return client.RouteTunnel(tunnelID, r)
}

func (sc *subcommandContext) tunnelActive(name string) (*tunnelstore.Tunnel, bool, error) {
	filter := tunnelstore.NewFilter()
	filter.NoDeleted()
	filter.ByName(name)
	tunnels, err := sc.list(filter)
	if err != nil {
		return nil, false, err
	}
	if len(tunnels) == 0 {
		return nil, false, nil
	}
	// There should only be 1 active tunnel for a given name
	return tunnels[0], true, nil
}

// findID parses the input. If it's a UUID, return the UUID.
// Otherwise, assume it's a name, and look up the ID of that tunnel.
func (sc *subcommandContext) findID(input string) (uuid.UUID, error) {
	if u, err := uuid.Parse(input); err == nil {
		return u, nil
	}

	if tunnel, found, err := sc.tunnelActive(input); err != nil {
		return uuid.Nil, err
	} else if found {
		return tunnel.ID, nil
	}

	return uuid.Nil, fmt.Errorf("%s is neither the ID nor the name of any of your tunnels", input)
}

// findIDs is just like mapping `findID` over a slice, but it only uses
// one Tunnelstore API call.
func (sc *subcommandContext) findIDs(inputs []string) ([]uuid.UUID, error) {

	// First, look up all tunnels the user has
	filter := tunnelstore.NewFilter()
	filter.NoDeleted()
	tunnels, err := sc.list(filter)
	if err != nil {
		return nil, err
	}
	// Do the pure list-processing in its own function, so that it can be
	// unit tested easily.
	return findIDs(tunnels, inputs)
}

func findIDs(tunnels []*tunnelstore.Tunnel, inputs []string) ([]uuid.UUID, error) {
	// Put them into a dictionary for faster lookups
	nameToID := make(map[string]uuid.UUID, len(tunnels))
	for _, tunnel := range tunnels {
		nameToID[tunnel.Name] = tunnel.ID
	}

	// For each input, try to find the tunnel ID.
	tunnelIDs := make([]uuid.UUID, len(inputs))
	var badInputs []string
	for i, input := range inputs {
		if id, err := uuid.Parse(input); err == nil {
			tunnelIDs[i] = id
		} else if id, ok := nameToID[input]; ok {
			tunnelIDs[i] = id
		} else {
			badInputs = append(badInputs, input)
		}
	}
	if len(badInputs) > 0 {
		msg := "Please specify either the ID or name of a tunnel. The following inputs were neither: %s"
		return nil, fmt.Errorf(msg, strings.Join(badInputs, ", "))
	}
	return tunnelIDs, nil
}
