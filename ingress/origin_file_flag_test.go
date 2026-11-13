package ingress

import (
	"flag"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/config"
)

func newFileFlagContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	for _, cliFlag := range []cli.Flag{
		&cli.StringFlag{Name: FileFlag},
		&cli.StringFlag{Name: "url"},
		&cli.StringFlag{Name: "unix-socket"},
		&cli.StringFlag{Name: "hostname"},
	} {
		require.NoError(t, cliFlag.Apply(flagSet))
	}
	require.NoError(t, flagSet.Parse(args))
	return cli.NewContext(cli.NewApp(), flagSet, nil)
}

func TestParseCLIIngressFileFlag(t *testing.T) {
	dir := t.TempDir()
	ing, err := parseCLIIngress(newFileFlagContext(t, "--"+FileFlag, dir), false)
	require.NoError(t, err)
	require.Len(t, ing.Rules, 1)

	svc, ok := ing.Rules[0].Service.(*fileServer)
	require.Truef(t, ok, "expected *fileServer, got %T", ing.Rules[0].Service)

	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	require.Equal(t, abs, svc.root)
	require.Equal(t, "file:"+abs, svc.String())
}

func TestFileFlagIsExclusive(t *testing.T) {
	dir := t.TempDir()

	cases := map[string][]string{
		"with --url":         {"--" + FileFlag, dir, "--url", "http://localhost:8080"},
		"with --unix-socket": {"--" + FileFlag, dir, "--unix-socket", "/tmp/sock"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseCLIIngress(newFileFlagContext(t, args...), false)
			require.Error(t, err)
		})
	}
}

func TestValidateIngressFileService(t *testing.T) {
	dir := t.TempDir()
	ing, err := validateIngress([]config.UnvalidatedIngressRule{
		{Service: "file:" + dir},
	}, OriginRequestConfig{})
	require.NoError(t, err)
	require.Len(t, ing.Rules, 1)

	svc, ok := ing.Rules[0].Service.(*fileServer)
	require.Truef(t, ok, "expected *fileServer, got %T", ing.Rules[0].Service)

	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	require.Equal(t, abs, svc.root)
}

func TestValidateIngressFileServiceRejectsEmptyPath(t *testing.T) {
	_, err := validateIngress([]config.UnvalidatedIngressRule{
		{Service: "file:"},
	}, OriginRequestConfig{})
	require.Error(t, err)
}
