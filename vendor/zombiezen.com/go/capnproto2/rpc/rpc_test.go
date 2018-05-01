package rpc_test

import (
	"errors"
	"flag"
	"fmt"
	"testing"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/rpc"
	"zombiezen.com/go/capnproto2/rpc/internal/logtransport"
	"zombiezen.com/go/capnproto2/rpc/internal/pipetransport"
	rpccapnp "zombiezen.com/go/capnproto2/std/capnp/rpc"
)

const (
	interfaceID       uint64 = 0xa7317bd7216570aa
	methodID          uint16 = 9
	bootstrapExportID uint32 = 84
)

var logMessages = flag.Bool("logmessages", false, "whether to log the transport in tests.  Messages are always from client to server.")

type testLogger struct {
	t interface {
		Logf(format string, args ...interface{})
	}
}

func (l testLogger) Infof(ctx context.Context, format string, args ...interface{}) {
	l.t.Logf("conn log: "+format, args...)
}

func (l testLogger) Errorf(ctx context.Context, format string, args ...interface{}) {
	l.t.Logf("conn log: "+format, args...)
}

func newUnpairedConn(t *testing.T, options ...rpc.ConnOption) (*rpc.Conn, rpc.Transport) {
	p, q := pipetransport.New()
	if *logMessages {
		p = logtransport.New(nil, p)
	}
	newopts := make([]rpc.ConnOption, len(options), len(options)+1)
	copy(newopts, options)
	newopts = append(newopts, rpc.ConnLog(testLogger{t}))
	c := rpc.NewConn(p, newopts...)
	return c, q
}

func TestBootstrap(t *testing.T) {
	ctx := context.Background()
	conn, p := newUnpairedConn(t)
	defer conn.Close()
	defer p.Close()

	clientCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	readBootstrap(t, clientCtx, conn, p)
}

func readBootstrap(t *testing.T, ctx context.Context, conn *rpc.Conn, p rpc.Transport) (client capnp.Client, questionID uint32) {
	clientCh := make(chan capnp.Client, 1)
	go func() {
		clientCh <- conn.Bootstrap(ctx)
	}()

	msg, err := p.RecvMessage(ctx)
	if err != nil {
		t.Fatal("Read Bootstrap failed:", err)
	}
	if msg.Which() != rpccapnp.Message_Which_bootstrap {
		t.Fatalf("Received %v message from bootstrap, want Message_Which_bootstrap", msg.Which())
	}
	boot, err := msg.Bootstrap()
	if err != nil {
		t.Fatal("Read Bootstrap failed:", err)
	}
	questionID = boot.QuestionId()
	// If this deadlocks, then Bootstrap isn't using a promised client.
	client = <-clientCh
	if client == nil {
		t.Fatal("Bootstrap client is nil")
	}
	return
}

func TestBootstrapFulfilledSenderHosted(t *testing.T) {
	testBootstrapFulfilled(t, false)
}

func TestBootstrapFulfilledSenderPromise(t *testing.T) {
	testBootstrapFulfilled(t, true)
}

func testBootstrapFulfilled(t *testing.T, resultIsPromise bool) {
	ctx := context.Background()
	conn, p := newUnpairedConn(t)
	defer conn.Close()
	defer p.Close()

	clientCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	bootstrapAndFulfill(t, clientCtx, conn, p, resultIsPromise)
}

// Receive a Finish message for the given question ID.
//
// Immediately releases any capabilities in the message.
//
// An error is returned if any of the following occur:
//
// * An error occurs when reading the message
// * The message is not of type `Finish`
// * The message's question ID is not equal to `questionID`.
//
// Parameters:
//
// ctx: The context to be used when sending the message.
// p: The rpc.Transport to send the message on.
// questionID: The expected question ID.
func recvFinish(ctx context.Context, p rpc.Transport, questionID uint32) error {
	if finish, err := p.RecvMessage(ctx); err != nil {
		return err
	} else if finish.Which() != rpccapnp.Message_Which_finish {
		return fmt.Errorf("message sent is %v; want Message_Which_finish", finish.Which())
	} else {
		f, err := finish.Finish()
		if err != nil {
			return err
		}
		if id := f.QuestionId(); id != questionID {
			return fmt.Errorf("finish question ID is %d; want %d", id, questionID)
		}
		if f.ReleaseResultCaps() {
			return fmt.Errorf("finish released bootstrap capability")
		}
	}
	return nil
}

// Send a Return message with a single capability to a bootstrap interface in
// its payload. Returns any error that occurs.
//
// Parameters:
//
// ctx: The context to be used when sending the message.
// p: The rpc.Transport to send the message on.
// answerId: The message's answerId.
// isPromise: If this is true, the capability in the response will be of type
//   senderPromise, otherwise it will be of type senderHosted.
func sendBootstrapReturn(ctx context.Context, p rpc.Transport, answerId uint32, isPromise bool) error {
	return sendMessage(ctx, p, func(msg rpccapnp.Message) error {
		ret, err := msg.NewReturn()
		if err != nil {
			return err
		}
		ret.SetAnswerId(answerId)
		payload, err := ret.NewResults()
		if err != nil {
			return err
		}
		payload.SetContent(capnp.NewInterface(msg.Segment(), 0))
		capTable, err := payload.NewCapTable(1)
		if err != nil {
			return err
		}
		if isPromise {
			capTable.At(0).SetSenderPromise(bootstrapExportID)
		} else {
			capTable.At(0).SetSenderHosted(bootstrapExportID)
		}
		return nil
	})
}

func bootstrapAndFulfill(t *testing.T, ctx context.Context, conn *rpc.Conn, p rpc.Transport, resultIsPromise bool) capnp.Client {
	client, bootstrapID := readBootstrap(t, ctx, conn, p)
	if err := sendBootstrapReturn(ctx, p, bootstrapID, resultIsPromise); err != nil {
		t.Fatalf("sendBootstrapReturn: %v", err)
	}
	if err := recvFinish(ctx, p, bootstrapID); err != nil {
		t.Fatalf("recvFinish: %v", err)
	}
	return client
}

func TestCallOnPromisedAnswer(t *testing.T) {
	ctx := context.Background()
	conn, p := newUnpairedConn(t)
	defer conn.Close()
	defer p.Close()
	client, bootstrapID := readBootstrap(t, ctx, conn, p)

	readDone := startRecvMessage(p)
	client.Call(&capnp.Call{
		Ctx: ctx,
		Method: capnp.Method{
			InterfaceID: interfaceID,
			MethodID:    methodID,
		},
		ParamsSize: capnp.ObjectSize{DataSize: 8},
		ParamsFunc: func(s capnp.Struct) error {
			s.SetUint64(0, 42)
			return nil
		},
	})
	read := <-readDone

	if read.err != nil {
		t.Fatal("Reading failed:", read.err)
	}
	if read.msg.Which() != rpccapnp.Message_Which_call {
		t.Fatalf("Conn sent %v message, want Message_Which_call", read.msg.Which())
	}
	call, err := read.msg.Call()
	if err != nil {
		t.Fatal("call error:", err)
	}
	if target, err := call.Target(); err != nil {
		t.Fatal(err)
	} else if target.Which() == rpccapnp.MessageTarget_Which_promisedAnswer {
		if pa, err := target.PromisedAnswer(); err != nil {
			t.Error("call.target.promisedAnswer error:", err)
		} else {
			if qid := pa.QuestionId(); qid != bootstrapID {
				t.Errorf("Target question ID = %d; want %d", qid, bootstrapID)
			}
			// TODO(light): allow no-ops
			if xform, err := pa.Transform(); err != nil {
				t.Error("call.target.promisedAnswer.transform error:", err)
			} else if xform.Len() != 0 {
				t.Error("Target transform is non-empty")
			}
		}
	} else {
		t.Errorf("Target is %v, want MessageTarget_Which_promisedAnswer", target.Which())
	}
	if id := call.InterfaceId(); id != interfaceID {
		t.Errorf("Interface ID = %x; want %x", id, interfaceID)
	}
	if id := call.MethodId(); id != methodID {
		t.Errorf("Method ID = %d; want %d", id, methodID)
	}
	if params, err := call.Params(); err != nil {
		t.Error("call.params error:", err)
	} else {
		if content, err := params.Content(); err != nil {
			t.Error("call.params.content error:", err)
		} else if x := capnp.ToStruct(content).Uint64(0); x != 42 {
			t.Errorf("Params content value = %d; want %d", x, 42)
		}
	}
	sendResultsTo := call.SendResultsTo()
	if sendResultsTo.Which() != rpccapnp.Call_sendResultsTo_Which_caller {
		t.Errorf("Send results to %v; want caller", sendResultsTo.Which())
	}
}

func TestCallOnExportId_BootstrapIsPromise(t *testing.T) {
	testCallOnExportId(t, true)
}

func TestCallOnExportId_BootstrapIsHosted(t *testing.T) {
	testCallOnExportId(t, false)
}

func testCallOnExportId(t *testing.T, bootstrapIsPromise bool) {
	ctx := context.Background()
	conn, p := newUnpairedConn(t)
	defer conn.Close()
	defer p.Close()
	client := bootstrapAndFulfill(t, ctx, conn, p, bootstrapIsPromise)

	readDone := startRecvMessage(p)
	client.Call(&capnp.Call{
		Ctx: ctx,
		Method: capnp.Method{
			InterfaceID: interfaceID,
			MethodID:    methodID,
		},
		ParamsSize: capnp.ObjectSize{DataSize: 8},
		ParamsFunc: func(s capnp.Struct) error {
			s.SetUint64(0, 42)
			return nil
		},
	})
	read := <-readDone

	if read.err != nil {
		t.Fatal("Reading failed:", read.err)
	}
	call, err := read.msg.Call()
	if err != nil {
		t.Fatal("call error:", err)
	}
	if read.msg.Which() != rpccapnp.Message_Which_call {
		t.Fatalf("Conn sent %v message, want Message_Which_call", read.msg.Which())
	}
	if target, err := call.Target(); err != nil {
		t.Error("call.target error:", err)
	} else if target.Which() != rpccapnp.MessageTarget_Which_importedCap {
		t.Errorf("Target is %v, want MessageTarget_Which_importedCap", target.Which())
	} else {
		if id := target.ImportedCap(); id != bootstrapExportID {
			t.Errorf("Target imported cap = %d; want %d", id, bootstrapExportID)
		}
	}
	if id := call.InterfaceId(); id != interfaceID {
		t.Errorf("Interface ID = %x; want %x", id, interfaceID)
	}
	if id := call.MethodId(); id != methodID {
		t.Errorf("Method ID = %d; want %d", id, methodID)
	}
	if params, err := call.Params(); err != nil {
		t.Error("call.params error:", err)
	} else if content, err := params.Content(); err != nil {
		t.Error("call.params.content error:", err)
	} else if x := capnp.ToStruct(content).Uint64(0); x != 42 {
		t.Errorf("Params content value = %d; want %d", x, 42)
	}
	if sendResultsTo := call.SendResultsTo(); err != nil {
		t.Error("call.sendResultsTo error:", err)
	} else if sendResultsTo.Which() != rpccapnp.Call_sendResultsTo_Which_caller {
		t.Errorf("Send results to %v; want caller", sendResultsTo.Which())
	}
}

func TestMainInterface(t *testing.T) {
	main := mockClient()
	conn, p := newUnpairedConn(t, rpc.MainInterface(main))
	defer conn.Close()
	defer p.Close()

	bootstrapRoundtrip(t, p)
}

func bootstrapRoundtrip(t *testing.T, p rpc.Transport) (importID, questionID uint32) {
	questionID = 54
	err := sendMessage(context.TODO(), p, func(msg rpccapnp.Message) error {
		bootstrap, err := msg.NewBootstrap()
		if err != nil {
			return err
		}
		bootstrap.SetQuestionId(questionID)
		return nil
	})
	if err != nil {
		t.Fatal("Write Bootstrap failed:", err)
	}
	msg, err := p.RecvMessage(context.TODO())
	if err != nil {
		t.Fatal("Read Bootstrap response failed:", err)
	}

	if msg.Which() != rpccapnp.Message_Which_return {
		t.Fatalf("Conn sent %v message, want Message_Which_return", msg.Which())
	}
	ret, err := msg.Return()
	if err != nil {
		t.Fatal("return error:", err)
	}
	if id := ret.AnswerId(); id != questionID {
		t.Fatalf("msg.Return().AnswerId() = %d; want %d", id, questionID)
	}
	if ret.Which() != rpccapnp.Return_Which_results {
		t.Fatalf("msg.Return().Which() = %v; want Return_Which_results", ret.Which())
	}
	payload, err := ret.Results()
	if err != nil {
		t.Fatal("return.results error:", err)
	}
	content, err := payload.ContentPtr()
	if err != nil {
		t.Fatal("return.results.content error:", err)
	}
	in := content.Interface()
	if !in.IsValid() {
		t.Fatalf("Result payload contains %v; want interface", content)
	}
	capIdx := int(in.Capability())
	capTable, err := payload.CapTable()
	if err != nil {
		t.Fatal("return.results.capTable error:", err)
	}
	if n := capTable.Len(); capIdx >= n {
		t.Fatalf("Payload capTable has size %d, but capability index = %d", n, capIdx)
	}
	if cw := capTable.At(capIdx).Which(); cw != rpccapnp.CapDescriptor_Which_senderHosted {
		t.Fatalf("Capability type is %d; want CapDescriptor_Which_senderHosted", cw)
	}
	return capTable.At(capIdx).SenderHosted(), questionID
}

func TestReceiveCallOnPromisedAnswer(t *testing.T) {
	const questionID = 999
	called := false
	main := stubClient(func(ctx context.Context, params capnp.Struct) (capnp.Struct, error) {
		msg, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			return capnp.Struct{}, err
		}
		result, err := capnp.NewStruct(s, capnp.ObjectSize{})
		if err != nil {
			return capnp.Struct{}, err
		}
		called = true
		if err := msg.SetRoot(result); err != nil {
			return capnp.Struct{}, err
		}
		return result, nil
	})
	conn, p := newUnpairedConn(t, rpc.MainInterface(main))
	defer conn.Close()
	defer p.Close()
	_, bootqID := bootstrapRoundtrip(t, p)

	err := sendMessage(context.TODO(), p, func(msg rpccapnp.Message) error {
		call, err := msg.NewCall()
		if err != nil {
			return err
		}
		call.SetQuestionId(questionID)
		call.SetInterfaceId(interfaceID)
		call.SetMethodId(methodID)
		target, err := call.NewTarget()
		if err != nil {
			return err
		}
		pa, err := target.NewPromisedAnswer()
		if err != nil {
			return err
		}
		pa.SetQuestionId(bootqID)
		payload, err := call.NewParams()
		if err != nil {
			return err
		}
		content, err := capnp.NewStruct(msg.Segment(), capnp.ObjectSize{})
		if err != nil {
			return err
		}
		payload.SetContent(content)
		return nil
	})
	if err != nil {
		t.Fatal("Call message failed:", err)
	}
	retmsg, err := p.RecvMessage(context.TODO())
	if err != nil {
		t.Fatal("Read Call return failed:", err)
	}

	if !called {
		t.Error("interface not called")
	}
	if retmsg.Which() != rpccapnp.Message_Which_return {
		t.Fatalf("Return message is %v; want %v", retmsg.Which(), rpccapnp.Message_Which_return)
	}
	ret, err := retmsg.Return()
	if err != nil {
		t.Fatal("return error:", err)
	}
	if id := ret.AnswerId(); id != questionID {
		t.Errorf("Return.answerId = %d; want %d", id, questionID)
	}
	if ret.Which() == rpccapnp.Return_Which_results {
		// TODO(light)
	} else if ret.Which() == rpccapnp.Return_Which_exception {
		exc, _ := ret.Exception()
		reason, _ := exc.Reason()
		t.Error("Return.exception:", reason)
	} else {
		t.Errorf("Return.Which() = %v; want %v", ret.Which(), rpccapnp.Return_Which_results)
	}
}

func TestReceiveCallOnExport(t *testing.T) {
	const questionID = 999
	called := false
	main := stubClient(func(ctx context.Context, params capnp.Struct) (capnp.Struct, error) {
		msg, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			return capnp.Struct{}, err
		}
		result, err := capnp.NewStruct(s, capnp.ObjectSize{})
		if err != nil {
			return capnp.Struct{}, err
		}
		called = true
		if err := msg.SetRoot(result); err != nil {
			return capnp.Struct{}, err
		}
		return result, nil
	})
	conn, p := newUnpairedConn(t, rpc.MainInterface(main))
	defer conn.Close()
	defer p.Close()
	importID := sendBootstrapAndFinish(t, p)

	err := sendMessage(context.TODO(), p, func(msg rpccapnp.Message) error {
		call, err := msg.NewCall()
		if err != nil {
			return err
		}
		call.SetQuestionId(questionID)
		call.SetInterfaceId(interfaceID)
		call.SetMethodId(methodID)
		target, err := call.NewTarget()
		if err != nil {
			return err
		}
		target.SetImportedCap(importID)
		call.SetTarget(target)
		payload, err := call.NewParams()
		if err != nil {
			return err
		}
		content, err := capnp.NewStruct(msg.Segment(), capnp.ObjectSize{})
		if err != nil {
			return err
		}
		payload.SetContent(content)
		return nil
	})
	if err != nil {
		t.Fatal("Call message failed:", err)
	}
	retmsg, err := p.RecvMessage(context.TODO())
	if err != nil {
		t.Fatal("Read Call return failed:", err)
	}

	if !called {
		t.Error("interface not called")
	}
	if retmsg.Which() != rpccapnp.Message_Which_return {
		t.Fatalf("Return message is %v; want %v", retmsg.Which(), rpccapnp.Message_Which_return)
	}
	ret, err := retmsg.Return()
	if err != nil {
		t.Fatal("return error:", err)
	}
	if id := ret.AnswerId(); id != questionID {
		t.Errorf("Return.answerId = %d; want %d", id, questionID)
	}
	if ret.Which() == rpccapnp.Return_Which_results {
		// TODO(light)
	} else if ret.Which() == rpccapnp.Return_Which_exception {
		exc, _ := ret.Exception()
		reason, _ := exc.Reason()
		t.Error("Return.exception:", reason)
	} else {
		t.Errorf("Return.Which() = %v; want %v", ret.Which(), rpccapnp.Return_Which_results)
	}
}

func sendBootstrapAndFinish(t *testing.T, p rpc.Transport) (importID uint32) {
	importID, questionID := bootstrapRoundtrip(t, p)
	err := sendMessage(context.TODO(), p, func(msg rpccapnp.Message) error {
		finish, err := msg.NewFinish()
		if err != nil {
			return err
		}
		finish.SetQuestionId(questionID)
		finish.SetReleaseResultCaps(false)
		return nil
	})
	if err != nil {
		t.Fatal("Write Bootstrap Finish failed:", err)
	}
	return importID
}

func sendMessage(ctx context.Context, t rpc.Transport, f func(rpccapnp.Message) error) error {
	_, s, err := capnp.NewMessage(capnp.SingleSegment(nil))
	m, err := rpccapnp.NewRootMessage(s)
	if err != nil {
		return err
	}
	if err := f(m); err != nil {
		return err
	}
	return t.SendMessage(ctx, m)
}

func startRecvMessage(t rpc.Transport) <-chan asyncRecv {
	ch := make(chan asyncRecv, 1)
	go func() {
		msg, err := t.RecvMessage(context.TODO())
		ch <- asyncRecv{msg, err}
	}()
	return ch
}

type asyncRecv struct {
	msg rpccapnp.Message
	err error
}

func mockClient() capnp.Client {
	return capnp.ErrorClient(errMockClient)
}

type stubClient func(ctx context.Context, params capnp.Struct) (capnp.Struct, error)

func (stub stubClient) Call(call *capnp.Call) capnp.Answer {
	if call.Method.InterfaceID != interfaceID || call.Method.MethodID != methodID {
		return capnp.ErrorAnswer(errNotImplemented)
	}
	cc, err := call.PlaceParams(nil)
	if err != nil {
		return capnp.ErrorAnswer(err)
	}
	s, err := stub(call.Ctx, cc)
	if err != nil {
		return capnp.ErrorAnswer(err)
	}
	return capnp.ImmediateAnswer(s)
}

func (stub stubClient) Close() error {
	return nil
}

var (
	errMockClient     = errors.New("rpc_test: mock client")
	errNotImplemented = errors.New("rpc_test: stub client method not implemented")
)
