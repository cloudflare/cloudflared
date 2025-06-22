package cliutil

import (
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

var (
	debugLevelWarning = "At debug level cloudflared will log request URL, method, protocol, content length, as well as, all request and response headers. " +
		"This can expose sensitive information in your logs."

	FlagLogOutput = &cli.StringFlag{
		Name:    flags.LogFormatOutput,
		Usage:   "Output format for the logs (default, json)",
		Value:   flags.LogFormatOutputValueDefault,
		EnvVars: []string{"TUNNEL_MANAGEMENT_OUTPUT", "TUNNEL_LOG_OUTPUT"},
	}
)

func ConfigureLoggingFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    flags.LogLevel,
			Value:   "info",
			Usage:   "Application logging level {debug, info, warn, error, fatal}. " + debugLevelWarning,
			EnvVars: []string{"TUNNEL_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    flags.TransportLogLevel,
			Aliases: []string{"proto-loglevel"}, // This flag used to be called proto-loglevel
			Value:   "info",
			Usage:   "Transport logging level(previously called protocol logging level) {debug, info, warn, error, fatal}",
			EnvVars: []string{"TUNNEL_PROTO_LOGLEVEL", "TUNNEL_TRANSPORT_LOGLEVEL"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    flags.LogFile,
			Usage:   "Save application log to this file for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGFILE"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    flags.LogDirectory,
			Usage:   "Save application log to this directory for reporting issues.",
			EnvVars: []string{"TUNNEL_LOGDIRECTORY"},
			Hidden:  shouldHide,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    flags.TraceOutput,
			Usage:   "Name of trace output file, generated when cloudflared stops.",
			EnvVars: []string{"TUNNEL_TRACE_OUTPUT"},
			Hidden:  shouldHide,
		}),
		FlagLogOutput,
	}
}
