package capnp

import (
	"bytes"
	"testing"
)

func TestToListDefault(t *testing.T) {
	msg := &Message{Arena: SingleSegment([]byte{
		0, 0, 0, 0, 0, 0, 0, 0,
		42, 0, 0, 0, 0, 0, 0, 0,
	})}
	seg, err := msg.Segment(0)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		ptr  Pointer
		def  []byte
		list List
	}{
		{nil, nil, List{}},
		{Struct{}, nil, List{}},
		{Struct{seg: seg, off: 0, depthLimit: maxDepth}, nil, List{}},
		{List{}, nil, List{}},
		{
			ptr: List{
				seg:        seg,
				off:        8,
				length:     1,
				size:       ObjectSize{DataSize: 8},
				depthLimit: maxDepth,
			},
			list: List{
				seg:        seg,
				off:        8,
				length:     1,
				size:       ObjectSize{DataSize: 8},
				depthLimit: maxDepth,
			},
		},
	}

	for _, test := range tests {
		list, err := ToListDefault(test.ptr, test.def)
		if err != nil {
			t.Errorf("ToListDefault(%#v, % 02x) error: %v", test.ptr, test.def, err)
			continue
		}
		if !deepPointerEqual(list, test.list) {
			t.Errorf("ToListDefault(%#v, % 02x) = %#v; want %#v", test.ptr, test.def, list, test.list)
		}
	}
}

func TestTextListBytesAt(t *testing.T) {
	msg := &Message{Arena: SingleSegment([]byte{
		0, 0, 0, 0, 0, 0, 0, 0,
		0x01, 0, 0, 0, 0x22, 0, 0, 0,
		'f', 'o', 'o', 0, 0, 0, 0, 0,
	})}
	seg, err := msg.Segment(0)
	if err != nil {
		t.Fatal(err)
	}
	list := TextList{List{
		seg:        seg,
		off:        8,
		length:     1,
		size:       ObjectSize{PointerCount: 1},
		depthLimit: maxDepth,
	}}
	b, err := list.BytesAt(0)
	if err != nil {
		t.Errorf("list.BytesAt(0) error: %v", err)
	}
	if !bytes.Equal(b, []byte("foo")) {
		t.Errorf("list.BytesAt(0) = %q; want \"foo\"", b)
	}
}

func TestListRaw(t *testing.T) {
	_, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		list List
		raw  rawPointer
	}{
		{list: List{}, raw: 0},
		{
			list: List{seg: seg, length: 3, size: ObjectSize{}},
			raw:  0x0000001800000001,
		},
		{
			list: List{seg: seg, off: 24, length: 15, flags: isBitList},
			raw:  0x0000007900000001,
		},
		{
			list: List{seg: seg, off: 40, length: 15, size: ObjectSize{DataSize: 1}},
			raw:  0x0000007a00000001,
		},
		{
			list: List{seg: seg, off: 40, length: 15, size: ObjectSize{DataSize: 2}},
			raw:  0x0000007b00000001,
		},
		{
			list: List{seg: seg, off: 40, length: 15, size: ObjectSize{DataSize: 4}},
			raw:  0x0000007c00000001,
		},
		{
			list: List{seg: seg, off: 40, length: 15, size: ObjectSize{DataSize: 8}},
			raw:  0x0000007d00000001,
		},
		{
			list: List{seg: seg, off: 40, length: 15, size: ObjectSize{PointerCount: 1}},
			raw:  0x0000007e00000001,
		},
		{
			list: List{seg: seg, off: 40, length: 7, size: ObjectSize{DataSize: 16, PointerCount: 1}, flags: isCompositeList},
			raw:  0x000000af00000001,
		},
	}
	for _, test := range tests {
		if raw := test.list.raw(); raw != test.raw {
			t.Errorf("%+v.raw() = %#v; want %#v", test.list, raw, test.raw)
		}
	}
}
