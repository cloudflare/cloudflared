package tunnelrpc

//go:generate capnp compile -ogo -I./tunnelrpc/ tunnelrpc.capnp

import (
	log "github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2/rpc"
)

// ConnLogger wraps a logrus *log.Entry for a connection.
type ConnLogger struct {
	Entry *log.Entry
}

func (c ConnLogger) Infof(ctx context.Context, format string, args ...interface{}) {
	c.Entry.Infof(format, args...)
}

func (c ConnLogger) Errorf(ctx context.Context, format string, args ...interface{}) {
	c.Entry.Errorf(format, args...)
}

func ConnLog(log *log.Entry) rpc.ConnOption {
	return rpc.ConnLog(ConnLogger{log})
}
