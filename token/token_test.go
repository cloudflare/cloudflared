// +build linux

package token

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalHandler(t *testing.T) {
	sigHandler := signalHandler{signals: []os.Signal{syscall.SIGUSR1}}
	handlerRan := false
	done := make(chan struct{})
	timer := time.NewTimer(time.Second)
	sigHandler.register(func() {
		handlerRan = true
		done <- struct{}{}
	})

	p, err := os.FindProcess(os.Getpid())
	require.Nil(t, err)
	p.Signal(syscall.SIGUSR1)

	// Blocks for up to one second to make sure the handler callback runs before the assert.
	select {
	case <-done:
		assert.True(t, handlerRan)
	case <-timer.C:
		t.Fail()
	}
	sigHandler.deregister()
}

func TestSignalHandlerClose(t *testing.T) {
	sigHandler := signalHandler{signals: []os.Signal{syscall.SIGUSR1}}
	done := make(chan struct{})
	timer := time.NewTimer(time.Second)
	sigHandler.register(func() { done <- struct{}{} })
	sigHandler.deregister()

	p, err := os.FindProcess(os.Getpid())
	require.Nil(t, err)
	p.Signal(syscall.SIGUSR1)
	select {
	case <-done:
		t.Fail()
	case <-timer.C:
	}
}
