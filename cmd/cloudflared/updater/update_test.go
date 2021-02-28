package updater

import (
	"context"
	"flag"
	"testing"

	"github.com/facebookgo/grace/gracenet"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
)

func TestDisabledAutoUpdater(t *testing.T) {
	listeners := &gracenet.Net{}
	log := zerolog.Nop()
	autoupdater := NewAutoUpdater(false, 0, listeners, &log)
	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error)
	go func() {
		errC <- autoupdater.Run(ctx)
	}()

	assert.False(t, autoupdater.configurable.enabled)
	assert.Equal(t, DefaultCheckUpdateFreq, autoupdater.configurable.freq)

	cancel()
	// Make sure that autoupdater terminates after canceling the context
	assert.Equal(t, context.Canceled, <-errC)
}

func TestCheckInWithUpdater(t *testing.T) {
	flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)

	warningChecker := StartWarningCheck(cliCtx)
	warning := warningChecker.getWarning()
	// Assuming this runs either on a release or development version, then the Worker will never have anything to tell us.
	assert.Empty(t, warning)
}
