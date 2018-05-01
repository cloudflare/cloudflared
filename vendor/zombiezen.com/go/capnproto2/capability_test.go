package capnp

import (
	"bytes"
	"errors"
	"testing"
)

func TestToInterface(t *testing.T) {
	_, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		ptr Pointer
		in  Interface
	}{
		{nil, Interface{}},
		{Struct{}, Interface{}},
		{Struct{seg: seg, off: 0, depthLimit: maxDepth}, Interface{}},
		{Interface{}, Interface{}},
		{Interface{seg, 42}, Interface{seg, 42}},
	}
	for _, test := range tests {
		if in := ToInterface(test.ptr); in != test.in {
			t.Errorf("ToInterface(%#v) = %#v; want %#v", test.ptr, in, test.in)
		}
	}
}

func TestInterface_value(t *testing.T) {
	_, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in  Interface
		val rawPointer
	}{
		{Interface{}, 0},
		{NewInterface(seg, 0), 0x0000000000000003},
		{NewInterface(seg, 0xdeadbeef), 0xdeadbeef00000003},
	}
	for _, test := range tests {
		for paddr := Address(0); paddr < 16; paddr++ {
			if val := test.in.value(paddr); val != test.val {
				t.Errorf("Interface{seg: %p, cap: %d}.value(%v) = %v; want %v", test.in.seg, test.in.cap, paddr, val, test.val)
			}
		}
	}
}

func TestTransform(t *testing.T) {
	_, s, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewStruct(s, ObjectSize{PointerCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewStruct(s, ObjectSize{DataSize: 8, PointerCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	root.SetPointer(1, a)
	a.SetUint64(0, 1)
	b, err := NewStruct(s, ObjectSize{DataSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	b.SetUint64(0, 2)
	a.SetPointer(0, b)

	dmsg, d, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	da, err := NewStruct(d, ObjectSize{DataSize: 8, PointerCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := dmsg.SetRoot(da); err != nil {
		t.Fatal(err)
	}
	da.SetUint64(0, 56)
	db, err := NewStruct(d, ObjectSize{DataSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	db.SetUint64(0, 78)
	da.SetPointer(0, db)

	tests := []struct {
		p         Pointer
		transform []PipelineOp
		out       Pointer
	}{
		{
			root,
			nil,
			root,
		},
		{
			root,
			[]PipelineOp{},
			root,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0},
			},
			nil,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0, DefaultValue: mustMarshal(t, dmsg)},
			},
			da,
		},
		{
			root,
			[]PipelineOp{
				{Field: 1},
			},
			a,
		},
		{
			root,
			[]PipelineOp{
				{Field: 1, DefaultValue: mustMarshal(t, dmsg)},
			},
			a,
		},
		{
			root,
			[]PipelineOp{
				{Field: 1},
				{Field: 0},
			},
			b,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0},
				{Field: 0},
			},
			nil,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0, DefaultValue: mustMarshal(t, dmsg)},
				{Field: 0},
			},
			db,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0},
				{Field: 0, DefaultValue: mustMarshal(t, dmsg)},
			},
			da,
		},
		{
			root,
			[]PipelineOp{
				{Field: 0, DefaultValue: mustMarshal(t, dmsg)},
				{Field: 1, DefaultValue: mustMarshal(t, dmsg)},
			},
			da,
		},
	}

	for _, test := range tests {
		out, err := Transform(test.p, test.transform)
		if !deepPointerEqual(out, test.out) {
			t.Errorf("Transform(%+v, %v) = %+v; want %+v", test.p, test.transform, out, test.out)
		}
		if err != nil {
			t.Errorf("Transform(%+v, %v) error: %v", test.p, test.transform, err)
		}
	}
}

func TestMethodString(t *testing.T) {
	tests := []struct {
		m *Method
		s string
	}{
		{
			&Method{
				InterfaceID: 0x8e5322c1e9282534,
				MethodID:    1,
			},
			"@0x8e5322c1e9282534.@1",
		},
		{
			&Method{
				InterfaceID:   0x8e5322c1e9282534,
				MethodID:      1,
				InterfaceName: "aircraftlib:Echo",
				MethodName:    "foo",
			},
			"aircraftlib:Echo.foo",
		},
	}
	for _, test := range tests {
		if s := test.m.String(); s != test.s {
			t.Errorf("%#v.String() = %q; want %q", test.m, s, test.s)
		}
	}
}

func TestPipelineOpString(t *testing.T) {
	tests := []struct {
		op PipelineOp
		s  string
	}{
		{
			PipelineOp{Field: 4},
			"get field 4",
		},
		{
			PipelineOp{Field: 4, DefaultValue: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
			"get field 4 with default",
		},
	}
	for _, test := range tests {
		if s := test.op.String(); s != test.s {
			t.Errorf("%#v.String() = %q; want %q", test.op, s, test.s)
		}
	}
}

func TestIsUnimplemented(t *testing.T) {
	tests := []struct {
		e  error
		ok bool
	}{
		{nil, false},
		{ErrUnimplemented, true},
		{errors.New("foo"), false},
		{&MethodError{Method: new(Method), Err: ErrUnimplemented}, true},
		{&MethodError{Method: new(Method), Err: errors.New("foo")}, false},
	}
	for _, test := range tests {
		if ok := IsUnimplemented(test.e); ok != test.ok {
			t.Errorf("IsUnimplemented(%#v) = %t; want %t", test.e, ok, test.ok)
		}
	}
}

func mustMarshal(t *testing.T, msg *Message) []byte {
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal("Marshal:", err)
	}
	return data
}

func deepPointerEqual(a, b Pointer) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	msgA, _, _ := NewMessage(SingleSegment(nil))
	msgA.SetRoot(a)
	abytes, _ := msgA.Marshal()
	msgB, _, _ := NewMessage(SingleSegment(nil))
	msgB.SetRoot(b)
	bbytes, _ := msgB.Marshal()
	return bytes.Equal(abytes, bbytes)
}
