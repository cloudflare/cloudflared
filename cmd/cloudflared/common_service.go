package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

const (
	defaultTokenFile             = "token"
	tokenPerms       os.FileMode = 0o600
)

func writeTokenToFile(path string, token string) error {
	if _, err := tunnel.ParseToken(token); err != nil {
		return cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	if err := os.WriteFile(path, []byte(token), tokenPerms); err != nil {
		return fmt.Errorf("failed to write token to %s: %v", path, err)
	}

	// If the token file already existed with unrestrictive perms, os.WriteFile
	// above will not update them
	if err := os.Chmod(path, tokenPerms); err != nil {
		return fmt.Errorf("failed to restrict permissions on token file %s: %v", path, err)
	}

	return nil
}

func removeTokenFile(tokenPath string, log *zerolog.Logger) {
	err := os.Remove(tokenPath)

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Msgf("Could not remove service token file at %s: %v", tokenPath, err)
	}
}

func buildArgsForTokenFile(tokenPath string) []string {
	return []string{
		"tunnel", "run", "--token-file", tokenPath,
	}
}

// nolint:unused // This function is used by macos and Windows builds, the unused warning when building for Linux is spurious
func buildArgsForToken(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	token := c.Args().First()
	if _, err := tunnel.ParseToken(token); err != nil {
		return nil, cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	return []string{
		"tunnel", "run", "--token", token,
	}, nil
}

// nolint:unused // This function is used by macos and Windows builds, the unused warning when building for Linux is spurious
func getServiceExtraArgsFromCliArgs(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	if c.NArg() > 0 {
		// currently, we only support extra args for token
		return buildArgsForToken(c, log)
	} else {
		// empty extra args
		return make([]string, 0), nil
	}
}
