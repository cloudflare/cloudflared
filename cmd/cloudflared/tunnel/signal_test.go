//go:build !windows

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

		waitForSignal(graceShutdownC, nil, &log)
		assert.True(t, channelClosed(graceShutdownC))
	}
}

func TestSignalSIGHUP_WithReloadChannel(t *testing.T) {
	log := zerolog.Nop()

	graceShutdownC := make(chan struct{})
	reloadC := make(chan struct{}, 1)

	go func() {
		// sleep for a tick to prevent sending signal before calling waitForSignal
		time.Sleep(tick)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
		// Give time for signal to be processed
		time.Sleep(tick)
		// Send SIGTERM to exit waitForSignal
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	time.AfterFunc(time.Second, func() {
		select {
		case <-graceShutdownC:
		default:
			close(graceShutdownC)
			t.Fatal("waitForSignal timed out")
		}
	})

	waitForSignal(graceShutdownC, reloadC, &log)

	// Check that reload signal was received
	select {
	case <-reloadC:
		// Expected - SIGHUP should trigger reload
	default:
		t.Fatal("Expected reload channel to receive signal from SIGHUP")
	}
}

func TestSignalSIGHUP_WithoutReloadChannel(t *testing.T) {
	log := zerolog.Nop()

	graceShutdownC := make(chan struct{})

	go func() {
		// sleep for a tick to prevent sending signal before calling waitForSignal
		time.Sleep(tick)
		// Send SIGHUP without reload channel - should be ignored
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
		time.Sleep(tick)
		// Send SIGTERM to exit waitForSignal
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	time.AfterFunc(time.Second, func() {
		select {
		case <-graceShutdownC:
		default:
			close(graceShutdownC)
			t.Fatal("waitForSignal timed out")
		}
	})

	// Should complete without panic or deadlock
	waitForSignal(graceShutdownC, nil, &log)
	assert.True(t, channelClosed(graceShutdownC))
}

func TestSignalSIGHUP_ReloadInProgress(t *testing.T) {
	log := zerolog.Nop()

	graceShutdownC := make(chan struct{})
	// Create buffered channel and fill it
	reloadC := make(chan struct{}, 1)
	reloadC <- struct{}{} // Pre-fill to simulate reload in progress

	go func() {
		// sleep for a tick to prevent sending signal before calling waitForSignal
		time.Sleep(tick)
		// Send SIGHUP while reload is "in progress"
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
		time.Sleep(tick)
		// Send SIGTERM to exit waitForSignal
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	time.AfterFunc(time.Second, func() {
		select {
		case <-graceShutdownC:
		default:
			close(graceShutdownC)
			t.Fatal("waitForSignal timed out")
		}
	})

	// Should complete without blocking (non-blocking send)
	waitForSignal(graceShutdownC, reloadC, &log)

	// Channel should still have exactly one signal (the pre-filled one)
	select {
	case <-reloadC:
		// Expected - drain the one signal
	default:
		t.Fatal("Expected reload channel to have signal")
	}

	// Should be empty now
	select {
	case <-reloadC:
		t.Fatal("Expected reload channel to be empty after draining")
	default:
		// Expected - channel is empty
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
