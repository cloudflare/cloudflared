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

func BenchmarkPingPong(b *testing.B) {
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	log := testLogger{b}
	c := rpc.NewConn(p, rpc.ConnLog(log))
	d := rpc.NewConn(q, rpc.ConnLog(log), rpc.BootstrapFunc(bootstrapPingPong))
	defer d.Wait()
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := testcapnp.PingPong{Client: c.Bootstrap(ctx)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		promise := client.EchoNum(ctx, func(p testcapnp.PingPong_echoNum_Params) error {
			p.SetN(42)
			return nil
		})
		result, err := promise.Struct()
		if err != nil {
			b.Errorf("EchoNum(42) failed on iteration %d: %v", i, err)
			break
		}
		if result.N() != 42 {
			b.Errorf("EchoNum(42) = %d; want 42", result.N())
			break
		}
	}
}

func bootstrapPingPong(ctx context.Context) (capnp.Client, error) {
	return testcapnp.PingPong_ServerToClient(pingPongServer{}).Client, nil
}

type pingPongServer struct{}

func (pingPongServer) EchoNum(call testcapnp.PingPong_echoNum) error {
	call.Results.SetN(call.Params.N())
	return nil
}
