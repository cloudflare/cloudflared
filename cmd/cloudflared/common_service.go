package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

const (
	defaultTokenFile = "token"
)

func ensureConfigDirExists(configDir string) error {
	if err := os.Mkdir(configDir, 0o755); err != nil { //nolint:gosec // config dir must be traversable by non-root user
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("create config dir at %s: %w", configDir, err)
	}
	return nil
}

func createTokenFileUnix(path string) error {
	const tokenPerms os.FileMode = 0o600
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, tokenPerms) //nolint:gosec // All callers of this function construct path from constant strings or well-known env vars (e.g., $HOME)
	if err != nil {
		return fmt.Errorf("create token file at %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// If the file already existed with unrestrictive permissions, os.OpenFile
	// will not update its permissions, so perform an extra os.Chmod
	if err := os.Chmod(path, tokenPerms); err != nil {
		return fmt.Errorf("chmod token file at %s: %w", path, err)
	}

	return nil
}

// Write out the token file to the configuration directory with the correct
// permissions. Since the method used to restrict the permissions is platform
// dependent, make the function used to restrict the permissions an injectable
// dependency
func writeTokenToFile(path string, token string) error {
	if _, err := tunnel.ParseToken(token); err != nil {
		return cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	if err := createTokenFile(path); err != nil {
		return fmt.Errorf("create token file at %s: %w", path, err)
	}

	// Won't update permissions as file already exists
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return fmt.Errorf("write token to %s: %w", path, err)
	}

	return nil
}

func removeTokenFile(configDir string, log *zerolog.Logger) {
	tp := tokenPath(configDir)
	err := os.Remove(tp)

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Msgf("Could not remove service token file at %s: %v", tp, err)
	}
}

func buildArgsForTokenFile(configDir string) []string {
	return []string{
		"tunnel", "run", "--token-file", tokenPath(configDir),
	}
}

func tokenPath(configDir string) string {
	return filepath.Join(configDir, defaultTokenFile)
}

func writeTokenToConfigDir(c *cli.Context, configDir string) error {
	if err := ensureConfigDirExists(configDir); err != nil {
		return err
	}

	if err := writeTokenToFile(tokenPath(configDir), c.Args().First()); err != nil {
		return err
	}

	return nil
}
