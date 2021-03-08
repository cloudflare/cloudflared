package tunnel

import (
	"fmt"
	"path/filepath"

	"github.com/cloudflare/cloudflared/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

// CredFinder can find the tunnel credentials file.
type CredFinder interface {
	Path() (string, error)
}

// Implements CredFinder and looks for the credentials file at the given
// filepath.
type staticPath struct {
	filePath string
	fs       fileSystem
}

func newStaticPath(filePath string, fs fileSystem) CredFinder {
	return staticPath{
		filePath: filePath,
		fs:       fs,
	}
}

func (a staticPath) Path() (string, error) {
	if a.filePath != "" && a.fs.validFilePath(a.filePath) {
		return a.filePath, nil
	}
	return "", fmt.Errorf("Tunnel credentials file '%s' doesn't exist or is not a file", a.filePath)
}

// Implements CredFinder and looks for the credentials file in several directories
// searching for a file named <id>.json
type searchByID struct {
	id  uuid.UUID
	c   *cli.Context
	log *zerolog.Logger
	fs  fileSystem
}

func newSearchByID(id uuid.UUID, c *cli.Context, log *zerolog.Logger, fs fileSystem) CredFinder {
	return searchByID{
		id:  id,
		c:   c,
		log: log,
		fs:  fs,
	}
}

func (s searchByID) Path() (string, error) {
	originCertPath := s.c.String("origincert")
	originCertLog := s.log.With().
		Str(LogFieldOriginCertPath, originCertPath).
		Logger()

	// Fallback to look for tunnel credentials in the origin cert directory
	if originCertPath, err := findOriginCert(originCertPath, &originCertLog); err == nil {
		originCertDir := filepath.Dir(originCertPath)
		if filePath, err := tunnelFilePath(s.id, originCertDir); err == nil {
			if s.fs.validFilePath(filePath) {
				return filePath, nil
			}
		}
	}

	// Last resort look under default config directories
	for _, configDir := range config.DefaultConfigSearchDirectories() {
		if filePath, err := tunnelFilePath(s.id, configDir); err == nil {
			if s.fs.validFilePath(filePath) {
				return filePath, nil
			}
		}
	}
	return "", fmt.Errorf("tunnel credentials file not found")
}
