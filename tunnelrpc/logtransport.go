// Package logtransport provides a transport that logs all of its messages.
package tunnelrpc

import (
	"bytes"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2/encoding/text"
	"zombiezen.com/go/capnproto2/rpc"
	rpccapnp "zombiezen.com/go/capnproto2/std/capnp/rpc"
)

type transport struct {
	rpc.Transport
	l *log.Entry
}

// New creates a new logger that proxies messages to and from t and
// logs them to l.  If l is nil, then the log package's default
// logger is used.
func NewTransportLogger(l *log.Entry, t rpc.Transport) rpc.Transport {
	return &transport{Transport: t, l: l}
}

func (t *transport) SendMessage(ctx context.Context, msg rpccapnp.Message) error {
	t.l.Debugf("tx %s", formatMsg(msg))
	return t.Transport.SendMessage(ctx, msg)
}

func (t *transport) RecvMessage(ctx context.Context) (rpccapnp.Message, error) {
	msg, err := t.Transport.RecvMessage(ctx)
	if err != nil {
		t.l.WithError(err).Debug("rx error")
		return msg, err
	}
	t.l.Debugf("rx %s", formatMsg(msg))
	return msg, nil
}

func formatMsg(m rpccapnp.Message) string {
	var buf bytes.Buffer
	text.NewEncoder(&buf).Encode(0x91b79f1f808db032, m.Struct)
	return buf.String()
}
