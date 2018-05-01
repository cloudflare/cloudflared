package rpc_test

import (
	"testing"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/rpc/internal/logtransport"
	"zombiezen.com/go/capnproto2/rpc/internal/pipetransport"
	"zombiezen.com/go/capnproto2/rpc/internal/testcapnp"
)

func TestIssue3(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	log := testLogger{t}
	c := rpc.NewConn(p, rpc.ConnLog(log))
	echoSrv := testcapnp.Echoer_ServerToClient(new(SideEffectEchoer))
	d := rpc.NewConn(q, rpc.MainInterface(echoSrv.Client), rpc.ConnLog(log))
	defer d.Wait()
	defer c.Close()
	client := testcapnp.Echoer{Client: c.Bootstrap(ctx)}
	localCap := testcapnp.CallOrder_ServerToClient(new(CallOrder))
	echo := client.Echo(ctx, func(p testcapnp.Echoer_echo_Params) error {
		return p.SetCap(localCap)
	})

	// This should not deadlock.
	_, err := echo.Struct()
	if err != nil {
		t.Error("Echo error:", err)
	}
}

type SideEffectEchoer struct {
	CallOrder
}

func (*SideEffectEchoer) Echo(call testcapnp.Echoer_echo) error {
	call.Params.Cap().GetCallSequence(call.Ctx, nil)
	call.Results.SetCap(call.Params.Cap())
	return nil
}
