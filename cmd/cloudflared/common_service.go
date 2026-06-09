package main

import (
	"strconv"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

func buildArgsForToken(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	token := c.Args().First()
	if _, err := tunnel.ParseToken(token); err != nil {
		return nil, cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	return []string{
		"tunnel", "run", "--token", token,
	}, nil
}

func getServiceExtraArgsFromCliArgs(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	if c.NArg() > 0 {
		// currently, we only support extra args for token
		return buildArgsForToken(c, log)
	} else {
		// empty extra args
		return make([]string, 0), nil
	}
}

type serviceFlagSerializer struct {
	name      string
	serialize func(*cli.Context) (string, bool)
}

var serviceFlagSerializers = []serviceFlagSerializer{
	{name: cfdflags.Region, serialize: serializeStringServiceFlag(cfdflags.Region)},
	{name: cfdflags.EdgeIpVersion, serialize: serializeStringServiceFlag(cfdflags.EdgeIpVersion)},
	{name: cfdflags.EdgeBindAddress, serialize: serializeStringServiceFlag(cfdflags.EdgeBindAddress)},
	{name: cfdflags.Protocol, serialize: serializeStringServiceFlag(cfdflags.Protocol)},
	{name: cfdflags.Retries, serialize: serializeIntServiceFlag(cfdflags.Retries)},
	{name: cfdflags.LogLevel, serialize: serializeStringServiceFlag(cfdflags.LogLevel)},
	{name: cfdflags.TransportLogLevel, serialize: serializeStringServiceFlag(cfdflags.TransportLogLevel)},
	{name: cfdflags.LogFile, serialize: serializeStringServiceFlag(cfdflags.LogFile)},
	{name: cfdflags.LogDirectory, serialize: serializeStringServiceFlag(cfdflags.LogDirectory)},
	{name: cfdflags.TraceOutput, serialize: serializeStringServiceFlag(cfdflags.TraceOutput)},
	{name: cfdflags.Metrics, serialize: serializeStringServiceFlag(cfdflags.Metrics)},
	{name: cfdflags.MetricsUpdateFreq, serialize: serializeDurationServiceFlag(cfdflags.MetricsUpdateFreq)},
	{name: cfdflags.GracePeriod, serialize: serializeDurationServiceFlag(cfdflags.GracePeriod)},
	{name: cfdflags.MaxActiveFlows, serialize: serializeIntServiceFlag(cfdflags.MaxActiveFlows)},
	{name: cfdflags.PostQuantum, serialize: serializeBoolServiceFlag(cfdflags.PostQuantum)},
}

func buildServiceFlagArgs(c *cli.Context) []string {
	args := make([]string, 0, len(serviceFlagSerializers))
	for _, flag := range serviceFlagSerializers {
		if arg, ok := flag.serialize(c); ok {
			args = append(args, arg)
		}
	}
	return args
}

func buildServiceRunArgs(c *cli.Context, runArgs []string) []string {
	args := buildServiceFlagArgs(c)
	return append(args, runArgs...)
}

func serializeStringServiceFlag(name string) func(*cli.Context) (string, bool) {
	return func(c *cli.Context) (string, bool) {
		if !c.IsSet(name) {
			return "", false
		}
		value := c.String(name)
		if value == "" {
			return "", false
		}
		return "--" + name + "=" + value, true
	}
}

func serializeIntServiceFlag(name string) func(*cli.Context) (string, bool) {
	return func(c *cli.Context) (string, bool) {
		if !c.IsSet(name) {
			return "", false
		}
		return "--" + name + "=" + strconv.Itoa(c.Int(name)), true
	}
}

func serializeDurationServiceFlag(name string) func(*cli.Context) (string, bool) {
	return func(c *cli.Context) (string, bool) {
		if !c.IsSet(name) {
			return "", false
		}
		return "--" + name + "=" + c.Duration(name).String(), true
	}
}

func serializeBoolServiceFlag(name string) func(*cli.Context) (string, bool) {
	return func(c *cli.Context) (string, bool) {
		if !c.IsSet(name) {
			return "", false
		}
		return "--" + name + "=" + strconv.FormatBool(c.Bool(name)), true
	}
}
