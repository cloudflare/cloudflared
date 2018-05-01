package pogs

import (
	"testing"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
)

type simpleEcho struct{}

func checkFatal(t *testing.T, name string, err error) {
	if err != nil {
		t.Fatalf("%s for TestInsertIFace: %v", name, err)
	}
}

func (s simpleEcho) Echo(p air.Echo_echo) error {
	text, err := p.Params.In()
	if err != nil {
		return err
	}
	p.Results.SetOut(text)
	return nil
}

type EchoBase struct {
	Echo air.Echo
}

type EchoBases struct {
	Bases []EchoBase
}

type Hoth struct {
	Base EchoBase
}

func TestInsertIFace(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	checkFatal(t, "NewMessage", err)
	h, err := air.NewRootHoth(seg)
	checkFatal(t, "NewRootHoth", err)
	err = Insert(air.Hoth_TypeID, h.Struct, Hoth{
		Base: EchoBase{Echo: air.Echo_ServerToClient(simpleEcho{})},
	})
	checkFatal(t, "Insert", err)
	base, err := h.Base()
	checkFatal(t, "h.Base", err)
	echo := base.Echo()

	testEcho(t, echo)
}

func TestInsertListIFace(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	checkFatal(t, "NewMessage", err)
	wrapper, err := air.NewEchoBases(seg)
	checkFatal(t, "NewEchoBases", err)
	err = Insert(air.EchoBases_TypeID, wrapper.Struct, EchoBases{
		Bases: []EchoBase{
			{Echo: air.Echo_ServerToClient(simpleEcho{})},
			{Echo: air.Echo_ServerToClient(simpleEcho{})},
		},
	})
	checkFatal(t, "Insert", err)
	bases, err := wrapper.Bases()
	checkFatal(t, "Bases", err)
	for i := 0; i < bases.Len(); i++ {
		testEcho(t, bases.At(i).Echo())
	}

}

func TestInsertNilInterface(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	checkFatal(t, "NewMessage", err)
	h, err := air.NewRootHoth(seg)
	checkFatal(t, "NewRootHoth", err)
	err = Insert(air.Hoth_TypeID, h.Struct, Hoth{
		Base: EchoBase{Echo: air.Echo{Client: nil}},
	})
	checkFatal(t, "Insert", err)
	base, err := h.Base()
	checkFatal(t, "h.Base", err)
	echo := base.Echo()
	if echo.Client != nil {
		t.Fatalf("Expected nil client, but got %v", echo.Client)
	}
}

func TestExtractIFace(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	checkFatal(t, "NewMessage", err)
	h, err := air.NewRootHoth(seg)
	checkFatal(t, "NewRootHoth", err)
	base, err := air.NewEchoBase(seg)
	checkFatal(t, "NewEchoBase", err)
	h.SetBase(base)
	base.SetEcho(air.Echo_ServerToClient(simpleEcho{}))

	extractedHoth := Hoth{}
	err = Extract(&extractedHoth, air.Hoth_TypeID, h.Struct)
	checkFatal(t, "Extract", err)

	testEcho(t, extractedHoth.Base.Echo)
}

func TestExtractListIFace(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	checkFatal(t, "NewMessage", err)
	wrapper, err := air.NewEchoBases(seg)
	checkFatal(t, "NewEchoBases", err)
	length := 2
	list, err := air.NewEchoBase_List(seg, int32(length))
	checkFatal(t, "NewEchoBase_List", err)
	for i := 0; i < length; i++ {
		base, err := air.NewEchoBase(seg)
		base.SetEcho(air.Echo_ServerToClient(simpleEcho{}))
		checkFatal(t, "NewEchoBase", err)
		list.Set(i, base)
	}
	wrapper.SetBases(list)

	extractedBases := EchoBases{}
	err = Extract(&extractedBases, air.EchoBases_TypeID, wrapper.Struct)
	checkFatal(t, "Extract", err)
	if extractedBases.Bases == nil {
		t.Fatal("Bases is nil")
	}
	if len(extractedBases.Bases) != length {
		t.Fatalf("Bases has Wrong length: got %d but wanted %d.",
			len(extractedBases.Bases), length)
	}
	for _, v := range extractedBases.Bases {
		testEcho(t, v.Echo)
	}
}

// Make sure extract correctly handles missing interfaces.
func TestExtractMissingIFace(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))

	base, err := air.NewRootEchoBase(seg)
	checkFatal(t, "NewRootEchoBase", err)

	// Fill the client in, so we know that after extracting, if
	// it's nil it's because it was *set*, not just left over:
	extractedBase := EchoBase{Echo: air.Echo_ServerToClient(simpleEcho{})}

	err = Extract(&extractedBase, air.EchoBase_TypeID, base.Struct)
	checkFatal(t, "Extract", err)
	if extractedBase.Echo.Client != nil {
		t.Fatalf("Expected nil client but got %v", extractedBase.Echo.Client)
	}
}

func testEcho(t *testing.T, echo air.Echo) {
	expected := "Hello!"
	result, err := echo.Echo(context.TODO(), func(p air.Echo_echo_Params) error {
		p.SetIn(expected)
		return nil
	}).Struct()
	checkFatal(t, "Echo", err)
	actual, err := result.Out()
	checkFatal(t, "result.Out", err)
	if actual != expected {
		t.Fatal("Echo result did not match input; "+
			"wanted %q but got %q.", expected, actual)
	}
}
