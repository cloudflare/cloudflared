package cliutil

import (
	"strings"

	"github.com/rs/zerolog"
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

// LogTable renders lines inside an ASCII table and logs each rendered row.
func LogTable(log *zerolog.Logger, lines []string, title ...string) {
	tableTitle := ""
	if len(title) > 0 {
		tableTitle = title[0]
	}
	for _, line := range asciiBox(lines, tableTitle, 2) {
		if line != "" {
			log.Info().Msg(line)
		}
	}
}

// asciiBox wraps lines in a bordered ASCII box with an optional title row.
func asciiBox(lines []string, title string, padding int) (box []string) {
	maxLen := maxLen(lines, title)
	spacer := strings.Repeat(" ", padding)
	border := "+" + strings.Repeat("-", maxLen+(padding*2)) + "+"
	box = append(box, border)
	if title != "" {
		box = append(box, renderBoxLine(centerLine(title, maxLen), maxLen, spacer))
		box = append(box, border)
	}
	for _, line := range lines {
		box = append(box, renderBoxLine(line, maxLen, spacer))
	}
	box = append(box, border)
	return
}

// renderBoxLine pads a single line so it fills the box width.
func renderBoxLine(line string, maxLen int, spacer string) string {
	return "|" + spacer + line + strings.Repeat(" ", maxLen-len(line)) + spacer + "|"
}

// centerLine pads line evenly so it is centered within width.
func centerLine(line string, width int) string {
	padding := width - len(line)
	leftPadding := padding / 2
	rightPadding := padding - leftPadding
	return strings.Repeat(" ", leftPadding) + line + strings.Repeat(" ", rightPadding)
}

// maxLen returns the longest visible line length including the title.
func maxLen(lines []string, title string) int {
	max := len(title)
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max
}
