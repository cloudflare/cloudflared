package capnp_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
	"zombiezen.com/go/capnproto2/internal/capnptool"
)

const schemaPath = "internal/aircraftlib/aircraft.capnp"

func initNester(t *testing.T, n air.Nester1Capn, strs ...string) {
	tl, err := n.NewStrs(int32(len(strs)))
	if err != nil {
		t.Fatalf("initNester(..., %q): NewStrs: %v", strs, err)
	}
	for i, s := range strs {
		if err := tl.Set(i, s); err != nil {
			t.Fatalf("initNester(..., %q): set strs[%d]: %v", strs, i, err)
		}
	}
}

func zdateFilledMessage(t testing.TB, n int32) *capnp.Message {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatal(err)
	}
	list, err := z.NewZdatevec(n)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < int(n); i++ {
		d, err := air.NewZdate(seg)
		if err != nil {
			t.Fatal(err)
		}
		d.SetMonth(12)
		d.SetDay(7)
		d.SetYear(int16(2004 + i))
		list.Set(i, d)
	}

	return msg
}

func zdataFilledMessage(t testing.TB, n int) *capnp.Message {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatal(err)
	}
	d, err := air.NewZdata(seg)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, n)
	for i := 0; i < len(b); i++ {
		b[i] = byte(i)
	}
	d.SetData(b)
	z.SetZdata(d)
	return msg
}

// encodeTestMessage encodes the textual Cap'n Proto message to unpacked
// binary using the capnp tool, or returns the fallback if the tool fails.
func encodeTestMessage(typ string, text string, fallback []byte) ([]byte, error) {
	tool, err := capnptool.Find()
	if err != nil {
		// TODO(light): log tool missing
		return fallback, nil
	}
	b, err := tool.Encode(capnptool.Type{SchemaPath: schemaPath, Name: typ}, text)
	if err != nil {
		return nil, fmt.Errorf("%s value %q encode failed: %v", typ, text, err)
	}
	if !bytes.Equal(b, fallback) {
		return nil, fmt.Errorf("%s value %q =\n%s; fallback is\n%s\nFallback out of date?", typ, text, hex.Dump(b), hex.Dump(fallback))
	}
	return b, nil
}

// mustEncodeTestMessage encodes the textual Cap'n Proto message to unpacked
// binary using the capnp tool, or returns the fallback if the tool fails.
func mustEncodeTestMessage(t testing.TB, typ string, text string, fallback []byte) []byte {
	b, err := encodeTestMessage(typ, text, fallback)
	if err != nil {
		if _, fname, line, ok := runtime.Caller(1); ok {
			t.Fatalf("%s:%d: %v", filepath.Base(fname), line, err)
		} else {
			t.Fatal(err)
		}
	}
	return b
}
