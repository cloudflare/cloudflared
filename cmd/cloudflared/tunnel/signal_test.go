// +build !windows

package tunnel

import (
	"fmt"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

const tick = 100 * time.Millisecond

var (
	serverErr        = fmt.Errorf("server error")
	shutdownErr      = fmt.Errorf("receive shutdown")
	graceShutdownErr = fmt.Errorf("receive grace shutdown")
)

func channelClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func TestSignalShutdown(t *testing.T) {
	log := zerolog.Nop()

	// Test handling SIGTERM & SIGINT
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		graceShutdownC := make(chan struct{})

		go func(sig syscall.Signal) {
			// sleep for a tick to prevent sending signal before calling waitForSignal
			time.Sleep(tick)
			_ = syscall.Kill(syscall.Getpid(), sig)
		}(sig)

		time.AfterFunc(time.Second, func() {
			select {
			case <-graceShutdownC:
			default:
				close(graceShutdownC)
				t.Fatal("waitForSignal timed out")
			}
		})

		waitForSignal(graceShutdownC, &log)
		assert.True(t, channelClosed(graceShutdownC))
	}
}

func TestWaitForShutdown(t *testing.T) {
	log := zerolog.Nop()

	errC := make(chan error)
	graceShutdownC := make(chan struct{})
	const gracePeriod = 5 * time.Second

	contextCancelled := false
	cancel := func() {
		contextCancelled = true
	}
	var wg sync.WaitGroup

	// on, error stop immediately
	contextCancelled = false
	startTime := time.Now()
	go func() {
		errC <- serverErr
	}()
	err := waitToShutdown(&wg, cancel, errC, graceShutdownC, gracePeriod, &log)
	assert.Equal(t, serverErr, err)
	assert.True(t, contextCancelled)
	assert.False(t, channelClosed(graceShutdownC))
	assert.True(t, time.Now().Sub(startTime) < time.Second) // check that wait ended early

	// on graceful shutdown, ignore error but stop as soon as an error arrives
	contextCancelled = false
	startTime = time.Now()
	go func() {
		close(graceShutdownC)
		time.Sleep(tick)
		errC <- serverErr
	}()
	err = waitToShutdown(&wg, cancel, errC, graceShutdownC, gracePeriod, &log)
	assert.Nil(t, err)
	assert.True(t, contextCancelled)
	assert.True(t, time.Now().Sub(startTime) < time.Second) // check that wait ended early

	// with graceShutdownC closed stop right away without grace period
	contextCancelled = false
	startTime = time.Now()
	err = waitToShutdown(&wg, cancel, errC, graceShutdownC, 0, &log)
	assert.Nil(t, err)
	assert.True(t, contextCancelled)
	assert.True(t, time.Now().Sub(startTime) < time.Second) // check that wait ended early
}
