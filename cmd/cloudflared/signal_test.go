package main

import (
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const tick = 100 * time.Millisecond

var (
	serverErr        = fmt.Errorf("server error")
	shutdownErr      = fmt.Errorf("receive shutdown")
	graceShutdownErr = fmt.Errorf("receive grace shutdown")
)

func testChannelClosed(t *testing.T, c chan struct{}) {
	select {
	case <-c:
		return
	default:
		t.Fatal("Channel should be readable")
	}
}

func TestWaitForSignal(t *testing.T) {
	// Test handling server error
	errC := make(chan error)
	shutdownC := make(chan struct{})

	go func() {
		errC <- serverErr
	}()

	err := waitForSignal(errC, shutdownC)
	assert.Equal(t, serverErr, err)
	testChannelClosed(t, shutdownC)

	// Test handling SIGTERM & SIGINT
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		errC = make(chan error)
		shutdownC = make(chan struct{})

		go func(shutdownC chan struct{}) {
			<-shutdownC
			errC <- shutdownErr
		}(shutdownC)

		go func(sig syscall.Signal) {
			// sleep for a tick to prevent sending signal before calling waitForSignal
			time.Sleep(tick)
			syscall.Kill(syscall.Getpid(), sig)
		}(sig)

		err = waitForSignal(errC, shutdownC)
		assert.Equal(t, nil, err)
		assert.Equal(t, shutdownErr, <-errC)
		testChannelClosed(t, shutdownC)
	}
}

func TestWaitForSignalWithGraceShutdown(t *testing.T) {
	// Test server returning error
	errC := make(chan error)
	shutdownC := make(chan struct{})
	graceshutdownC := make(chan struct{})

	go func() {
		errC <- serverErr
	}()

	err := waitForSignalWithGraceShutdown(errC, shutdownC, graceshutdownC, tick)
	assert.Equal(t, serverErr, err)
	testChannelClosed(t, shutdownC)
	testChannelClosed(t, graceshutdownC)

	// Test handling SIGTERM & SIGINT
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		//var wg sync.WaitGroup
		errC := make(chan error)
		shutdownC = make(chan struct{})
		graceshutdownC = make(chan struct{})

		go func(shutdownC, graceshutdownC chan struct{}) {
			<-graceshutdownC
			<-shutdownC
			errC <- graceShutdownErr
		}(shutdownC, graceshutdownC)

		go func(sig syscall.Signal) {
			// sleep for a tick to prevent sending signal before calling waitForSignalWithGraceShutdown
			time.Sleep(tick)
			syscall.Kill(syscall.Getpid(), sig)
		}(sig)

		err = waitForSignalWithGraceShutdown(errC, shutdownC, graceshutdownC, tick)
		assert.Equal(t, nil, err)
		assert.Equal(t, graceShutdownErr, <-errC)
		testChannelClosed(t, shutdownC)
		testChannelClosed(t, graceshutdownC)
	}

	// Test handling SIGTERM & SIGINT, server send error before end of grace period
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		errC := make(chan error)
		shutdownC = make(chan struct{})
		graceshutdownC = make(chan struct{})

		go func(shutdownC, graceshutdownC chan struct{}) {
			<-graceshutdownC
			errC <- graceShutdownErr
			<-shutdownC
			errC <- shutdownErr
		}(shutdownC, graceshutdownC)

		go func(sig syscall.Signal) {
			// sleep for a tick to prevent sending signal before calling waitForSignalWithGraceShutdown
			time.Sleep(tick)
			syscall.Kill(syscall.Getpid(), sig)
		}(sig)

		err = waitForSignalWithGraceShutdown(errC, shutdownC, graceshutdownC, tick)
		assert.Equal(t, nil, err)
		assert.Equal(t, shutdownErr, <-errC)
		testChannelClosed(t, shutdownC)
		testChannelClosed(t, graceshutdownC)
	}
}
