package capnp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestSegmentInBounds(t *testing.T) {
	tests := []struct {
		n    int
		addr Address
		ok   bool
	}{
		{0, 0, false},
		{0, 1, false},
		{0, 2, false},
		{1, 0, true},
		{1, 1, false},
		{1, 2, false},
		{2, 0, true},
		{2, 1, true},
		{2, 2, false},
	}
	for _, test := range tests {
		seg := &Segment{data: make([]byte, test.n)}
		if ok := seg.inBounds(test.addr); ok != test.ok {
			t.Errorf("&Segment{data: make([]byte, %d)}.inBounds(%#v) = %t; want %t", test.n, test.addr, ok, test.ok)
		}
	}
}

func TestSegmentRegionInBounds(t *testing.T) {
	tests := []struct {
		n    int
		addr Address
		sz   Size
		ok   bool
	}{
		{0, 0, 0, true}, // zero-sized region <= len is okay
		{0, 0, 1, false},
		{0, 1, 0, false},
		{0, 1, 1, false},
		{1, 0, 0, true},
		{1, 0, 1, true},
		{1, 1, 0, true},
		{1, 1, 1, false},
		{2, 0, 0, true},
		{2, 0, 1, true},
		{2, 0, 2, true},
		{2, 0, 3, false},
		{2, 1, 0, true},
		{2, 1, 1, true},
		{2, 1, 2, false},
		{2, 1, 3, false},
		{2, 2, 0, true},
		{2, 2, 1, false},
	}
	for _, test := range tests {
		seg := &Segment{data: make([]byte, test.n)}
		if ok := seg.regionInBounds(test.addr, test.sz); ok != test.ok {
			t.Errorf("&Segment{data: make([]byte, %d)}.regionInBounds(%#v, %#v) = %t; want %t", test.n, test.addr, test.sz, ok, test.ok)
		}
	}
}

func TestSegmentReadUint8(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint8
		panics bool
	}{
		{data: []byte{}, addr: 0, panics: true},
		{data: []byte{42}, addr: 0, val: 42},
		{data: []byte{42}, addr: 1, panics: true},
		{data: []byte{1, 42, 2}, addr: 0, val: 1},
		{data: []byte{1, 42, 2}, addr: 1, val: 42},
		{data: []byte{1, 42, 2}, addr: 2, val: 2},
		{data: []byte{1, 42, 2}, addr: 3, panics: true},
	}
	for _, test := range tests {
		seg := &Segment{data: test.data}
		var val uint8
		err := catchPanic(func() {
			val = seg.readUint8(test.addr)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.readUint8(%v) unexpected panic: %v", test.data, test.addr, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.readUint8(%v) did not panic as expected", test.data, test.addr)
			continue
		}
		if val != test.val {
			t.Errorf("&Segment{data: % x}.readUint8(%v) = %#x; want %#x", test.data, test.addr, val, test.val)
		}
	}
}

func TestSegmentReadUint16(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint16
		panics bool
	}{
		{data: []byte{}, addr: 0, panics: true},
		{data: []byte{0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00}, addr: 0, val: 0},
		{data: []byte{0x01, 0x00}, addr: 0, val: 1},
		{data: []byte{0x34, 0x12}, addr: 0, val: 0x1234},
		{data: []byte{0x34, 0x12, 0x56}, addr: 0, val: 0x1234},
		{data: []byte{0x34, 0x12, 0x56}, addr: 1, val: 0x5612},
		{data: []byte{0x34, 0x12, 0x56}, addr: 2, panics: true},
	}
	for _, test := range tests {
		seg := &Segment{data: test.data}
		var val uint16
		err := catchPanic(func() {
			val = seg.readUint16(test.addr)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.readUint16(%v) unexpected panic: %v", test.data, test.addr, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.readUint16(%v) did not panic as expected", test.data, test.addr)
			continue
		}
		if val != test.val {
			t.Errorf("&Segment{data: % x}.readUint16(%v) = %#x; want %#x", test.data, test.addr, val, test.val)
		}
	}
}

func TestSegmentReadUint32(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint32
		panics bool
	}{
		{data: []byte{}, addr: 0, panics: true},
		{data: []byte{0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00}, addr: 0, val: 0},
		{data: []byte{0x78, 0x56, 0x34, 0x12}, addr: 0, val: 0x12345678},
		{data: []byte{0xff, 0x78, 0x56, 0x34, 0x12, 0xff}, addr: 1, val: 0x12345678},
		{data: []byte{0xff, 0x78, 0x56, 0x34, 0x12, 0xff}, addr: 2, val: 0xff123456},
		{data: []byte{0xff, 0x78, 0x56, 0x34, 0x12, 0xff}, addr: 3, panics: true},
	}
	for _, test := range tests {
		seg := &Segment{data: test.data}
		var val uint32
		err := catchPanic(func() {
			val = seg.readUint32(test.addr)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.readUint32(%v) unexpected panic: %v", test.data, test.addr, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.readUint32(%v) did not panic as expected", test.data, test.addr)
			continue
		}
		if val != test.val {
			t.Errorf("&Segment{data: % x}.readUint32(%v) = %#x; want %#x", test.data, test.addr, val, test.val)
		}
	}
}

func TestSegmentReadUint64(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint64
		panics bool
	}{
		{data: []byte{}, addr: 0, panics: true},
		{data: []byte{0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, addr: 0, panics: true},
		{data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, addr: 0, val: 0},
		{data: []byte{0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01}, addr: 0, val: 0x0123456789abcdef},
		{data: []byte{0xff, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01, 0xff}, addr: 0, val: 0x23456789abcdefff},
		{data: []byte{0xff, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01, 0xff}, addr: 1, val: 0x0123456789abcdef},
		{data: []byte{0xff, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01, 0xff}, addr: 2, val: 0xff0123456789abcd},
		{data: []byte{0xff, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01, 0xff}, addr: 3, panics: true},
	}
	for _, test := range tests {
		seg := &Segment{data: test.data}
		var val uint64
		err := catchPanic(func() {
			val = seg.readUint64(test.addr)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.readUint64(%v) unexpected panic: %v", test.data, test.addr, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.readUint64(%v) did not panic as expected", test.data, test.addr)
			continue
		}
		if val != test.val {
			t.Errorf("&Segment{data: % x}.readUint64(%v) = %#x; want %#x", test.data, test.addr, val, test.val)
		}
	}
}

func TestSegmentWriteUint8(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint8
		out    []byte
		panics bool
	}{
		{
			data:   []byte{},
			addr:   0,
			val:    0,
			panics: true,
		},
		{
			data: []byte{1},
			addr: 0,
			val:  42,
			out:  []byte{42},
		},
		{
			data:   []byte{42},
			addr:   1,
			val:    1,
			panics: true,
		},
		{
			data: []byte{1, 2, 3},
			addr: 0,
			val:  0xff,
			out:  []byte{0xff, 2, 3},
		},
		{
			data: []byte{1, 2, 3},
			addr: 1,
			val:  0xff,
			out:  []byte{1, 0xff, 3},
		},
		{
			data: []byte{1, 2, 3},
			addr: 2,
			val:  0xff,
			out:  []byte{1, 2, 0xff},
		},
		{
			data:   []byte{1, 2, 3},
			addr:   3,
			val:    0xff,
			panics: true,
		},
	}
	for _, test := range tests {
		out := make([]byte, len(test.data))
		copy(out, test.data)
		seg := &Segment{data: out}
		err := catchPanic(func() {
			seg.writeUint8(test.addr, test.val)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.writeUint8(%v, %#x) unexpected panic: %v", test.data, test.addr, test.val, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.writeUint8(%v, %#x) did not panic as expected", test.data, test.addr, test.val)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("data after &Segment{data: % x}.writeUint8(%v, %#x) = % x; want % x", test.data, test.addr, test.val, out, test.out)
		}
	}
}

func TestSegmentWriteUint16(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint16
		out    []byte
		panics bool
	}{
		{
			data:   []byte{},
			addr:   0,
			val:    0,
			panics: true,
		},
		{
			data: []byte{1, 2, 3, 4},
			addr: 1,
			val:  0x1234,
			out:  []byte{1, 0x34, 0x12, 4},
		},
	}
	for _, test := range tests {
		out := make([]byte, len(test.data))
		copy(out, test.data)
		seg := &Segment{data: out}
		err := catchPanic(func() {
			seg.writeUint16(test.addr, test.val)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.writeUint16(%v, %#x) unexpected panic: %v", test.data, test.addr, test.val, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.writeUint16(%v, %#x) did not panic as expected", test.data, test.addr, test.val)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("data after &Segment{data: % x}.writeUint16(%v, %#x) = % x; want % x", test.data, test.addr, test.val, out, test.out)
		}
	}
}

func TestSegmentWriteUint32(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint32
		out    []byte
		panics bool
	}{
		{
			data:   []byte{},
			addr:   0,
			val:    0,
			panics: true,
		},
		{
			data: []byte{1, 2, 3, 4, 5, 6},
			addr: 1,
			val:  0x01234567,
			out:  []byte{1, 0x67, 0x45, 0x23, 0x01, 6},
		},
	}
	for _, test := range tests {
		out := make([]byte, len(test.data))
		copy(out, test.data)
		seg := &Segment{data: out}
		err := catchPanic(func() {
			seg.writeUint32(test.addr, test.val)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.writeUint32(%v, %#x) unexpected panic: %v", test.data, test.addr, test.val, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.writeUint32(%v, %#x) did not panic as expected", test.data, test.addr, test.val)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("data after &Segment{data: % x}.writeUint32(%v, %#x) = % x; want % x", test.data, test.addr, test.val, out, test.out)
		}
	}
}

func TestSegmentWriteUint64(t *testing.T) {
	tests := []struct {
		data   []byte
		addr   Address
		val    uint64
		out    []byte
		panics bool
	}{
		{
			data:   []byte{},
			addr:   0,
			val:    0,
			panics: true,
		},
		{
			data: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			addr: 1,
			val:  0x0123456789abcdef,
			out:  []byte{1, 0xef, 0xcd, 0xab, 0x89, 0x67, 0x45, 0x23, 0x01, 10},
		},
	}
	for _, test := range tests {
		out := make([]byte, len(test.data))
		copy(out, test.data)
		seg := &Segment{data: out}
		err := catchPanic(func() {
			seg.writeUint64(test.addr, test.val)
		})
		if err != nil {
			if !test.panics {
				t.Errorf("&Segment{data: % x}.writeUint64(%v, %#x) unexpected panic: %v", test.data, test.addr, test.val, err)
			}
			continue
		}
		if test.panics {
			t.Errorf("&Segment{data: % x}.writeUint64(%v, %#x) did not panic as expected", test.data, test.addr, test.val)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("data after &Segment{data: % x}.writeUint64(%v, %#x) = % x; want % x", test.data, test.addr, test.val, out, test.out)
		}
	}
}

func TestSetPtrCopyListMember(t *testing.T) {
	_, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	root, err := NewRootStruct(seg, ObjectSize{PointerCount: 2})
	if err != nil {
		t.Fatal("NewRootStruct:", err)
	}
	plist, err := NewCompositeList(seg, ObjectSize{PointerCount: 1}, 1)
	if err != nil {
		t.Fatal("NewCompositeList:", err)
	}
	if err := root.SetPtr(0, plist.ToPtr()); err != nil {
		t.Fatal("root.SetPtr(0, plist):", err)
	}
	sub, err := NewStruct(seg, ObjectSize{DataSize: 8})
	if err != nil {
		t.Fatal("NewStruct:", err)
	}
	sub.SetUint64(0, 42)
	pl0 := plist.Struct(0)
	if err := pl0.SetPtr(0, sub.ToPtr()); err != nil {
		t.Fatal("pl0.SetPtr(0, sub.ToPtr()):", err)
	}

	if err := root.SetPtr(1, pl0.ToPtr()); err != nil {
		t.Error("root.SetPtr(1, pl0):", err)
	}

	p1, err := root.Ptr(1)
	if err != nil {
		t.Error("root.Ptr(1):", err)
	}
	s1 := p1.Struct()
	if !s1.IsValid() {
		t.Error("root.Ptr(1) is not a valid struct")
	}
	if SamePtr(s1.ToPtr(), pl0.ToPtr()) {
		t.Error("list member not copied; points to same object")
	}
	s1p0, err := s1.Ptr(0)
	if err != nil {
		t.Error("root.Ptr(1).Struct().Ptr(0):", err)
	}
	s1s0 := s1p0.Struct()
	if !s1s0.IsValid() {
		t.Error("root.Ptr(1).Struct().Ptr(0) is not a valid struct")
	}
	if SamePtr(s1s0.ToPtr(), sub.ToPtr()) {
		t.Error("sub-object not copied; points to same object")
	}
	if got := s1s0.Uint64(0); got != 42 {
		t.Errorf("sub-object data = %d; want 42", got)
	}
}

func TestSetPtrToZeroSizeStruct(t *testing.T) {
	_, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal("NewMessage:", err)
	}
	root, err := NewRootStruct(seg, ObjectSize{PointerCount: 1})
	if err != nil {
		t.Fatal("NewRootStruct:", err)
	}
	sub, err := NewStruct(seg, ObjectSize{})
	if err != nil {
		t.Fatal("NewStruct:", err)
	}
	if err := root.SetPtr(0, sub.ToPtr()); err != nil {
		t.Fatal("root.SetPtr(0, sub.ToPtr()):", err)
	}
	ptrSlice := seg.Data()[root.off : root.off+8]
	want := []byte{0xfc, 0xff, 0xff, 0xff, 0, 0, 0, 0}
	if !bytes.Equal(ptrSlice, want) {
		t.Errorf("SetPtr wrote % 02x; want % 02x", ptrSlice, want)
	}
}

func TestReadFarPointers(t *testing.T) {
	msg := &Message{
		// an rpc.capnp Message
		Arena: MultiSegment([][]byte{
			// Segment 0
			{
				// Double-far pointer: segment 2, offset 0
				0x06, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00,
			},
			// Segment 1
			{
				// (Root) Struct data section
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Struct pointer section
				// Double-far pointer: segment 4, offset 0
				0x06, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
			},
			// Segment 2
			{
				// Far pointer landing pad: segment 1, offset 0
				0x02, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
				// Far pointer landing pad tag word: struct with 1 word data and 1 pointer
				0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
			},
			// Segment 3
			{
				// (Root>0) Struct data section
				0x00, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00,
				0xaa, 0x70, 0x65, 0x21, 0xd7, 0x7b, 0x31, 0xa7,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Struct pointer section
				// Far pointer: segment 4, offset 4
				0x22, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
				// Far pointer: segment 4, offset 7
				0x3a, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
				// Null
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			// Segment 4
			{
				// Far pointer landing pad: segment 3, offset 0
				0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
				// Far pointer landing pad tag word: struct with 3 word data and 3 pointer
				0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x03, 0x00,
				// (Root>0>0) Struct data section
				0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Struct pointer section
				// Null
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Far pointer landing pad: struct pointer: offset -3, 1 word data, 1 pointer
				0xf4, 0xff, 0xff, 0xff, 0x01, 0x00, 0x01, 0x00,
				// (Root>0>1) Struct pointer section
				// Struct pointer: offset 2, 1 word data
				0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
				// Null
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Far pointer landing pad: struct pointer: offset -3, 2 pointers
				0xf4, 0xff, 0xff, 0xff, 0x00, 0x00, 0x02, 0x00,
				// (Root>0>1>0) Struct data section
				0x2a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		}),
	}
	rootp, err := msg.RootPtr()
	if err != nil {
		t.Error("RootPtr:", err)
	}
	root := rootp.Struct()
	if root.Uint16(0) != 2 {
		t.Errorf("root.Uint16(0) = %d; want 2", root.Uint16(0))
	}
	callp, err := root.Ptr(0)
	if err != nil {
		t.Error("root.Ptr(0):", err)
	}
	call := callp.Struct()
	targetp, err := call.Ptr(0)
	if err != nil {
		t.Error("root.Ptr(0).Ptr(0):", err)
	}
	if got := targetp.Struct().Uint32(0); got != 84 {
		t.Errorf("root.Ptr(0).Ptr(0).Uint32(0) = %d; want 84", got)
	}
	paramsp, err := call.Ptr(1)
	if err != nil {
		t.Error("root.Ptr(0).Ptr(1):", err)
	}
	contentp, err := paramsp.Struct().Ptr(0)
	if err != nil {
		t.Error("root.Ptr(0).Ptr(1).Ptr(0):", err)
	}
	if got := contentp.Struct().Uint64(0); got != 42 {
		t.Errorf("root.Ptr(0).Ptr(1).Ptr(0).Uint64(0) = %d; want 42", got)
	}
}

func TestWriteFarPointer(t *testing.T) {
	// TODO(someday): run same test with a two-word list

	msg := &Message{
		Arena: MultiSegment([][]byte{
			make([]byte, 8),
			make([]byte, 0, 24),
		}),
	}
	seg1, err := msg.Segment(1)
	if err != nil {
		t.Fatal("msg.Segment(1):", err)
	}
	s, err := NewStruct(seg1, ObjectSize{DataSize: 8, PointerCount: 1})
	if err != nil {
		t.Fatal("NewStruct(msg.Segment(1), ObjectSize{8, 1}):", err)
	}
	if s.Segment() != seg1 {
		t.Fatalf("struct allocated in segment %d", s.Segment().ID())
	}
	if err := msg.SetRootPtr(s.ToPtr()); err != nil {
		t.Error("msg.SetRootPtr(...):", err)
	}
	seg0, err := msg.Segment(0)
	if err != nil {
		t.Fatal("msg.Segment(0):", err)
	}

	root := rawPointer(binary.LittleEndian.Uint64(seg0.Data()))
	if root.pointerType() != farPointer {
		t.Fatalf("root (%#016x) type = %v; want %v (farPointer)", root, root.pointerType(), farPointer)
	}
	if root.farSegment() != 1 {
		t.Fatalf("root points to segment %d; want 1", root.farSegment())
	}
	padAddr := root.farAddress()
	if padAddr > Address(len(seg1.Data())-8) {
		t.Fatalf("root points to out of bounds address %v; size of segment is %d", padAddr, len(seg1.Data()))
	}

	pad := rawPointer(binary.LittleEndian.Uint64(seg1.Data()[padAddr:]))
	if pad.pointerType() != structPointer {
		t.Errorf("landing pad (%#016x) type = %v; want %v (structPointer)", pad, pad.pointerType(), structPointer)
	}
	if got, ok := pad.offset().resolve(padAddr + 8); !ok || got != s.off {
		t.Errorf("landing pad (%#016x @ %v) resolved address = %v, %t; want %v, true", pad, padAddr, got, ok, s.off)
	}
	if got, want := pad.structSize(), (ObjectSize{DataSize: 8, PointerCount: 1}); got != want {
		t.Errorf("landing pad (%#016x) struct size = %v; want %v", pad, got, want)
	}
}

func TestWriteDoubleFarPointer(t *testing.T) {
	// TODO(someday): run same test with a two-word list

	msg := &Message{
		Arena: MultiSegment([][]byte{
			make([]byte, 8),
			make([]byte, 0, 16),
		}),
	}
	seg1, err := msg.Segment(1)
	if err != nil {
		t.Fatal("msg.Segment(1):", err)
	}
	s, err := NewStruct(seg1, ObjectSize{DataSize: 8, PointerCount: 1})
	if err != nil {
		t.Fatal("NewStruct(msg.Segment(1), ObjectSize{8, 1}):", err)
	}
	if s.Segment() != seg1 {
		t.Fatalf("struct allocated in segment %d", s.Segment().ID())
	}
	if err := msg.SetRootPtr(s.ToPtr()); err != nil {
		t.Error("msg.SetRootPtr(...):", err)
	}
	seg0, err := msg.Segment(0)
	if err != nil {
		t.Fatal("msg.Segment(0):", err)
	}

	root := rawPointer(binary.LittleEndian.Uint64(seg0.Data()))
	if root.pointerType() != doubleFarPointer {
		t.Fatalf("root (%#016x) type = %v; want %v (doubleFarPointer)", root, root.pointerType(), doubleFarPointer)
	}
	if root.farSegment() == 0 || root.farSegment() == 1 {
		t.Fatalf("root points to segment %d; want !=0,1", root.farSegment())
	}
	padSeg, err := msg.Segment(root.farSegment())
	if err != nil {
		t.Fatalf("msg.Segment(%d): %v", root.farSegment(), err)
	}
	padAddr := root.farAddress()
	if padAddr > Address(len(padSeg.Data())-16) {
		t.Fatalf("root points to out of bounds address %v; size of segment is %d", padAddr, len(padSeg.Data()))
	}

	pad1 := rawPointer(binary.LittleEndian.Uint64(padSeg.Data()[padAddr:]))
	if pad1.pointerType() != farPointer {
		t.Errorf("landing pad pointer 1 (%#016x) type = %v; want %v (farPointer)", pad1, pad1.pointerType(), farPointer)
	}
	if pad1.farSegment() != 1 {
		t.Fatalf("landing pad pointer 1 (%#016x) points to segment %d; want 1", pad1, pad1.farSegment())
	}
	if pad1.farAddress() != s.off {
		t.Fatalf("landing pad pointer 1 (%#016x) points to address %v; want %v", pad1, pad1.farAddress(), s.off)
	}

	pad2 := rawPointer(binary.LittleEndian.Uint64(padSeg.Data()[padAddr+8:]))
	if pad2.pointerType() != structPointer {
		t.Errorf("landing pad pointer 2 (%#016x) type = %v; want %v (structPointer)", pad2, pad2.pointerType(), structPointer)
	}
	if pad2.offset() != 0 {
		t.Errorf("landing pad pointer 2 (%#016x) offset = %d; want 0", pad2, pad2.offset())
	}
	if got, want := pad2.structSize(), (ObjectSize{DataSize: 8, PointerCount: 1}); got != want {
		t.Errorf("landing pad pointer 2 (%#016x) struct size = %v; want %v", pad2, got, want)
	}
}

func catchPanic(f func()) (err error) {
	defer func() {
		pval := recover()
		if pval == nil {
			return
		}
		e, ok := pval.(error)
		if !ok {
			err = fmt.Errorf("non-error panic: %#v", pval)
			return
		}
		err = e
	}()
	f()
	return nil
}
