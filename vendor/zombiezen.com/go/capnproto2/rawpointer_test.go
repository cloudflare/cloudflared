package capnp

import (
	"testing"
)

func TestPointerOffsetResolve(t *testing.T) {
	tests := []struct {
		off      pointerOffset
		base     Address
		resolved Address
		ok       bool
	}{
		{off: 0, base: 0, resolved: 0, ok: true},
		{off: 1, base: 0, resolved: 8, ok: true},
		{off: 1, base: 16, resolved: 24, ok: true},
		{off: -1, base: 32, resolved: 24, ok: true},
		{off: -1, base: 0, ok: false},
		{off: -4, base: 0, ok: false},
		{off: -4, base: 32, resolved: 0, ok: true},
		{off: -5, base: 32, ok: false},
		{off: 0, base: 0xfffffff8, resolved: 0xfffffff8, ok: true},
		{off: 1, base: 0xfffffff8, ok: false},
		{off: -1, base: 0xfffffff8, resolved: 0xfffffff0, ok: true},
	}
	for _, test := range tests {
		resolved, ok := test.off.resolve(test.base)
		if test.ok && (resolved != test.resolved || ok != test.ok) {
			t.Errorf("%v.resolve(%v) = %v, true; want %v, true", test.off, test.base, resolved, test.resolved)
		} else if !test.ok && ok {
			t.Errorf("%v.resolve(%v) = %v, true; want _, false", test.off, test.base, resolved)
		}
	}
}

func TestRawStructPointer(t *testing.T) {
	tests := []struct {
		ptr    rawPointer
		offset pointerOffset
		size   ObjectSize
	}{
		{0x0000000000000000, 0, ObjectSize{}},
		{0x0000000000000004, 1, ObjectSize{}},
		{0x0000000000000008, 2, ObjectSize{}},
		{0x000000000000000c, 3, ObjectSize{}},
		{0x0000000000000010, 4, ObjectSize{}},
		{0x0403020100000000, 0, ObjectSize{DataSize: 0x0201 * 8, PointerCount: 0x0403}},
		{0xffffffff00000000, 0, ObjectSize{DataSize: 0xffff * 8, PointerCount: 0xffff}},
		{0xfffffffffffffff8, -2, ObjectSize{DataSize: 0xffff * 8, PointerCount: 0xffff}},
		{0x00000000fffffffc, -1, ObjectSize{}},
		{0x04030201fffffffc, -1, ObjectSize{DataSize: 0x0201 * 8, PointerCount: 0x0403}},
		{0xfffffffffffffffc, -1, ObjectSize{DataSize: 0xffff * 8, PointerCount: 0xffff}},
	}
	for _, test := range tests {
		if typ := test.ptr.pointerType(); typ != structPointer {
			t.Errorf("rawPointer(%#016x).pointerType() = %d; want %d", uint64(test.ptr), typ, structPointer)
		}
		if offset := test.ptr.offset(); offset != test.offset {
			t.Errorf("rawPointer(%#016x).offset() = %d; want %d", uint64(test.ptr), offset, test.offset)
		}
		if size := test.ptr.structSize(); size != test.size {
			t.Errorf("rawPointer(%#016x).structSize() = %d; want %d", uint64(test.ptr), size, test.size)
		}
	}
	for _, test := range tests {
		ptr := rawStructPointer(test.offset, test.size)
		if ptr != test.ptr {
			t.Errorf("rawStructPointer(%d, %d) = rawPointer(%#016x); want rawPointer(%#016x)", test.offset, test.size, ptr, test.ptr)
		}
	}
}

func TestRawListPointer(t *testing.T) {
	tests := []struct {
		ptr    rawPointer
		offset pointerOffset
		lt     listType
		n      int32
	}{
		{0x0000000000000001, 0, voidList, 0},
		{0x0000000000000005, 1, voidList, 0},
		{0x0000000000000009, 2, voidList, 0},
		{0x000000000000000d, 3, voidList, 0},
		{0x0000000100000001, 0, bit1List, 0},
		{0x0000000200000001, 0, byte1List, 0},
		{0x0000000300000001, 0, byte2List, 0},
		{0x0000000400000001, 0, byte4List, 0},
		{0x0000000500000001, 0, byte8List, 0},
		{0x0000000600000001, 0, pointerList, 0},
		{0x0000000700000001, 0, compositeList, 0},
		{0x0000001500000009, 2, byte8List, 2},
		{0x000000170000002d, 11, compositeList, 2},
		{0x0000001700000035, 13, compositeList, 2},
		{0xfffffff8fffffffd, -1, voidList, 0x1fffffff},
		{0xfffffff9fffffffd, -1, bit1List, 0x1fffffff},
		{0xfffffffafffffffd, -1, byte1List, 0x1fffffff},
		{0xfffffffbfffffffd, -1, byte2List, 0x1fffffff},
		{0xfffffffcfffffffd, -1, byte4List, 0x1fffffff},
		{0xfffffffdfffffffd, -1, byte8List, 0x1fffffff},
		{0xfffffffefffffffd, -1, pointerList, 0x1fffffff},
		{0xfffffffffffffff9, -2, compositeList, 0x1fffffff},
		{0xfffffffffffffffd, -1, compositeList, 0x1fffffff},
	}
	for _, test := range tests {
		if typ := test.ptr.pointerType(); typ != listPointer {
			t.Errorf("rawPointer(%#016x).pointerType() = %d; want %d", uint64(test.ptr), typ, listPointer)
		}
		if offset := test.ptr.offset(); offset != test.offset {
			t.Errorf("rawPointer(%#016x).offset() = %d; want %d", uint64(test.ptr), offset, test.offset)
		}
		if lt := test.ptr.listType(); lt != test.lt {
			t.Errorf("rawPointer(%#016x).listType() = %d; want %d", uint64(test.ptr), lt, test.lt)
		}
		if n := test.ptr.numListElements(); n != test.n {
			t.Errorf("rawPointer(%#016x).numListElements() = %d; want %d", uint64(test.ptr), n, test.n)
		}
	}
	for _, test := range tests {
		ptr := rawListPointer(test.offset, test.lt, test.n)
		if ptr != test.ptr {
			t.Errorf("rawListPointer(%d, %d, %d) = rawPointer(%#016x); want rawPointer(%#016x)", test.offset, test.lt, test.n, ptr, test.ptr)
		}
	}
}

func TestRawOtherPointer(t *testing.T) {
	tests := []struct {
		ptr rawPointer
		typ uint32
		cap CapabilityID
	}{
		{0x0000000000000003, 0, 0},
		{0x0000000000000007, 1, 0},
		{0x000000000000000b, 2, 0},
		{0x000000000000000f, 3, 0},
		{0xffffffff00000003, 0, 0xffffffff},
		{0xfffffffffffffffb, 0x3ffffffe, 0xffffffff},
		{0xffffffffffffffff, 0x3fffffff, 0xffffffff},
	}
	for _, test := range tests {
		if typ := test.ptr.pointerType(); typ != otherPointer {
			t.Errorf("rawPointer(%#016x).pointerType() = %d; want %d", uint64(test.ptr), typ, otherPointer)
		}
		if typ := test.ptr.otherPointerType(); typ != test.typ {
			t.Errorf("rawPointer(%#016x).otherPointerType() = %d; want %d", uint64(test.ptr), typ, test.typ)
		}
		if cap := test.ptr.capabilityIndex(); cap != test.cap {
			t.Errorf("rawPointer(%#016x).capabilityIndex() = %d; want %d", uint64(test.ptr), cap, test.cap)
		}
	}
	for _, test := range tests {
		if test.typ != 0 {
			continue
		}
		ptr := rawInterfacePointer(test.cap)
		if ptr != test.ptr {
			t.Errorf("rawInterfacePointer(%d) = rawPointer(%#016x); want rawPointer(%#016x)", test.cap, ptr, test.ptr)
		}
	}
}

func TestRawFarPointer(t *testing.T) {
	tests := []struct {
		ptr  rawPointer
		typ  pointerType
		addr Address
		seg  SegmentID
	}{
		{0x0000000000000002, farPointer, 0, 0},
		{0x0000000000000006, doubleFarPointer, 0, 0},
		{0x000000000000000a, farPointer, 8, 0},
		{0x000000000000000e, doubleFarPointer, 8, 0},
		{0xfffffffffffffffa, farPointer, 0xfffffff8, 0xffffffff},
		{0xfffffffffffffffe, doubleFarPointer, 0xfffffff8, 0xffffffff},
	}
	for _, test := range tests {
		if typ := test.ptr.pointerType(); typ != test.typ {
			t.Errorf("rawPointer(%#016x).pointerType() = %d; want %d", uint64(test.ptr), typ, test.typ)
		}
		if addr := test.ptr.farAddress(); addr != test.addr {
			t.Errorf("rawPointer(%#016x).farAddress() = %v; want %v", uint64(test.ptr), addr, test.addr)
		}
		if seg := test.ptr.farSegment(); seg != test.seg {
			t.Errorf("rawPointer(%#016x).farSegment() = %d; want %d", uint64(test.ptr), seg, test.seg)
		}
	}
	for _, test := range tests {
		if test.typ == farPointer {
			ptr := rawFarPointer(test.seg, test.addr)
			if ptr != test.ptr {
				t.Errorf("rawFarPointer(%d, %v) = rawPointer(%#016x); want rawPointer(%#016x)", test.seg, test.addr, ptr, test.ptr)
			}
		} else {
			ptr := rawDoubleFarPointer(test.seg, test.addr)
			if ptr != test.ptr {
				t.Errorf("rawDoubleFarPointer(%d, %v) = rawPointer(%#016x); want rawPointer(%#016x)", test.seg, test.addr, ptr, test.ptr)
			}
		}
	}
}

func TestRawPointerElementSize(t *testing.T) {
	tests := []struct {
		typ listType
		sz  ObjectSize
	}{
		{voidList, ObjectSize{}},
		{byte1List, ObjectSize{DataSize: 1}},
		{byte2List, ObjectSize{DataSize: 2}},
		{byte4List, ObjectSize{DataSize: 4}},
		{byte8List, ObjectSize{DataSize: 8}},
		{pointerList, ObjectSize{PointerCount: 1}},
	}
	for _, test := range tests {
		rp := rawListPointer(0, test.typ, 0)
		if sz := rp.elementSize(); sz != test.sz {
			t.Errorf("rawListPointer(0, %d, 0).elementSize() = %v; want %v", test.typ, sz, test.sz)
		}
	}
}

func TestRawPointerTotalListSize(t *testing.T) {
	tests := []struct {
		typ listType
		n   int32
		sz  Size
		ok  bool
	}{
		{voidList, 0, 0, true},
		{voidList, 5, 0, true},
		{bit1List, 0, 0, true},
		{bit1List, 1, 1, true},
		{bit1List, 2, 1, true},
		{bit1List, 7, 1, true},
		{bit1List, 8, 1, true},
		{bit1List, 9, 2, true},
		{compositeList, 0, 8, true},
		{compositeList, 1, 16, true},
		{compositeList, 2, 24, true},
		{byte1List, 0, 0, true},
		{byte1List, 1, 1, true},
		{byte1List, 2, 2, true},
		{byte2List, 0, 0, true},
		{byte2List, 1, 2, true},
		{byte2List, 2, 4, true},
		{byte4List, 0, 0, true},
		{byte4List, 1, 4, true},
		{byte4List, 2, 8, true},
		{byte8List, 0, 0, true},
		{byte8List, 1, 8, true},
		{byte8List, 2, 16, true},
		{pointerList, 0, 0, true},
		{pointerList, 1, 8, true},
		{pointerList, 2, 16, true},
	}
	for _, test := range tests {
		p := rawListPointer(0, test.typ, test.n)
		sz, ok := p.totalListSize()
		if ok != test.ok || (ok && sz != test.sz) {
			t.Errorf("rawListPointer(0, %d, %d).totalListSize() = %d, %t; want %d, %t", test.typ, test.n, sz, ok, test.sz, test.ok)
		}
	}
}

func TestLandingPadNearPointer(t *testing.T) {
	tests := []struct {
		far  rawPointer
		tag  rawPointer
		near rawPointer
	}{
		{rawFarPointer(0, 0), rawStructPointer(0, ObjectSize{16, 2}), rawStructPointer(0, ObjectSize{16, 2})},
		{rawFarPointer(0, 8), rawStructPointer(0, ObjectSize{16, 2}), rawStructPointer(1, ObjectSize{16, 2})},
		{rawFarPointer(0, 160), rawStructPointer(0, ObjectSize{16, 2}), rawStructPointer(20, ObjectSize{16, 2})},
		{rawFarPointer(0, 2832), rawStructPointer(0, ObjectSize{16, 2}), rawStructPointer(354, ObjectSize{16, 2})},
		{rawFarPointer(12, 2832), rawStructPointer(0, ObjectSize{16, 2}), rawStructPointer(354, ObjectSize{16, 2})},
		{rawFarPointer(12, 2832), rawStructPointer(8, ObjectSize{16, 2}), rawStructPointer(354, ObjectSize{16, 2})},
		{rawFarPointer(0, 2832), rawListPointer(0, 0, 10), rawListPointer(354, 0, 10)},
		{rawFarPointer(12, 2832), rawListPointer(0, 0, 10), rawListPointer(354, 0, 10)},
	}

	for _, test := range tests {
		near := landingPadNearPointer(test.far, test.tag)
		if near != test.near {
			t.Errorf("landingPadNearPointer(%#v, %#v) = %#v; want %#v", test.far, test.tag, near, test.near)
		}
	}
}

func TestRawPointerOffset(t *testing.T) {
	tests := []struct {
		p   rawPointer
		off pointerOffset
	}{
		{0x0000000000000000, 0},
		{0x0000000000000001, 0},
		{0xffffffff00000000, 0},
		{0xffffffff00000001, 0},

		{0x0000000000000004, 1},
		{0x0000000000000005, 1},
		{0xffffffff00000004, 1},
		{0xffffffff00000005, 1},

		{0x000000007ffffff8, 0x1ffffffe},
		{0x000000007ffffff9, 0x1ffffffe},
		{0xffffffff7ffffff8, 0x1ffffffe},
		{0xffffffff7ffffff9, 0x1ffffffe},

		{0x000000007ffffffc, 0x1fffffff},
		{0x000000007ffffffd, 0x1fffffff},
		{0xffffffff7ffffffc, 0x1fffffff},
		{0xffffffff7ffffffd, 0x1fffffff},

		{0x00000000fffffffc, -1},
		{0x00000000fffffffd, -1},
		{0xfffffffffffffffc, -1},
		{0xfffffffffffffffd, -1},

		{0x00000000fffffff8, -2},
		{0x00000000fffffff9, -2},
		{0xfffffffffffffff8, -2},
		{0xfffffffffffffff9, -2},

		{0x0000000080000004, -0x1fffffff},
		{0x0000000080000005, -0x1fffffff},
		{0xffffffff80000004, -0x1fffffff},
		{0xffffffff80000005, -0x1fffffff},

		{0x0000000080000000, -0x20000000},
		{0x0000000080000001, -0x20000000},
		{0xffffffff80000000, -0x20000000},
		{0xffffffff80000001, -0x20000000},
	}
	for _, test := range tests {
		off := test.p.offset()
		if off != test.off {
			t.Errorf("rawPointer(%#016x).offset() = %d; want %d", test.p, off, test.off)
		}
	}
}

func TestRawPointerWithOffset(t *testing.T) {
	tests := []struct {
		p    rawPointer
		off  pointerOffset
		want rawPointer
	}{
		{
			p:    rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  0,
			want: rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  1,
			want: rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  -1,
			want: rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  0,
			want: rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  0,
			want: rawStructPointer(0, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  1,
			want: rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  -1,
			want: rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  1,
			want: rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
		{
			p:    rawStructPointer(1, ObjectSize{DataSize: 8, PointerCount: 2}),
			off:  -1,
			want: rawStructPointer(-1, ObjectSize{DataSize: 8, PointerCount: 2}),
		},
	}
	for _, test := range tests {
		if got := test.p.withOffset(test.off); got != test.want {
			t.Errorf("%#v.withOffset(%d) = %#v; want %#v", test.p, test.off, got, test.want)
		}
	}
}
