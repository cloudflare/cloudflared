package supervisor

import (
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

type ConnAwareLogger struct {
	tracker *tunnelstate.ConnTracker
	logger  *zerolog.Logger
}

func NewConnAwareLogger(logger *zerolog.Logger, tracker *tunnelstate.ConnTracker, observer *connection.Observer) *ConnAwareLogger {
	connAwareLogger := &ConnAwareLogger{
		tracker: tracker,
		logger:  logger,
	}

	observer.RegisterSink(connAwareLogger.tracker)

	return connAwareLogger
}

func (c *ConnAwareLogger) ReplaceLogger(logger *zerolog.Logger) *ConnAwareLogger {
	return &ConnAwareLogger{
		tracker: c.tracker,
		logger:  logger,
	}
}

func (c *ConnAwareLogger) ConnAwareLogger() *zerolog.Event {
	if c.tracker.CountActiveConns() == 0 {
		return c.logger.Error()
	}
	return c.logger.Warn()
}

func (c *ConnAwareLogger) Logger() *zerolog.Logger {
	return c.logger
}
