package pogs

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/kylelemons/godebug/pretty"
	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
)

type Z struct {
	Which air.Z_Which

	F64 float64
	F32 float32

	I64 int64
	I32 int32
	I16 int16
	I8  int8

	U64 uint64
	U32 uint32
	U16 uint16
	U8  uint8

	Bool bool
	Text string
	Blob []byte

	F64vec []float64
	F32vec []float32

	I64vec []int64
	I8vec  []int8

	U64vec []uint64
	U8vec  []uint8

	Boolvec []bool
	Datavec [][]byte
	Textvec []string

	Zvec    []*Z
	Zvecvec [][]*Z

	Planebase *PlaneBase
	Airport   air.Airport

	Grp *ZGroup

	Echo air.Echo

	EchoBases EchoBases
}

type PlaneBase struct {
	Name     string
	Homes    []air.Airport
	Rating   int64
	CanFly   bool
	Capacity int64
	MaxSpeed float64
}

func (p *PlaneBase) equal(q *PlaneBase) bool {
	if p == nil && q == nil {
		return true
	}
	if (p == nil) != (q == nil) {
		return false
	}
	if len(p.Homes) != len(q.Homes) {
		return false
	}
	for i := range p.Homes {
		if p.Homes[i] != q.Homes[i] {
			return false
		}
	}
	return p.Name == q.Name &&
		p.Rating == q.Rating &&
		p.CanFly == q.CanFly &&
		p.Capacity == q.Capacity &&
		p.MaxSpeed == q.MaxSpeed
}

type ZGroup struct {
	First  uint64
	Second uint64
}

var goodTests = []Z{
	{Which: air.Z_Which_f64, F64: 3.5},
	{Which: air.Z_Which_f32, F32: 3.5},
	{Which: air.Z_Which_i64, I64: -123},
	{Which: air.Z_Which_i32, I32: -123},
	{Which: air.Z_Which_i16, I16: -123},
	{Which: air.Z_Which_i8, I8: -123},
	{Which: air.Z_Which_u64, U64: 123},
	{Which: air.Z_Which_u32, U32: 123},
	{Which: air.Z_Which_u16, U16: 123},
	{Which: air.Z_Which_u8, U8: 123},
	{Which: air.Z_Which_bool, Bool: true},
	{Which: air.Z_Which_bool, Bool: false},
	{Which: air.Z_Which_text, Text: "Hello, World!"},
	{Which: air.Z_Which_blob, Blob: nil},
	{Which: air.Z_Which_blob, Blob: []byte{}},
	{Which: air.Z_Which_blob, Blob: []byte("Hello, World!")},
	{Which: air.Z_Which_f64vec, F64vec: nil},
	{Which: air.Z_Which_f64vec, F64vec: []float64{-2.0, 4.5}},
	{Which: air.Z_Which_f32vec, F32vec: nil},
	{Which: air.Z_Which_f32vec, F32vec: []float32{-2.0, 4.5}},
	{Which: air.Z_Which_i64vec, I64vec: nil},
	{Which: air.Z_Which_i64vec, I64vec: []int64{-123, 0, 123}},
	{Which: air.Z_Which_i8vec, I8vec: nil},
	{Which: air.Z_Which_i8vec, I8vec: []int8{-123, 0, 123}},
	{Which: air.Z_Which_u64vec, U64vec: nil},
	{Which: air.Z_Which_u64vec, U64vec: []uint64{0, 123}},
	{Which: air.Z_Which_u8vec, U8vec: nil},
	{Which: air.Z_Which_u8vec, U8vec: []uint8{0, 123}},
	{Which: air.Z_Which_boolvec, Boolvec: nil},
	{Which: air.Z_Which_boolvec, Boolvec: []bool{false, true, false}},
	{Which: air.Z_Which_datavec, Datavec: nil},
	{Which: air.Z_Which_datavec, Datavec: [][]byte{[]byte("hi"), []byte("bye")}},
	{Which: air.Z_Which_datavec, Datavec: [][]byte{nil, nil, nil}},
	{Which: air.Z_Which_textvec, Textvec: nil},
	{Which: air.Z_Which_textvec, Textvec: []string{"John", "Paul", "George", "Ringo"}},
	{Which: air.Z_Which_textvec, Textvec: []string{"", "", ""}},
	{Which: air.Z_Which_zvec, Zvec: []*Z{
		{Which: air.Z_Which_i64, I64: -123},
		{Which: air.Z_Which_text, Text: "Hi"},
	}},
	{Which: air.Z_Which_zvecvec, Zvecvec: [][]*Z{
		{
			{Which: air.Z_Which_i64, I64: 1},
			{Which: air.Z_Which_i64, I64: 2},
		},
		{
			{Which: air.Z_Which_i64, I64: 3},
			{Which: air.Z_Which_i64, I64: 4},
		},
	}},
	{Which: air.Z_Which_planebase, Planebase: nil},
	{Which: air.Z_Which_planebase, Planebase: &PlaneBase{
		Name:     "Boeing",
		Homes:    []air.Airport{air.Airport_lax, air.Airport_dfw},
		Rating:   123,
		CanFly:   true,
		Capacity: 100,
		MaxSpeed: 9001.0,
	}},
	{Which: air.Z_Which_airport, Airport: air.Airport_lax},
	{Which: air.Z_Which_grp, Grp: &ZGroup{First: 123, Second: 456}},
	{Which: air.Z_Which_echo, Echo: air.Echo_ServerToClient(simpleEcho{})},
	{Which: air.Z_Which_echo, Echo: air.Echo{Client: nil}},
	{Which: air.Z_Which_echoBases, EchoBases: EchoBases{
		Bases: []EchoBase{
			{Echo: air.Echo{Client: nil}},
			{Echo: air.Echo_ServerToClient(simpleEcho{})},
			{Echo: air.Echo{Client: nil}},
			{Echo: air.Echo_ServerToClient(simpleEcho{})},
			{Echo: air.Echo{Client: nil}},
		},
	}},
}

func TestExtract(t *testing.T) {
	for _, test := range goodTests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Errorf("NewMessage for %s: %v", zpretty.Sprint(test), err)
			continue
		}
		z, err := air.NewRootZ(seg)
		if err != nil {
			t.Errorf("NewRootZ for %s: %v", zpretty.Sprint(test), err)
			continue
		}
		if err := zfill(z, &test); err != nil {
			t.Errorf("zfill for %s: %v", zpretty.Sprint(test), err)
			continue
		}
		out := new(Z)
		if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
			t.Errorf("Extract(%v) error: %v", z, err)
		}
		if !test.equal(out) {
			t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(test))
		}
	}
}

func TestInsert(t *testing.T) {
	for _, test := range goodTests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Errorf("NewMessage for %s: %v", zpretty.Sprint(test), err)
			continue
		}
		z, err := air.NewRootZ(seg)
		if err != nil {
			t.Errorf("NewRootZ for %s: %v", zpretty.Sprint(test), err)
			continue
		}
		err = Insert(air.Z_TypeID, z.Struct, &test)
		if err != nil {
			t.Errorf("Insert(%s) error: %v", zpretty.Sprint(test), err)
		}
		if equal, err := zequal(&test, z); err != nil {
			t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(test), err)
		} else if !equal {
			t.Errorf("Insert(%s) produced %v", zpretty.Sprint(test), z)
		}
	}
}

func TestInsert_Size(t *testing.T) {
	const baseSize = 8
	tests := []struct {
		name string
		sz   capnp.ObjectSize
		z    Z
		ok   bool
	}{
		{
			name: "void into empty",
			z:    Z{Which: air.Z_Which_void},
		},
		{
			name: "void into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_void},
			ok:   true,
		},
		{
			name: "void into 1-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 1},
			z:    Z{Which: air.Z_Which_void},
			ok:   true,
		},
		{
			name: "bool into empty",
			z:    Z{Which: air.Z_Which_bool, Bool: true},
		},
		{
			name: "bool into 0 byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_bool, Bool: true},
		},
		{
			name: "bool into 1 byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 1},
			z:    Z{Which: air.Z_Which_bool, Bool: true},
			ok:   true,
		},
		{
			name: "bool into 0 byte, 1-pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z:    Z{Which: air.Z_Which_bool, Bool: true},
		},
		{
			name: "int8 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_i8, I8: 123},
		},
		{
			name: "int8 into 1-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 1},
			z:    Z{Which: air.Z_Which_i8, I8: 123},
			ok:   true,
		},
		{
			name: "uint8 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_u8, U8: 123},
		},
		{
			name: "uint8 into 1-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 1},
			z:    Z{Which: air.Z_Which_u8, U8: 123},
			ok:   true,
		},
		{
			name: "int16 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_i16, I16: 123},
		},
		{
			name: "int16 into 2-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 2},
			z:    Z{Which: air.Z_Which_i16, I16: 123},
			ok:   true,
		},
		{
			name: "uint16 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_u16, U16: 123},
		},
		{
			name: "uint16 into 2-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 2},
			z:    Z{Which: air.Z_Which_u16, U16: 123},
			ok:   true,
		},
		{
			name: "enum into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_airport, Airport: air.Airport_jfk},
		},
		{
			name: "enum into 2-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 2},
			z:    Z{Which: air.Z_Which_airport, Airport: air.Airport_jfk},
			ok:   true,
		},
		{
			name: "int32 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_i32, I32: 123},
		},
		{
			name: "int32 into 4-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 4},
			z:    Z{Which: air.Z_Which_i32, I32: 123},
			ok:   true,
		},
		{
			name: "uint32 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_u32, U32: 123},
		},
		{
			name: "uint32 into 4-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 4},
			z:    Z{Which: air.Z_Which_u32, U32: 123},
			ok:   true,
		},
		{
			name: "float32 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_f32, F32: 123},
		},
		{
			name: "float32 into 4-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 4},
			z:    Z{Which: air.Z_Which_f32, F32: 123},
			ok:   true,
		},
		{
			name: "int64 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_i64, I64: 123},
		},
		{
			name: "int64 into 8-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 8},
			z:    Z{Which: air.Z_Which_i64, I64: 123},
			ok:   true,
		},
		{
			name: "uint64 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_u64, U64: 123},
		},
		{
			name: "uint64 into 8-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 8},
			z:    Z{Which: air.Z_Which_u64, U64: 123},
			ok:   true,
		},
		{
			name: "float64 into 0-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize},
			z:    Z{Which: air.Z_Which_f64, F64: 123},
		},
		{
			name: "float64 into 8-byte",
			sz:   capnp.ObjectSize{DataSize: baseSize + 8},
			z:    Z{Which: air.Z_Which_f64, F64: 123},
			ok:   true,
		},
		{
			name: "text into 0 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 0},
			z:    Z{Which: air.Z_Which_text, Text: "hi"},
		},
		{
			name: "text into 1 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z:    Z{Which: air.Z_Which_text, Text: "hi"},
			ok:   true,
		},
		{
			name: "data into 0 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 0},
			z:    Z{Which: air.Z_Which_blob, Blob: []byte("hi")},
		},
		{
			name: "data into 1 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z:    Z{Which: air.Z_Which_blob, Blob: []byte("hi")},
			ok:   true,
		},
		{
			name: "list into 0 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 0},
			z:    Z{Which: air.Z_Which_f64vec, F64vec: []float64{123}},
		},
		{
			name: "list into 1 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z:    Z{Which: air.Z_Which_f64vec, F64vec: []float64{123}},
			ok:   true,
		},
		{
			name: "struct into 0 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 0},
			z:    Z{Which: air.Z_Which_planebase, Planebase: new(PlaneBase)},
		},
		{
			name: "struct into 1 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z:    Z{Which: air.Z_Which_planebase, Planebase: new(PlaneBase)},
			ok:   true,
		},
		{
			name: "interface into 0 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 0},
			z: Z{
				Which: air.Z_Which_echo,
				Echo:  air.Echo_ServerToClient(simpleEcho{}),
			},
		},
		{
			name: "interface into 1 pointer",
			sz:   capnp.ObjectSize{DataSize: baseSize, PointerCount: 1},
			z: Z{
				Which: air.Z_Which_echo,
				Echo:  air.Echo_ServerToClient(simpleEcho{}),
			},
			ok: true,
		},
	}
	for _, test := range tests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Errorf("%s: NewMessage: %v", test.name, err)
			continue
		}
		st, err := capnp.NewRootStruct(seg, test.sz)
		if err != nil {
			t.Errorf("%s: NewRootStruct(seg, %v): %v", test.name, test.sz, err)
			continue
		}
		err = Insert(air.Z_TypeID, st, &test.z)
		if test.ok && err != nil {
			t.Errorf("%s: Insert(%#x, capnp.NewStruct(seg, %v), %s) = %v; want nil", test.name, uint64(air.Z_TypeID), test.sz, zpretty.Sprint(test.z), err)
		}
		if !test.ok && err == nil {
			t.Errorf("%s: Insert(%#x, capnp.NewStruct(seg, %v), %s) = nil; want error about not fitting", test.name, uint64(air.Z_TypeID), test.sz, zpretty.Sprint(test.z))
		}
	}
}

type BytesZ struct {
	Which   air.Z_Which
	Text    []byte
	Textvec [][]byte
}

func TestExtract_StringBytes(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	err = zfill(z, &Z{Which: air.Z_Which_text, Text: "Hello, World!"})
	if err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(BytesZ)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract(%v) error: %v", z, err)
	}
	want := &BytesZ{Which: air.Z_Which_text, Text: []byte("Hello, World!")}
	if out.Which != want.Which || !bytes.Equal(out.Text, want.Text) {
		t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(want))
	}
}

func TestExtract_StringListBytes(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	err = zfill(z, &Z{Which: air.Z_Which_textvec, Textvec: []string{"Holmes", "Watson"}})
	if err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(BytesZ)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract(%v) error: %v", z, err)
	}
	want := &BytesZ{Which: air.Z_Which_textvec, Textvec: [][]byte{[]byte("Holmes"), []byte("Watson")}}
	eq := sliceeq(len(out.Textvec), len(want.Textvec), func(i int) bool {
		return bytes.Equal(out.Textvec[i], want.Textvec[i])
	})
	if out.Which != want.Which || !eq {
		t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(want))
	}
}

func TestInsert_StringBytes(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	bz := &BytesZ{Which: air.Z_Which_text, Text: []byte("Hello, World!")}
	err = Insert(air.Z_TypeID, z.Struct, bz)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(bz), err)
	}
	want := &Z{Which: air.Z_Which_text, Text: "Hello, World!"}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(bz), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(bz), z)
	}
}

func TestInsert_StringListBytes(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	bz := &BytesZ{Which: air.Z_Which_textvec, Textvec: [][]byte{[]byte("Holmes"), []byte("Watson")}}
	err = Insert(air.Z_TypeID, z.Struct, bz)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(bz), err)
	}
	want := &Z{Which: air.Z_Which_textvec, Textvec: []string{"Holmes", "Watson"}}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(bz), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(bz), z)
	}
}

// StructZ is a variant of Z that has direct structs instead of pointers.
type StructZ struct {
	Which     air.Z_Which
	Zvec      []Z
	Planebase PlaneBase
	Grp       ZGroup
}

func (z *StructZ) equal(y *StructZ) bool {
	if z.Which != y.Which {
		return false
	}
	switch z.Which {
	case air.Z_Which_zvec:
		return sliceeq(len(z.Zvec), len(y.Zvec), func(i int) bool {
			return z.Zvec[i].equal(&y.Zvec[i])
		})
	case air.Z_Which_planebase:
		return z.Planebase.equal(&y.Planebase)
	case air.Z_Which_grp:
		return z.Grp.First == y.Grp.First && z.Grp.Second == y.Grp.Second
	default:
		panic("unknown Z which")
	}
}

func TestExtract_StructNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	err = zfill(z, &Z{Which: air.Z_Which_planebase, Planebase: &PlaneBase{Name: "foo"}})
	if err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(StructZ)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract(%v) error: %v", z, err)
	}
	want := &StructZ{Which: air.Z_Which_planebase, Planebase: PlaneBase{Name: "foo"}}
	if !out.equal(want) {
		t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(want))
	}
}

func TestExtract_StructListNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	err = zfill(z, &Z{Which: air.Z_Which_zvec, Zvec: []*Z{
		{Which: air.Z_Which_i64, I64: 123},
	}})
	if err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(StructZ)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract(%v) error: %v", z, err)
	}
	want := &StructZ{Which: air.Z_Which_zvec, Zvec: []Z{
		{Which: air.Z_Which_i64, I64: 123},
	}}
	if !out.equal(want) {
		t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(want))
	}
}

func TestExtract_GroupNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	err = zfill(z, &Z{Which: air.Z_Which_grp, Grp: &ZGroup{First: 123, Second: 456}})
	if err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(StructZ)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract(%v) error: %v", z, err)
	}
	want := &StructZ{Which: air.Z_Which_grp, Grp: ZGroup{First: 123, Second: 456}}
	if !out.equal(want) {
		t.Errorf("Extract(%v) produced %s; want %s", z, zpretty.Sprint(out), zpretty.Sprint(want))
	}
}

func TestInsert_StructNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	bz := &StructZ{Which: air.Z_Which_planebase, Planebase: PlaneBase{Name: "foo"}}
	err = Insert(air.Z_TypeID, z.Struct, bz)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(bz), err)
	}
	want := &Z{Which: air.Z_Which_planebase, Planebase: &PlaneBase{Name: "foo"}}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(bz), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(bz), z)
	}
}

func TestInsert_StructListNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	bz := &StructZ{Which: air.Z_Which_zvec, Zvec: []Z{
		{Which: air.Z_Which_i64, I64: 123},
	}}
	err = Insert(air.Z_TypeID, z.Struct, bz)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(bz), err)
	}
	want := &Z{Which: air.Z_Which_zvec, Zvec: []*Z{
		{Which: air.Z_Which_i64, I64: 123},
	}}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(bz), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(bz), z)
	}
}

func TestInsert_GroupNoPtr(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	bz := &StructZ{Which: air.Z_Which_grp, Grp: ZGroup{First: 123, Second: 456}}
	err = Insert(air.Z_TypeID, z.Struct, bz)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(bz), err)
	}
	want := &Z{Which: air.Z_Which_grp, Grp: &ZGroup{First: 123, Second: 456}}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(bz), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(bz), z)
	}
}

// TagZ is a variant of Z that has tags.
type TagZ struct {
	Which   air.Z_Which
	Float64 float64 `capnp:"f64"`
	I64     int64   `capnp:"-"`
	U8      bool    `capnp:"bool"`
}

func TestExtract_Tags(t *testing.T) {
	tests := []struct {
		name string
		z    Z
		tagz TagZ
	}{
		{
			name: "renamed field",
			z:    Z{Which: air.Z_Which_f64, F64: 3.5},
			tagz: TagZ{Which: air.Z_Which_f64, Float64: 3.5},
		},
		{
			name: "omitted field",
			z:    Z{Which: air.Z_Which_i64, I64: 42},
			tagz: TagZ{Which: air.Z_Which_i64},
		},
		{
			name: "field with overlapping name",
			z:    Z{Which: air.Z_Which_bool, Bool: true},
			tagz: TagZ{Which: air.Z_Which_bool, U8: true},
		},
	}
	for _, test := range tests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Errorf("%s: NewMessage: %v", test.name, err)
			continue
		}
		z, err := air.NewRootZ(seg)
		if err != nil {
			t.Errorf("%s: NewRootZ: %v", test.name, err)
			continue
		}
		if err := zfill(z, &test.z); err != nil {
			t.Errorf("%s: zfill: %v", test.name, err)
			continue
		}
		out := new(TagZ)
		if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
			t.Errorf("%s: Extract error: %v", test.name, err)
		}
		if *out != test.tagz {
			t.Errorf("%s: Extract produced %s; want %s", test.name, zpretty.Sprint(out), zpretty.Sprint(test.tagz))
		}
	}
}

func TestInsert_Tags(t *testing.T) {
	tests := []struct {
		name string
		tagz TagZ
		z    Z
	}{
		{
			name: "renamed field",
			tagz: TagZ{Which: air.Z_Which_f64, Float64: 3.5},
			z:    Z{Which: air.Z_Which_f64, F64: 3.5},
		},
		{
			name: "omitted field",
			tagz: TagZ{Which: air.Z_Which_i64, I64: 42},
			z:    Z{Which: air.Z_Which_i64, I64: 0},
		},
		{
			name: "field with overlapping name",
			tagz: TagZ{Which: air.Z_Which_bool, U8: true},
			z:    Z{Which: air.Z_Which_bool, Bool: true},
		},
	}
	for _, test := range tests {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			t.Errorf("%s: NewMessage: %v", test.name, err)
			continue
		}
		z, err := air.NewRootZ(seg)
		if err != nil {
			t.Errorf("%s: NewRootZ: %v", test.name, err)
			continue
		}
		err = Insert(air.Z_TypeID, z.Struct, &test.tagz)
		if err != nil {
			t.Errorf("%s: Insert(%s) error: %v", test.name, zpretty.Sprint(test.tagz), err)
		}
		if equal, err := zequal(&test.z, z); err != nil {
			t.Errorf("%s: Insert(%s) compare err: %v", test.name, zpretty.Sprint(test.tagz), err)
		} else if !equal {
			t.Errorf("%s: Insert(%s) produced %v", test.name, zpretty.Sprint(test.tagz), z)
		}
	}
}

type ZBool struct {
	Which struct{} `capnp:",which=bool"`
	Bool  bool
}

func TestExtract_WhichTag(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	if err := zfill(z, &Z{Which: air.Z_Which_bool, Bool: true}); err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(ZBool)
	if err := Extract(out, air.Z_TypeID, z.Struct); err != nil {
		t.Errorf("Extract error: %v", err)
	}
	if !out.Bool {
		t.Errorf("Extract produced %s; want %s", zpretty.Sprint(out), zpretty.Sprint(&ZBool{Bool: true}))
	}
}

func TestExtract_WhichTagMismatch(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	if err := zfill(z, &Z{Which: air.Z_Which_i64, I64: 42}); err != nil {
		t.Fatalf("zfill: %v", err)
	}
	out := new(ZBool)
	if err := Extract(out, air.Z_TypeID, z.Struct); err == nil {
		t.Error("Extract did not return an error")
	}
}

func TestInsert_WhichTag(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	zb := &ZBool{Bool: true}
	err = Insert(air.Z_TypeID, z.Struct, zb)
	if err != nil {
		t.Errorf("Insert(%s) error: %v", zpretty.Sprint(zb), err)
	}
	want := &Z{Which: air.Z_Which_bool, Bool: true}
	if equal, err := zequal(want, z); err != nil {
		t.Errorf("Insert(%s) compare err: %v", zpretty.Sprint(zb), err)
	} else if !equal {
		t.Errorf("Insert(%s) produced %v", zpretty.Sprint(zb), z)
	}
}

type ZBoolWithExtra struct {
	Which      struct{} `capnp:",which=bool"`
	Bool       bool
	ExtraField uint16
}

func TestExtraFields(t *testing.T) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	z, err := air.NewRootZ(seg)
	if err != nil {
		t.Fatalf("NewRootZ: %v", err)
	}
	zb := &ZBoolWithExtra{Bool: true, ExtraField: 42}
	err = Insert(air.Z_TypeID, z.Struct, zb)
	if err == nil {
		t.Errorf("Insert(%s) did not return error", zpretty.Sprint(zb))
	}
	err = Extract(zb, air.Z_TypeID, z.Struct)
	if err == nil {
		t.Errorf("Extract(%v) did not return error", z)
	}
	if zb.ExtraField != 42 {
		t.Errorf("zb.ExtraField modified to %d; want 42", zb.ExtraField)
	}
}

func zequal(g *Z, c air.Z) (bool, error) {
	if g.Which != c.Which() {
		return false, nil
	}
	listeq := func(has bool, n int, l capnp.List, f func(i int) (bool, error)) (bool, error) {
		if has != l.IsValid() {
			return false, nil
		}
		if !has {
			return true, nil
		}
		if l.Len() != n {
			return false, nil
		}
		for i := 0; i < l.Len(); i++ {
			if ok, err := f(i); !ok || err != nil {
				return ok, err
			}
		}
		return true, nil
	}
	switch g.Which {
	case air.Z_Which_f64:
		return g.F64 == c.F64(), nil
	case air.Z_Which_f32:
		return g.F32 == c.F32(), nil
	case air.Z_Which_i64:
		return g.I64 == c.I64(), nil
	case air.Z_Which_i32:
		return g.I32 == c.I32(), nil
	case air.Z_Which_i16:
		return g.I16 == c.I16(), nil
	case air.Z_Which_i8:
		return g.I8 == c.I8(), nil
	case air.Z_Which_u64:
		return g.U64 == c.U64(), nil
	case air.Z_Which_u32:
		return g.U32 == c.U32(), nil
	case air.Z_Which_u16:
		return g.U16 == c.U16(), nil
	case air.Z_Which_u8:
		return g.U8 == c.U8(), nil
	case air.Z_Which_bool:
		return g.Bool == c.Bool(), nil
	case air.Z_Which_text:
		text, err := c.Text()
		if err != nil {
			return false, err
		}
		return g.Text == text, nil
	case air.Z_Which_blob:
		blob, err := c.Blob()
		if err != nil {
			return false, err
		}
		if (blob == nil) != (g.Blob == nil) {
			return false, nil
		}
		return bytes.Equal(g.Blob, blob), nil
	case air.Z_Which_f64vec:
		fv, err := c.F64vec()
		if err != nil {
			return false, err
		}
		return listeq(g.F64vec != nil, len(g.F64vec), fv.List, func(i int) (bool, error) {
			return fv.At(i) == g.F64vec[i], nil
		})
	case air.Z_Which_f32vec:
		fv, err := c.F32vec()
		if err != nil {
			return false, err
		}
		return listeq(g.F32vec != nil, len(g.F32vec), fv.List, func(i int) (bool, error) {
			return fv.At(i) == g.F32vec[i], nil
		})
	case air.Z_Which_i64vec:
		iv, err := c.I64vec()
		if err != nil {
			return false, err
		}
		return listeq(g.I64vec != nil, len(g.I64vec), iv.List, func(i int) (bool, error) {
			return iv.At(i) == g.I64vec[i], nil
		})
	case air.Z_Which_i8vec:
		iv, err := c.I8vec()
		if err != nil {
			return false, err
		}
		return listeq(g.I8vec != nil, len(g.I8vec), iv.List, func(i int) (bool, error) {
			return iv.At(i) == g.I8vec[i], nil
		})
	case air.Z_Which_u64vec:
		uv, err := c.U64vec()
		if err != nil {
			return false, err
		}
		return listeq(g.U64vec != nil, len(g.U64vec), uv.List, func(i int) (bool, error) {
			return uv.At(i) == g.U64vec[i], nil
		})
	case air.Z_Which_u8vec:
		uv, err := c.U8vec()
		if err != nil {
			return false, err
		}
		return listeq(g.U8vec != nil, len(g.U8vec), uv.List, func(i int) (bool, error) {
			return uv.At(i) == g.U8vec[i], nil
		})
	case air.Z_Which_boolvec:
		bv, err := c.Boolvec()
		if err != nil {
			return false, err
		}
		return listeq(g.Boolvec != nil, len(g.Boolvec), bv.List, func(i int) (bool, error) {
			return bv.At(i) == g.Boolvec[i], nil
		})
	case air.Z_Which_datavec:
		dv, err := c.Datavec()
		if err != nil {
			return false, err
		}
		return listeq(g.Datavec != nil, len(g.Datavec), dv.List, func(i int) (bool, error) {
			di, err := dv.At(i)
			return bytes.Equal(di, g.Datavec[i]), err
		})
	case air.Z_Which_textvec:
		tv, err := c.Textvec()
		if err != nil {
			return false, err
		}
		return listeq(g.Textvec != nil, len(g.Textvec), tv.List, func(i int) (bool, error) {
			s, err := tv.At(i)
			return s == g.Textvec[i], err
		})
	case air.Z_Which_zvec:
		vec, err := c.Zvec()
		if err != nil {
			return false, err
		}
		return listeq(g.Zvec != nil, len(g.Zvec), vec.List, func(i int) (bool, error) {
			return zequal(g.Zvec[i], vec.At(i))
		})
	case air.Z_Which_zvecvec:
		vv, err := c.Zvecvec()
		if err != nil {
			return false, err
		}
		return listeq(g.Zvecvec != nil, len(g.Zvecvec), vv.List, func(i int) (bool, error) {
			p, err := vv.PtrAt(i)
			if err != nil {
				return false, err
			}
			v := air.Z_List{List: p.List()}
			return listeq(g.Zvecvec[i] != nil, len(g.Zvecvec[i]), v.List, func(j int) (bool, error) {
				return zequal(g.Zvecvec[i][j], v.At(j))
			})
		})
	case air.Z_Which_planebase:
		pb, err := c.Planebase()
		if err != nil {
			return false, err
		}
		if (g.Planebase != nil) != pb.IsValid() {
			return false, nil
		}
		if g.Planebase == nil {
			return true, nil
		}
		name, err := pb.Name()
		if err != nil {
			return false, err
		}
		if g.Planebase.Name != name {
			return false, nil
		}
		homes, err := pb.Homes()
		if err != nil {
			return false, err
		}
		homeseq, _ := listeq(g.Planebase.Homes != nil, len(g.Planebase.Homes), homes.List, func(i int) (bool, error) {
			return g.Planebase.Homes[i] == homes.At(i), nil
		})
		if !homeseq {
			return false, nil
		}
		return g.Planebase.Rating == pb.Rating() && g.Planebase.CanFly == pb.CanFly() && g.Planebase.Capacity == pb.Capacity() && g.Planebase.MaxSpeed == pb.MaxSpeed(), nil
	case air.Z_Which_airport:
		return g.Airport == c.Airport(), nil
	case air.Z_Which_grp:
		if g.Grp == nil {
			return false, nil
		}
		return g.Grp.First == c.Grp().First() && g.Grp.Second == c.Grp().Second(), nil
	case air.Z_Which_echo:
		return g.Echo.Client == nil && !c.HasEcho() ||
			g.Echo.Client != nil && c.HasEcho(), nil
	case air.Z_Which_echoBases:
		echoBases, err := c.EchoBases()
		if err != nil {
			return false, err
		}
		list, err := echoBases.Bases()
		if err != nil {
			return false, err
		}
		return listeq(g.EchoBases.Bases != nil, len(g.EchoBases.Bases), list.List, func(i int) (bool, error) {
			return (g.EchoBases.Bases[i].Echo.Client == nil) == !list.At(i).HasEcho(), nil
		})
	default:
		return false, fmt.Errorf("zequal: unknown type: %v", g.Which)
	}
}

func (z *Z) equal(y *Z) bool {
	if z.Which != y.Which {
		return false
	}
	switch z.Which {
	case air.Z_Which_f64:
		return z.F64 == y.F64
	case air.Z_Which_f32:
		return z.F32 == y.F32
	case air.Z_Which_i64:
		return z.I64 == y.I64
	case air.Z_Which_i32:
		return z.I32 == y.I32
	case air.Z_Which_i16:
		return z.I16 == y.I16
	case air.Z_Which_i8:
		return z.I8 == y.I8
	case air.Z_Which_u64:
		return z.U64 == y.U64
	case air.Z_Which_u32:
		return z.U32 == y.U32
	case air.Z_Which_u16:
		return z.U16 == y.U16
	case air.Z_Which_u8:
		return z.U8 == y.U8
	case air.Z_Which_bool:
		return z.Bool == y.Bool
	case air.Z_Which_text:
		return z.Text == y.Text
	case air.Z_Which_blob:
		return bytes.Equal(z.Blob, y.Blob)
	case air.Z_Which_f64vec:
		return sliceeq(len(z.F64vec), len(y.F64vec), func(i int) bool {
			return z.F64vec[i] == y.F64vec[i]
		})
	case air.Z_Which_f32vec:
		return sliceeq(len(z.F32vec), len(y.F32vec), func(i int) bool {
			return z.F32vec[i] == y.F32vec[i]
		})
	case air.Z_Which_i64vec:
		return sliceeq(len(z.I64vec), len(y.I64vec), func(i int) bool {
			return z.I64vec[i] == y.I64vec[i]
		})
	case air.Z_Which_i8vec:
		return sliceeq(len(z.I8vec), len(y.I8vec), func(i int) bool {
			return z.I8vec[i] == y.I8vec[i]
		})
	case air.Z_Which_u64vec:
		return sliceeq(len(z.U64vec), len(y.U64vec), func(i int) bool {
			return z.U64vec[i] == y.U64vec[i]
		})
	case air.Z_Which_u8vec:
		return sliceeq(len(z.U8vec), len(y.U8vec), func(i int) bool {
			return z.U8vec[i] == y.U8vec[i]
		})
	case air.Z_Which_boolvec:
		return sliceeq(len(z.Boolvec), len(y.Boolvec), func(i int) bool {
			return z.Boolvec[i] == y.Boolvec[i]
		})
	case air.Z_Which_datavec:
		return sliceeq(len(z.Datavec), len(y.Datavec), func(i int) bool {
			return bytes.Equal(z.Datavec[i], y.Datavec[i])
		})
	case air.Z_Which_textvec:
		return sliceeq(len(z.Textvec), len(y.Textvec), func(i int) bool {
			return z.Textvec[i] == y.Textvec[i]
		})
	case air.Z_Which_zvec:
		return sliceeq(len(z.Zvec), len(y.Zvec), func(i int) bool {
			return z.Zvec[i].equal(y.Zvec[i])
		})
	case air.Z_Which_zvecvec:
		return sliceeq(len(z.Zvecvec), len(y.Zvecvec), func(i int) bool {
			return sliceeq(len(z.Zvecvec[i]), len(y.Zvecvec[i]), func(j int) bool {
				return z.Zvecvec[i][j].equal(y.Zvecvec[i][j])
			})
		})
	case air.Z_Which_planebase:
		return z.Planebase.equal(y.Planebase)
	case air.Z_Which_airport:
		return z.Airport == y.Airport
	case air.Z_Which_grp:
		return z.Grp.First == y.Grp.First && z.Grp.Second == y.Grp.Second
	case air.Z_Which_echo:
		return (z.Echo.Client == nil) == (y.Echo.Client == nil)
	case air.Z_Which_echoBases:
		return sliceeq(len(z.EchoBases.Bases), len(y.EchoBases.Bases), func(i int) bool {
			return (z.EchoBases.Bases[i].Echo.Client == nil) ==
				(y.EchoBases.Bases[i].Echo.Client == nil)
		})
	default:
		panic("unknown Z which")
	}
}

func zfill(c air.Z, g *Z) error {
	switch g.Which {
	case air.Z_Which_f64:
		c.SetF64(g.F64)
	case air.Z_Which_f32:
		c.SetF32(g.F32)
	case air.Z_Which_i64:
		c.SetI64(g.I64)
	case air.Z_Which_i32:
		c.SetI32(g.I32)
	case air.Z_Which_i16:
		c.SetI16(g.I16)
	case air.Z_Which_i8:
		c.SetI8(g.I8)
	case air.Z_Which_u64:
		c.SetU64(g.U64)
	case air.Z_Which_u32:
		c.SetU32(g.U32)
	case air.Z_Which_u16:
		c.SetU16(g.U16)
	case air.Z_Which_u8:
		c.SetU8(g.U8)
	case air.Z_Which_bool:
		c.SetBool(g.Bool)
	case air.Z_Which_text:
		return c.SetText(g.Text)
	case air.Z_Which_blob:
		return c.SetBlob(g.Blob)
	case air.Z_Which_f64vec:
		if g.F64vec == nil {
			return c.SetF64vec(capnp.Float64List{})
		}
		fv, err := c.NewF64vec(int32(len(g.F64vec)))
		if err != nil {
			return err
		}
		for i, f := range g.F64vec {
			fv.Set(i, f)
		}
	case air.Z_Which_f32vec:
		if g.F32vec == nil {
			return c.SetF32vec(capnp.Float32List{})
		}
		fv, err := c.NewF32vec(int32(len(g.F32vec)))
		if err != nil {
			return err
		}
		for i, f := range g.F32vec {
			fv.Set(i, f)
		}
	case air.Z_Which_i64vec:
		if g.I64vec == nil {
			return c.SetI64vec(capnp.Int64List{})
		}
		iv, err := c.NewI64vec(int32(len(g.I64vec)))
		if err != nil {
			return err
		}
		for i, n := range g.I64vec {
			iv.Set(i, n)
		}
	case air.Z_Which_i8vec:
		if g.I8vec == nil {
			return c.SetI8vec(capnp.Int8List{})
		}
		iv, err := c.NewI8vec(int32(len(g.I8vec)))
		if err != nil {
			return err
		}
		for i, n := range g.I8vec {
			iv.Set(i, n)
		}
	case air.Z_Which_u64vec:
		if g.U64vec == nil {
			return c.SetU64vec(capnp.UInt64List{})
		}
		uv, err := c.NewU64vec(int32(len(g.U64vec)))
		if err != nil {
			return err
		}
		for i, n := range g.U64vec {
			uv.Set(i, n)
		}
	case air.Z_Which_u8vec:
		if g.U8vec == nil {
			return c.SetU8vec(capnp.UInt8List{})
		}
		uv, err := c.NewU8vec(int32(len(g.U8vec)))
		if err != nil {
			return err
		}
		for i, n := range g.U8vec {
			uv.Set(i, n)
		}
	case air.Z_Which_boolvec:
		if g.Boolvec == nil {
			return c.SetBoolvec(capnp.BitList{})
		}
		vec, err := c.NewBoolvec(int32(len(g.Boolvec)))
		if err != nil {
			return err
		}
		for i, v := range g.Boolvec {
			vec.Set(i, v)
		}
	case air.Z_Which_datavec:
		if g.Datavec == nil {
			return c.SetDatavec(capnp.DataList{})
		}
		vec, err := c.NewDatavec(int32(len(g.Datavec)))
		if err != nil {
			return err
		}
		for i, v := range g.Datavec {
			if err := vec.Set(i, v); err != nil {
				return err
			}
		}
	case air.Z_Which_textvec:
		if g.Textvec == nil {
			return c.SetTextvec(capnp.TextList{})
		}
		vec, err := c.NewTextvec(int32(len(g.Textvec)))
		if err != nil {
			return err
		}
		for i, v := range g.Textvec {
			if err := vec.Set(i, v); err != nil {
				return err
			}
		}
	case air.Z_Which_zvec:
		if g.Zvec == nil {
			return c.SetZvec(air.Z_List{})
		}
		vec, err := c.NewZvec(int32(len(g.Zvec)))
		if err != nil {
			return err
		}
		for i, z := range g.Zvec {
			if err := zfill(vec.At(i), z); err != nil {
				return err
			}
		}
	case air.Z_Which_zvecvec:
		if g.Zvecvec == nil {
			return c.SetZvecvec(capnp.PointerList{})
		}
		vv, err := c.NewZvecvec(int32(len(g.Zvecvec)))
		if err != nil {
			return err
		}
		for i, zz := range g.Zvecvec {
			v, err := air.NewZ_List(vv.Segment(), int32(len(zz)))
			if err != nil {
				return err
			}
			if err := vv.SetPtr(i, v.ToPtr()); err != nil {
				return err
			}
			for j, z := range zz {
				if err := zfill(v.At(j), z); err != nil {
					return err
				}
			}
		}
	case air.Z_Which_planebase:
		if g.Planebase == nil {
			return c.SetPlanebase(air.PlaneBase{})
		}
		pb, err := c.NewPlanebase()
		if err != nil {
			return err
		}
		if err := pb.SetName(g.Planebase.Name); err != nil {
			return err
		}
		if g.Planebase.Homes != nil {
			homes, err := pb.NewHomes(int32(len(g.Planebase.Homes)))
			if err != nil {
				return err
			}
			for i := range g.Planebase.Homes {
				homes.Set(i, g.Planebase.Homes[i])
			}
		}
		pb.SetRating(g.Planebase.Rating)
		pb.SetCanFly(g.Planebase.CanFly)
		pb.SetCapacity(g.Planebase.Capacity)
		pb.SetMaxSpeed(g.Planebase.MaxSpeed)
	case air.Z_Which_airport:
		c.SetAirport(g.Airport)
	case air.Z_Which_grp:
		c.SetGrp()
		if g.Grp != nil {
			c.Grp().SetFirst(g.Grp.First)
			c.Grp().SetSecond(g.Grp.Second)
		}
	case air.Z_Which_echo:
		c.SetEcho(g.Echo)
	case air.Z_Which_echoBases:
		echoBases, err := c.NewEchoBases()
		if err != nil {
			return err
		}
		list, err := echoBases.NewBases(int32(len(g.EchoBases.Bases)))
		if err != nil {
			return err
		}
		for i, v := range g.EchoBases.Bases {
			base := list.At(i)
			base.SetEcho(v.Echo)
		}
	default:
		return fmt.Errorf("zfill: unknown type: %v", g.Which)
	}
	return nil
}

var zpretty = &pretty.Config{
	Compact:        true,
	SkipZeroFields: true,
}

func sliceeq(na, nb int, f func(i int) bool) bool {
	if na != nb {
		return false
	}
	for i := 0; i < na; i++ {
		if !f(i) {
			return false
		}
	}
	return true
}
