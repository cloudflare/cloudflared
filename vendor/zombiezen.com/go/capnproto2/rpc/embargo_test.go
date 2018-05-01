package rpc_test

import (
	"testing"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/rpc/internal/logtransport"
	"zombiezen.com/go/capnproto2/rpc/internal/pipetransport"
	"zombiezen.com/go/capnproto2/rpc/internal/testcapnp"
)

func TestEmbargo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	log := testLogger{t}
	c := rpc.NewConn(p, rpc.ConnLog(log))
	echoSrv := testcapnp.Echoer_ServerToClient(new(Echoer))
	d := rpc.NewConn(q, rpc.MainInterface(echoSrv.Client), rpc.ConnLog(log))
	defer d.Wait()
	defer c.Close()
	client := testcapnp.Echoer{Client: c.Bootstrap(ctx)}
	localCap := testcapnp.CallOrder_ServerToClient(new(CallOrder))

	earlyCall := callseq(ctx, client.Client, 0)
	echo := client.Echo(ctx, func(p testcapnp.Echoer_echo_Params) error {
		return p.SetCap(localCap)
	})
	pipeline := echo.Cap()
	call0 := callseq(ctx, pipeline.Client, 0)
	call1 := callseq(ctx, pipeline.Client, 1)
	_, err := earlyCall.Struct()
	if err != nil {
		t.Errorf("earlyCall error: %v", err)
	}
	call2 := callseq(ctx, pipeline.Client, 2)
	_, err = echo.Struct()
	if err != nil {
		t.Errorf("echo.Get() error: %v", err)
	}
	call3 := callseq(ctx, pipeline.Client, 3)
	call4 := callseq(ctx, pipeline.Client, 4)
	call5 := callseq(ctx, pipeline.Client, 5)

	check := func(promise testcapnp.CallOrder_getCallSequence_Results_Promise, n uint32) {
		r, err := promise.Struct()
		if err != nil {
			t.Errorf("call%d error: %v", n, err)
		}
		if r.N() != n {
			t.Errorf("call%d = %d; want %d", n, r.N(), n)
		}
	}
	check(call0, 0)
	check(call1, 1)
	check(call2, 2)
	check(call3, 3)
	check(call4, 4)
	check(call5, 5)
}

func callseq(c context.Context, client capnp.Client, n uint32) testcapnp.CallOrder_getCallSequence_Results_Promise {
	return testcapnp.CallOrder{Client: client}.GetCallSequence(c, func(p testcapnp.CallOrder_getCallSequence_Params) error {
		p.SetExpected(n)
		return nil
	})
}

type CallOrder struct {
	n uint32
}

func (co *CallOrder) GetCallSequence(call testcapnp.CallOrder_getCallSequence) error {
	call.Results.SetN(co.n)
	co.n++
	return nil
}

type Echoer struct {
	CallOrder
}

func (*Echoer) Echo(call testcapnp.Echoer_echo) error {
	call.Results.SetCap(call.Params.Cap())
	return nil
}
