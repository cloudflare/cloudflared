package updater

import (
	"context"
	"testing"

	"github.com/facebookgo/grace/gracenet"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestDisabledAutoUpdater(t *testing.T) {
	listeners := &gracenet.Net{}
	log := zerolog.Nop()
	autoupdater := NewAutoUpdater(0, listeners, &log)
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
