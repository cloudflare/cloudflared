package main

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

const (
	defaultTokenFile             = "token"
	tokenPerms       os.FileMode = 0o600
)

func ensureConfigDirExists(configDir string) error {
	if err := os.Mkdir(configDir, 0o755); err != nil { //nolint:gosec // config dir must be traversable by non-root user
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("failed to create config dir at %s: %w", configDir, err)
	}
	return nil
}

func writeTokenToFile(path string, token string) error {
	if _, err := tunnel.ParseToken(token); err != nil {
		return cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	if err := os.WriteFile(path, []byte(token), tokenPerms); err != nil {
		return fmt.Errorf("failed to write token to %s: %w", path, err)
	}

	// If the token file already existed with unrestrictive perms, os.WriteFile
	// above will not update them
	if err := os.Chmod(path, tokenPerms); err != nil {
		return fmt.Errorf("failed to restrict permissions on token file %s: %w", path, err)
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
	return path.Join(configDir, defaultTokenFile)
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

// nolint:unused // This function is used by the Windows build, the unused warning when building for Linux and MacOS is spurious
func buildArgsForToken(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	token := c.Args().First()
	if _, err := tunnel.ParseToken(token); err != nil {
		return nil, cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	return []string{
		"tunnel", "run", "--token", token,
	}, nil
}

// nolint:unused // This function is used by the Windows build, the unused warning when building for Linux and MacOS is spurious
func getServiceExtraArgsFromCliArgs(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	if c.NArg() > 0 {
		// currently, we only support extra args for token
		return buildArgsForToken(c, log)
	} else {
		return []string{
			"tunnel", "run",
		}, nil
	}
}
