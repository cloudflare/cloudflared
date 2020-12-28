package tunnel

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

const LogFieldSignal = "signal"

// waitForSignal notifies all routines to shutdownC immediately by closing the
// shutdownC when one of the routines in main exits, or when this process receives
// SIGTERM/SIGINT
func waitForSignal(errC chan error, shutdownC chan struct{}, log *zerolog.Logger) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case err := <-errC:
		log.Err(err).Msg("terminating due to error")
		close(shutdownC)
		return err
	case s := <-signals:
		log.Info().Str(LogFieldSignal, s.String()).Msg("terminating due to signal")
		close(shutdownC)
	case <-shutdownC:
	}
	return nil
}

// waitForSignalWithGraceShutdown notifies all routines to shutdown immediately
// by closing the shutdownC when one of the routines in main exits.
// When this process recieves SIGTERM/SIGINT, it closes the graceShutdownC to
// notify certain routines to start graceful shutdown. When grace period is over,
// or when some routine exits, it notifies the rest of the routines to shutdown
// immediately by closing shutdownC.
// In the case of handling commands from Windows Service Manager, closing graceShutdownC
// initiate graceful shutdown.
func waitForSignalWithGraceShutdown(errC chan error,
	shutdownC, graceShutdownC chan struct{},
	gracePeriod time.Duration,
	logger *zerolog.Logger,
) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case err := <-errC:
		logger.Info().Msgf("Initiating graceful shutdown due to %v ...", err)
		close(graceShutdownC)
		close(shutdownC)
		return err
	case s := <-signals:
		logger.Info().Msgf("Initiating graceful shutdown due to signal %s ...", s)
		close(graceShutdownC)
		waitForGracePeriod(signals, errC, shutdownC, gracePeriod)
	case <-graceShutdownC:
		waitForGracePeriod(signals, errC, shutdownC, gracePeriod)
	case <-shutdownC:
		close(graceShutdownC)
	}

	return nil
}

func waitForGracePeriod(signals chan os.Signal,
	errC chan error,
	shutdownC chan struct{},
	gracePeriod time.Duration,
) {
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
}
