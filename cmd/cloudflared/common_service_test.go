package main

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

func TestBuildServiceFlagArgs(t *testing.T) {
	cliCtx := newServiceTestContext(t)

	require.NoError(t, cliCtx.Set(cfdflags.Region, "us"))
	require.NoError(t, cliCtx.Set(cfdflags.EdgeIpVersion, "6"))
	require.NoError(t, cliCtx.Set(cfdflags.Retries, "3"))
	require.NoError(t, cliCtx.Set(cfdflags.GracePeriod, "10s"))
	require.NoError(t, cliCtx.Set(cfdflags.PostQuantum, "true"))

	require.Equal(t, []string{
		"--region=us",
		"--edge-ip-version=6",
		"--retries=3",
		"--grace-period=10s",
		"--post-quantum=true",
	}, buildServiceFlagArgs(cliCtx))
}

func TestBuildServiceRunArgsAppendsTunnelCommand(t *testing.T) {
	cliCtx := newServiceTestContext(t)

	require.NoError(t, cliCtx.Set(cfdflags.EdgeIpVersion, "6"))
	require.NoError(t, cliCtx.Set(cfdflags.GracePeriod, "15s"))
	require.NoError(t, cliCtx.Set(cfdflags.NoAutoUpdate, "true"))
	require.NoError(t, cliCtx.Set(cfdflags.AutoUpdateFreq, "24h"))

	got := buildServiceRunArgs(cliCtx, []string{"--config", "/etc/cloudflared/config.yml", "tunnel", "run"})

	require.Equal(t, []string{
		"--edge-ip-version=6",
		"--grace-period=15s",
		"--config", "/etc/cloudflared/config.yml", "tunnel", "run",
	}, got)
}

func newServiceTestContext(t *testing.T) *cli.Context {
	t.Helper()

	flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSet.String(cfdflags.Region, "", "")
	flagSet.String(cfdflags.EdgeIpVersion, "", "")
	flagSet.String(cfdflags.EdgeBindAddress, "", "")
	flagSet.String(cfdflags.Protocol, "", "")
	flagSet.Int(cfdflags.Retries, 0, "")
	flagSet.String(cfdflags.LogLevel, "", "")
	flagSet.String(cfdflags.TransportLogLevel, "", "")
	flagSet.String(cfdflags.LogFile, "", "")
	flagSet.String(cfdflags.LogDirectory, "", "")
	flagSet.String(cfdflags.TraceOutput, "", "")
	flagSet.String(cfdflags.Metrics, "", "")
	flagSet.Duration(cfdflags.MetricsUpdateFreq, 0, "")
	flagSet.Duration(cfdflags.GracePeriod, 0, "")
	flagSet.Int(cfdflags.MaxActiveFlows, 0, "")
	flagSet.Bool(cfdflags.PostQuantum, false, "")
	flagSet.Bool(cfdflags.NoAutoUpdate, false, "")
	flagSet.Duration(cfdflags.AutoUpdateFreq, 24*time.Hour, "")

	return cli.NewContext(cli.NewApp(), flagSet, nil)
}
