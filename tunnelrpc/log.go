package tunnelrpc

import (
	"context"

	"github.com/rs/zerolog"
	"golang.org/x/net/trace"
	"zombiezen.com/go/capnproto2/rpc"
)

// ConnLogger wraps a Zerolog Logger for a connection.
type ConnLogger struct {
	Log *zerolog.Logger
}

func (c ConnLogger) Infof(ctx context.Context, format string, args ...interface{}) {
	c.Log.Info().Msgf(format, args...)
}

func (c ConnLogger) Errorf(ctx context.Context, format string, args ...interface{}) {
	c.Log.Error().Msgf(format, args...)
}

func ConnLog(log *zerolog.Logger) rpc.ConnOption {
	return rpc.ConnLog(ConnLogger{log})
}

// ConnTracer wraps a trace.EventLog for a connection.
type ConnTracer struct {
	Events trace.EventLog
}

func (c ConnTracer) Infof(ctx context.Context, format string, args ...interface{}) {
	c.Events.Printf(format, args...)
}

func (c ConnTracer) Errorf(ctx context.Context, format string, args ...interface{}) {
	c.Events.Errorf(format, args...)
}

func ConnTrace(events trace.EventLog) rpc.ConnOption {
	return rpc.ConnLog(ConnTracer{events})
}
