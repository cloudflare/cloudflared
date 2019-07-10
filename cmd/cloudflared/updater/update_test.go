package updater

import (
	"context"
	"testing"

	"github.com/facebookgo/grace/gracenet"
	"github.com/stretchr/testify/assert"
)

func TestDisabledAutoUpdater(t *testing.T) {
	listeners := &gracenet.Net{}
	autoupdater := NewAutoUpdater(0, listeners)
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
