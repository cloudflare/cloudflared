package tunnel

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
)

// waitForSignal handles OS signals for graceful shutdown and configuration reload.
// It closes graceShutdownC on SIGTERM/SIGINT to trigger graceful shutdown.
// If reloadC is provided, SIGHUP will send a reload signal instead of being ignored.
func waitForSignal(graceShutdownC chan struct{}, reloadC chan<- struct{}, logger *zerolog.Logger) {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)

	for {
		select {
		case s := <-signals:
			switch s {
			case syscall.SIGHUP:
				if reloadC != nil {
					logger.Info().Msg("Received SIGHUP, triggering configuration reload")
					select {
					case reloadC <- struct{}{}:
					default:
						logger.Warn().Msg("Configuration reload already in progress, skipping")
					}
				} else {
					logger.Info().Msg("Received SIGHUP but hot reload is not enabled for this tunnel")
				}
			case syscall.SIGTERM, syscall.SIGINT:
				logger.Info().Msgf("Initiating graceful shutdown due to signal %s ...", s)
				close(graceShutdownC)
				return
			}
		case <-graceShutdownC:
			return
		}
	}
}
