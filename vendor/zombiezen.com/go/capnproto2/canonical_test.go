package capnp

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCanonicalize(t *testing.T) {
	{
		// null
		b, err := Canonicalize(Struct{})
		if err != nil {
			t.Fatal("Canonicalize(Struct{}):", err)
		}
		if want := ([]byte{0, 0, 0, 0, 0, 0, 0, 0}); !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(Struct{}) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// empty struct
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{})
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(empty struct):", err)
		}
		want := ([]byte{0xfc, 0xff, 0xff, 0xff, 0, 0, 0, 0})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(empty struct) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// zero data, zero pointer struct
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{DataSize: 8, PointerCount: 1})
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(zero data, zero pointer struct):", err)
		}
		want := ([]byte{0xfc, 0xff, 0xff, 0xff, 0, 0, 0, 0})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(zero data, zero pointer struct) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// one word data struct
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{DataSize: 8, PointerCount: 1})
		s.SetUint16(0, 0xbeef)
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(one word data struct):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 1, 0, 0, 0,
			0xef, 0xbe, 0, 0, 0, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(one word data struct) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// two pointers to zero structs
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 2})
		e1, _ := NewStruct(seg, ObjectSize{DataSize: 8})
		e2, _ := NewStruct(seg, ObjectSize{DataSize: 8})
		s.SetPtr(0, e1.ToPtr())
		s.SetPtr(1, e2.ToPtr())
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(two pointers to zero structs):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 2, 0,
			0xfc, 0xff, 0xff, 0xff, 0, 0, 0, 0,
			0xfc, 0xff, 0xff, 0xff, 0, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(two pointers to zero structs) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// int list
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 1})
		l, _ := NewInt8List(seg, 5)
		s.SetPtr(0, l.ToPtr())
		l.Set(0, 1)
		l.Set(1, 2)
		l.Set(2, 3)
		l.Set(3, 4)
		l.Set(4, 5)
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(int list):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 1, 0,
			0x01, 0, 0, 0, 0x2a, 0, 0, 0,
			1, 2, 3, 4, 5, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(int list) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// zero int list
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 1})
		l, _ := NewInt8List(seg, 5)
		s.SetPtr(0, l.ToPtr())
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(zero int list):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 1, 0,
			0x01, 0, 0, 0, 0x2a, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(zero int list) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// struct list
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 1})
		l, _ := NewCompositeList(seg, ObjectSize{DataSize: 8, PointerCount: 1}, 2)
		s.SetPtr(0, l.ToPtr())
		l.Struct(0).SetUint64(0, 0xdeadbeef)
		txt, _ := NewText(seg, "xyzzy")
		l.Struct(1).SetPtr(0, txt.ToPtr())
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(struct list):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 1, 0,
			0x01, 0, 0, 0, 0x27, 0, 0, 0,
			0x08, 0, 0, 0, 1, 0, 1, 0,
			0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0x01, 0, 0, 0, 0x32, 0, 0, 0,
			'x', 'y', 'z', 'z', 'y', 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(struct list) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// zero struct list
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 1})
		l, _ := NewCompositeList(seg, ObjectSize{DataSize: 16, PointerCount: 2}, 3)
		s.SetPtr(0, l.ToPtr())
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(zero struct list):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 1, 0,
			0x01, 0, 0, 0, 0x07, 0, 0, 0,
			0x0c, 0, 0, 0, 0, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(zero struct list) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
	{
		// zero-length struct list
		_, seg, _ := NewMessage(SingleSegment(nil))
		s, _ := NewStruct(seg, ObjectSize{PointerCount: 1})
		l, _ := NewCompositeList(seg, ObjectSize{DataSize: 16, PointerCount: 2}, 0)
		s.SetPtr(0, l.ToPtr())
		b, err := Canonicalize(s)
		if err != nil {
			t.Fatal("Canonicalize(zero-length struct list):", err)
		}
		want := ([]byte{
			0, 0, 0, 0, 0, 0, 1, 0,
			0x01, 0, 0, 0, 0x07, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
		})
		if !bytes.Equal(b, want) {
			t.Errorf("Canonicalize(zero-length struct list) =\n%s\n; want\n%s", hex.Dump(b), hex.Dump(want))
		}
	}
}
