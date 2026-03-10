package cliutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-colorable"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cfapi"
	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/credentials"
)

// Error definitions for management token operations
var (
	ErrNoTunnelID      = errors.New("no tunnel ID provided")
	ErrInvalidTunnelID = errors.New("unable to parse provided tunnel id as a valid UUID")
)

// GetManagementToken acquires a management token from Cloudflare API for the specified resource
func GetManagementToken(c *cli.Context, log *zerolog.Logger, res cfapi.ManagementResource, buildInfo *BuildInfo) (string, error) {
	userCreds, err := credentials.Read(c.String(cfdflags.OriginCert), log)
	if err != nil {
		return "", err
	}

	var apiURL string
	if userCreds.IsFEDEndpoint() {
		apiURL = credentials.FedRampBaseApiURL
	} else {
		apiURL = c.String(cfdflags.ApiURL)
	}

	client, err := userCreds.Client(apiURL, buildInfo.UserAgent(), log)
	if err != nil {
		return "", err
	}

	tunnelIDString := c.Args().First()
	if tunnelIDString == "" {
		return "", ErrNoTunnelID
	}
	tunnelID, err := uuid.Parse(tunnelIDString)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidTunnelID, err)
	}

	token, err := client.GetManagementToken(tunnelID, res)
	if err != nil {
		return "", err
	}

	return token, nil
}

// CreateStderrLogger creates a logger that outputs to stderr to avoid interfering with stdout
func CreateStderrLogger(c *cli.Context) *zerolog.Logger {
	level, levelErr := zerolog.ParseLevel(c.String(cfdflags.LogLevel))
	if levelErr != nil {
		level = zerolog.InfoLevel
	}
	var writer io.Writer
	switch c.String(cfdflags.LogFormatOutput) {
	case cfdflags.LogFormatOutputValueJSON:
		// zerolog by default outputs as JSON
		writer = os.Stderr
	case cfdflags.LogFormatOutputValueDefault:
		// "default" and unset use the same logger output format
		fallthrough
	default:
		writer = zerolog.ConsoleWriter{
			Out:        colorable.NewColorable(os.Stderr),
			TimeFormat: time.RFC3339,
		}
	}
	log := zerolog.New(writer).With().Timestamp().Logger().Level(level)
	return &log
}
