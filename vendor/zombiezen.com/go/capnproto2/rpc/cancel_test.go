package rpc_test

import (
	"testing"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/rpc/internal/logtransport"
	"zombiezen.com/go/capnproto2/rpc/internal/pipetransport"
	"zombiezen.com/go/capnproto2/rpc/internal/testcapnp"
	"zombiezen.com/go/capnproto2/server"
)

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := testLogger{t}
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	c := rpc.NewConn(p, rpc.ConnLog(log))
	notify := make(chan struct{})
	hanger := testcapnp.Hanger_ServerToClient(Hanger{notify: notify})
	d := rpc.NewConn(q, rpc.MainInterface(hanger.Client), rpc.ConnLog(log))
	defer d.Wait()
	defer c.Close()
	client := testcapnp.Hanger{Client: c.Bootstrap(ctx)}

	subctx, subcancel := context.WithCancel(ctx)
	promise := client.Hang(subctx, nil)
	<-notify
	subcancel()
	_, err := promise.Struct()
	<-notify // test will deadlock if cancel not delivered

	if err != context.Canceled {
		t.Errorf("promise.Get() error: %v; want %v", err, context.Canceled)
	}
}

type Hanger struct {
	notify chan struct{}
}

func (h Hanger) Hang(call testcapnp.Hanger_hang) error {
	server.Ack(call.Options)
	h.notify <- struct{}{}
	<-call.Ctx.Done()
	close(h.notify)
	return nil
}
