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

func TestPromisedCapability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	log := testLogger{t}
	c := rpc.NewConn(p, rpc.ConnLog(log))
	delay := make(chan struct{})
	echoSrv := testcapnp.Echoer_ServerToClient(&DelayEchoer{delay: delay})
	d := rpc.NewConn(q, rpc.MainInterface(echoSrv.Client), rpc.ConnLog(log))
	defer d.Wait()
	defer c.Close()
	client := testcapnp.Echoer{Client: c.Bootstrap(ctx)}

	echo := client.Echo(ctx, func(p testcapnp.Echoer_echo_Params) error {
		return p.SetCap(testcapnp.CallOrder{Client: client.Client})
	})
	pipeline := echo.Cap()
	call0 := callseq(ctx, pipeline.Client, 0)
	call1 := callseq(ctx, pipeline.Client, 1)
	close(delay)

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
}

type DelayEchoer struct {
	Echoer
	delay chan struct{}
}

func (de *DelayEchoer) Echo(call testcapnp.Echoer_echo) error {
	server.Ack(call.Options)
	<-de.delay
	return de.Echoer.Echo(call)
}
