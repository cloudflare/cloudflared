package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func waitForSignal(errC chan error, shutdownC chan struct{}) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case err := <-errC:
		close(shutdownC)
		return err
	case <-signals:
		close(shutdownC)
	case <-shutdownC:
	}
	return nil
}

func waitForSignalWithGraceShutdown(errC chan error, shutdownC, graceShutdownSignal chan struct{}, gracePeriod time.Duration) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case err := <-errC:
		close(graceShutdownSignal)
		close(shutdownC)
		return err
	case <-signals:
		close(graceShutdownSignal)
		logger.Infof("Initiating graceful shutdown...")
		// Unregister signal handler early, so the client can send a second SIGTERM/SIGINT
		// to force shutdown cloudflared
		signal.Stop(signals)
		graceTimerTick := time.Tick(gracePeriod)
		// send close signal via shutdownC when grace period expires or when an
		// error is encountered.
		select {
		case <-graceTimerTick:
		case <-errC:
		}
		close(shutdownC)
	case <-shutdownC:
	}

	return nil
}
