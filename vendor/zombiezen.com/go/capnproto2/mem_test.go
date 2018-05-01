package capnp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestNewMessage(t *testing.T) {
	tests := []struct {
		arena Arena
		fails bool
	}{
		{arena: SingleSegment(nil)},
		{arena: MultiSegment(nil)},
		{arena: readOnlyArena{SingleSegment(make([]byte, 0, 7))}, fails: true},
		{arena: readOnlyArena{SingleSegment(make([]byte, 0, 8))}},
		{arena: MultiSegment(nil)},
		{arena: MultiSegment([][]byte{make([]byte, 8)}), fails: true},
		{arena: MultiSegment([][]byte{incrementingData(8)}), fails: true},
		// This is somewhat arbitrary, but more than one segment = data.
		// This restriction may be lifted if it's not useful.
		{arena: MultiSegment([][]byte{make([]byte, 0, 16), make([]byte, 0)}), fails: true},
	}
	for _, test := range tests {
		msg, seg, err := NewMessage(test.arena)
		if err != nil {
			if !test.fails {
				t.Errorf("NewMessage(%v) failed unexpectedly: %v", test.arena, err)
			}
			continue
		}
		if test.fails {
			t.Errorf("NewMessage(%v) succeeded; want error", test.arena)
			continue
		}
		if n := msg.NumSegments(); n != 1 {
			t.Errorf("NewMessage(%v).NumSegments() = %d; want 1", test.arena, n)
		}
		if seg.ID() != 0 {
			t.Errorf("NewMessage(%v) segment.ID() = %d; want 0", test.arena, seg.ID())
		}
		if len(seg.Data()) != 8 {
			t.Errorf("NewMessage(%v) segment.Data() = % 02x; want length 8", test.arena, seg.Data())
		}
	}
}

func TestAlloc(t *testing.T) {
	type allocTest struct {
		name string

		seg  *Segment
		size Size

		allocID SegmentID
		addr    Address
	}
	var tests []allocTest

	{
		msg := &Message{Arena: SingleSegment(nil)}
		seg, err := msg.Segment(0)
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, allocTest{
			name:    "empty alloc in empty segment",
			seg:     seg,
			size:    0,
			allocID: 0,
			addr:    0,
		})
	}
	{
		msg := &Message{Arena: SingleSegment(nil)}
		seg, err := msg.Segment(0)
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, allocTest{
			name:    "alloc in empty segment",
			seg:     seg,
			size:    8,
			allocID: 0,
			addr:    0,
		})
	}
	{
		msg := &Message{Arena: MultiSegment([][]byte{
			incrementingData(24)[:8],
			incrementingData(24)[:8],
			incrementingData(24)[:8],
		})}
		seg, err := msg.Segment(1)
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, allocTest{
			name:    "prefers given segment",
			seg:     seg,
			size:    16,
			allocID: 1,
			addr:    8,
		})
	}
	{
		msg := &Message{Arena: MultiSegment([][]byte{
			incrementingData(24)[:8],
			incrementingData(24),
		})}
		seg, err := msg.Segment(1)
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, allocTest{
			name:    "given segment full with another available",
			seg:     seg,
			size:    16,
			allocID: 0,
			addr:    8,
		})
	}
	{
		msg := &Message{Arena: MultiSegment([][]byte{
			incrementingData(24),
			incrementingData(24),
		})}
		seg, err := msg.Segment(1)
		if err != nil {
			t.Fatal(err)
		}
		tests = append(tests, allocTest{
			name:    "given segment full and no others available",
			seg:     seg,
			size:    16,
			allocID: 2,
			addr:    0,
		})
	}

	for i, test := range tests {
		seg, addr, err := alloc(test.seg, test.size)
		if err != nil {
			t.Errorf("tests[%d] - %s: alloc(..., %d) error: %v", i, test.name, test.size, err)
			continue
		}
		if seg.ID() != test.allocID {
			t.Errorf("tests[%d] - %s: alloc(..., %d) returned segment %d; want segment %d", i, test.name, test.size, seg.ID(), test.allocID)
		}
		if addr != test.addr {
			t.Errorf("tests[%d] - %s: alloc(..., %d) returned address %v; want address %v", i, test.name, test.size, addr, test.addr)
		}
		if !seg.regionInBounds(addr, test.size) {
			t.Errorf("tests[%d] - %s: alloc(..., %d) returned address %v, which is not in bounds (len(seg.data) == %d)", i, test.name, test.size, addr, len(seg.Data()))
		} else if data := seg.slice(addr, test.size); !isZeroFilled(data) {
			t.Errorf("tests[%d] - %s: alloc(..., %d) region has data % 02x; want zero-filled", i, test.name, test.size, data)
		}
	}
}

func TestSingleSegment(t *testing.T) {
	// fresh arena
	{
		arena := SingleSegment(nil)
		if n := arena.NumSegments(); n != 1 {
			t.Errorf("SingleSegment(nil).NumSegments() = %d; want 1", n)
		}
		data, err := arena.Data(0)
		if len(data) != 0 {
			t.Errorf("SingleSegment(nil).Data(0) = %#v; want nil", data)
		}
		if err != nil {
			t.Errorf("SingleSegment(nil).Data(0) error: %v", err)
		}
		_, err = arena.Data(1)
		if err == nil {
			t.Error("SingleSegment(nil).Data(1) succeeded; want error")
		}
	}

	// existing data
	{
		arena := SingleSegment(incrementingData(8))
		if n := arena.NumSegments(); n != 1 {
			t.Errorf("SingleSegment(incrementingData(8)).NumSegments() = %d; want 1", n)
		}
		data, err := arena.Data(0)
		if want := incrementingData(8); !bytes.Equal(data, want) {
			t.Errorf("SingleSegment(incrementingData(8)).Data(0) = %#v; want %#v", data, want)
		}
		if err != nil {
			t.Errorf("SingleSegment(incrementingData(8)).Data(0) error: %v", err)
		}
		_, err = arena.Data(1)
		if err == nil {
			t.Error("SingleSegment(incrementingData(8)).Data(1) succeeded; want error")
		}
	}
}

func TestSingleSegmentAllocate(t *testing.T) {
	tests := []arenaAllocTest{
		{
			name: "empty arena",
			init: func() (Arena, map[SegmentID]*Segment) {
				return SingleSegment(nil), nil
			},
			size: 8,
			id:   0,
			data: []byte{},
		},
		{
			name: "unloaded",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				return SingleSegment(buf[:16]), nil
			},
			size: 8,
			id:   0,
			data: incrementingData(16),
		},
		{
			name: "loaded",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				buf = buf[:16]
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf},
				}
				return SingleSegment(buf), segs
			},
			size: 8,
			id:   0,
			data: incrementingData(16),
		},
		{
			name: "loaded changes length",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(32)
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf[:24]},
				}
				return SingleSegment(buf[:16]), segs
			},
			size: 8,
			id:   0,
			data: incrementingData(24),
		},
		{
			name: "message-filled segment",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf},
				}
				return SingleSegment(buf[:16]), segs
			},
			size: 8,
			id:   0,
			data: incrementingData(24),
		},
	}
	for i := range tests {
		tests[i].run(t, i)
	}
}

func TestMultiSegment(t *testing.T) {
	// fresh arena
	{
		arena := MultiSegment(nil)
		if n := arena.NumSegments(); n != 0 {
			t.Errorf("MultiSegment(nil).NumSegments() = %d; want 1", n)
		}
		_, err := arena.Data(0)
		if err == nil {
			t.Error("MultiSegment(nil).Data(0) succeeded; want error")
		}
	}

	// existing data
	{
		arena := MultiSegment([][]byte{incrementingData(8), incrementingData(24)})
		if n := arena.NumSegments(); n != 2 {
			t.Errorf("MultiSegment(...).NumSegments() = %d; want 2", n)
		}
		data, err := arena.Data(0)
		if want := incrementingData(8); !bytes.Equal(data, want) {
			t.Errorf("MultiSegment(...).Data(0) = %#v; want %#v", data, want)
		}
		if err != nil {
			t.Errorf("MultiSegment(...).Data(0) error: %v", err)
		}
		data, err = arena.Data(1)
		if want := incrementingData(24); !bytes.Equal(data, want) {
			t.Errorf("MultiSegment(...).Data(1) = %#v; want %#v", data, want)
		}
		if err != nil {
			t.Errorf("MultiSegment(...).Data(1) error: %v", err)
		}
		_, err = arena.Data(2)
		if err == nil {
			t.Error("MultiSegment(...).Data(2) succeeded; want error")
		}
	}
}

func TestMultiSegmentAllocate(t *testing.T) {
	tests := []arenaAllocTest{
		{
			name: "empty arena",
			init: func() (Arena, map[SegmentID]*Segment) {
				return MultiSegment(nil), nil
			},
			size: 8,
			id:   0,
			data: []byte{},
		},
		{
			name: "space in unloaded segment",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				return MultiSegment([][]byte{buf[:16]}), nil
			},
			size: 8,
			id:   0,
			data: incrementingData(16),
		},
		{
			name: "space in loaded segment",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				buf = buf[:16]
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf},
				}
				return MultiSegment([][]byte{buf}), segs
			},
			size: 8,
			id:   0,
			data: incrementingData(16),
		},
		{
			name: "space in loaded segment changes length",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(32)
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf[:24]},
				}
				return MultiSegment([][]byte{buf[:16]}), segs
			},
			size: 8,
			id:   0,
			data: incrementingData(24),
		},
		{
			name: "message-filled segment",
			init: func() (Arena, map[SegmentID]*Segment) {
				buf := incrementingData(24)
				segs := map[SegmentID]*Segment{
					0: {id: 0, data: buf},
				}
				return MultiSegment([][]byte{buf[:16]}), segs
			},
			size: 8,
			id:   1,
			data: []byte{},
		},
	}

	for i := range tests {
		tests[i].run(t, i)
	}
}

type serializeTest struct {
	name        string
	segs        [][]byte
	out         []byte
	encodeFails bool
	decodeFails bool
	decodeError error
}

func (st *serializeTest) arena() Arena {
	bb := make([][]byte, len(st.segs))
	for i := range bb {
		bb[i] = make([]byte, len(st.segs[i]))
		copy(bb[i], st.segs[i])
	}
	return MultiSegment(bb)
}

func (st *serializeTest) copyOut() []byte {
	out := make([]byte, len(st.out))
	copy(out, st.out)
	return out
}

var serializeTests = []serializeTest{
	{
		name:        "empty message",
		segs:        [][]byte{},
		encodeFails: true,
	},
	{
		name:        "empty stream",
		out:         []byte{},
		decodeFails: true,
		decodeError: io.EOF,
	},
	{
		name:        "incomplete segment count",
		out:         []byte{0x01},
		decodeFails: true,
	},
	{
		name: "incomplete segment size",
		out: []byte{
			0x00, 0x00, 0x00, 0x00,
			0x00,
		},
		decodeFails: true,
	},
	{
		name: "empty single segment",
		segs: [][]byte{
			{},
		},
		out: []byte{
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
	},
	{
		name: "missing segment data",
		out: []byte{
			0x00, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
		},
		decodeFails: true,
	},
	{
		name: "missing segment size",
		out: []byte{
			0x01, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		decodeFails: true,
	},
	{
		name: "missing segment size padding",
		out: []byte{
			0x01, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		decodeFails: true,
	},
	{
		name: "single segment",
		segs: [][]byte{
			incrementingData(8),
		},
		out: []byte{
			0x00, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		},
	},
	{
		name: "two segments",
		segs: [][]byte{
			incrementingData(8),
			incrementingData(8),
		},
		out: []byte{
			0x01, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		},
	},
	{
		name: "two segments, missing size padding",
		out: []byte{
			0x01, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		},
		decodeFails: true,
	},
	{
		name:        "HTTP traffic should not panic on GOARCH=386",
		out:         []byte("GET / HTTP/1.1\r\n\r\n"),
		decodeFails: true,
	},
	{
		name:        "max segment should not panic",
		out:         bytes.Repeat([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 16),
		decodeFails: true,
	},
}

func TestMarshal(t *testing.T) {
	for i, test := range serializeTests {
		if test.decodeFails {
			continue
		}
		msg := &Message{Arena: test.arena()}
		out, err := msg.Marshal()
		if err != nil {
			if !test.encodeFails {
				t.Errorf("serializeTests[%d] %s: Marshal error: %v", i, test.name, err)
			}
			continue
		}
		if test.encodeFails {
			t.Errorf("serializeTests[%d] - %s: Marshal success; want error", i, test.name)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("serializeTests[%d] - %s: Marshal = % 02x; want % 02x", i, test.name, out, test.out)
		}
	}
}

func TestUnmarshal(t *testing.T) {
	for i, test := range serializeTests {
		if test.encodeFails {
			continue
		}
		msg, err := Unmarshal(test.copyOut())
		if err != nil {
			if !test.decodeFails {
				t.Errorf("serializeTests[%d] - %s: Unmarshal error: %v", i, test.name, err)
			}
			if test.decodeError != nil && err != test.decodeError {
				t.Errorf("serializeTests[%d] - %s: Unmarshal error: %v; want %v", i, test.name, err, test.decodeError)
			}
			continue
		}
		if test.decodeFails {
			t.Errorf("serializeTests[%d] - %s: Unmarshal success; want error", i, test.name)
			continue
		}
		if msg.NumSegments() != int64(len(test.segs)) {
			t.Errorf("serializeTests[%d] - %s: Unmarshal NumSegments() = %d; want %d", i, test.name, msg.NumSegments(), len(test.segs))
			continue
		}
		for j := range test.segs {
			seg, err := msg.Segment(SegmentID(j))
			if err != nil {
				t.Errorf("serializeTests[%d] - %s: Unmarshal Segment(%d) error: %v", i, test.name, j, err)
				continue
			}
			if !bytes.Equal(seg.Data(), test.segs[j]) {
				t.Errorf("serializeTests[%d] - %s: Unmarshal Segment(%d) = % 02x; want % 02x", i, test.name, j, seg.Data(), test.segs[j])
			}
		}
	}
}

func TestEncoder(t *testing.T) {
	for i, test := range serializeTests {
		if test.decodeFails {
			continue
		}
		msg := &Message{Arena: test.arena()}
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		err := enc.Encode(msg)
		out := buf.Bytes()
		if err != nil {
			if !test.encodeFails {
				t.Errorf("serializeTests[%d] - %s: Encode error: %v", i, test.name, err)
			}
			continue
		}
		if test.encodeFails {
			t.Errorf("serializeTests[%d] - %s: Encode success; want error", i, test.name)
			continue
		}
		if !bytes.Equal(out, test.out) {
			t.Errorf("serializeTests[%d] - %s: Encode = % 02x; want % 02x", i, test.name, out, test.out)
		}
	}
}

func TestDecoder(t *testing.T) {
	for i, test := range serializeTests {
		if test.encodeFails {
			continue
		}
		msg, err := NewDecoder(bytes.NewReader(test.out)).Decode()
		if err != nil {
			if !test.decodeFails {
				t.Errorf("serializeTests[%d] - %s: Decode error: %v", i, test.name, err)
			}
			if test.decodeError != nil && err != test.decodeError {
				t.Errorf("serializeTests[%d] - %s: Decode error: %v; want %v", i, test.name, err, test.decodeError)
			}
			continue
		}
		if test.decodeFails {
			t.Errorf("serializeTests[%d] - %s: Decode success; want error", i, test.name)
			continue
		}
		if msg.NumSegments() != int64(len(test.segs)) {
			t.Errorf("serializeTests[%d] - %s: Decode NumSegments() = %d; want %d", i, test.name, msg.NumSegments(), len(test.segs))
			continue
		}
		for j := range test.segs {
			seg, err := msg.Segment(SegmentID(j))
			if err != nil {
				t.Errorf("serializeTests[%d] - %s: Decode Segment(%d) error: %v", i, test.name, j, err)
				continue
			}
			if !bytes.Equal(seg.Data(), test.segs[j]) {
				t.Errorf("serializeTests[%d] - %s: Decode Segment(%d) = % 02x; want % 02x", i, test.name, j, seg.Data(), test.segs[j])
			}
		}
	}
}

func TestDecoder_MaxMessageSize(t *testing.T) {
	t.Parallel()
	zeroWord := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	tests := []struct {
		name    string
		maxSize uint64
		r       io.Reader
		ok      bool
	}{
		{
			name:    "header too big",
			maxSize: 15,
			r: bytes.NewReader([]byte{
				0x02, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			}),
		},
		{
			name:    "header at limit",
			maxSize: 16,
			r: bytes.NewReader([]byte{
				0x02, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
			}),
			ok: true,
		},
		{
			name:    "body too large",
			maxSize: 64,
			r: io.MultiReader(
				bytes.NewReader([]byte{
					0x00, 0x00, 0x00, 0x00,
					0x09, 0x00, 0x00, 0x00,
				}),
				bytes.NewReader(bytes.Repeat(zeroWord, 9)),
			),
		},
		{
			name:    "body plus header too large",
			maxSize: 64,
			r: io.MultiReader(
				bytes.NewReader([]byte{
					0x00, 0x00, 0x00, 0x00,
					0x08, 0x00, 0x00, 0x00,
				}),
				bytes.NewReader(bytes.Repeat(zeroWord, 8)),
			),
		},
		{
			name:    "body plus header at limit",
			maxSize: 72,
			r: io.MultiReader(
				bytes.NewReader([]byte{
					0x00, 0x00, 0x00, 0x00,
					0x08, 0x00, 0x00, 0x00,
				}),
				bytes.NewReader(bytes.Repeat(zeroWord, 8)),
			),
			ok: true,
		},
	}
	for _, test := range tests {
		d := NewDecoder(test.r)
		d.MaxMessageSize = test.maxSize
		_, err := d.Decode()
		switch {
		case err != nil && test.ok:
			t.Errorf("%s test: Decode error: %v", test.name, err)
		case err == nil && !test.ok:
			t.Errorf("%s test: Decode success; want error", test.name)
		}
	}
}

// TestStreamHeaderPadding is a regression test for
// stream header padding.
//
// Encoder reuses a buffer for stream header marshalling,
// this test ensures that the padding is explicitly
// zeroed. This was not done in previous versions and
// resulted in the padding being garbage.
func TestStreamHeaderPadding(t *testing.T) {
	msg := &Message{
		Arena: MultiSegment([][]byte{
			incrementingData(8),
			incrementingData(8),
			incrementingData(8),
		}),
	}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.Encode(msg)
	buf.Reset()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	msg = &Message{
		Arena: MultiSegment([][]byte{
			incrementingData(8),
			incrementingData(8),
		}),
	}
	err = enc.Encode(msg)
	out := buf.Bytes()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	want := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
	}
	if !bytes.Equal(out, want) {
		t.Errorf("Encode = % 02x; want % 02x", out, want)
	}
}

func TestFirstSegmentMessage_SingleSegment(t *testing.T) {
	msg, seg, err := NewMessage(SingleSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	if msg.NumSegments() != 1 {
		t.Errorf("msg.NumSegments() = %d; want 1", msg.NumSegments())
	}
	if seg.Message() != msg {
		t.Errorf("seg.Message() = %p; want %p", seg.Message(), msg)
	}
	if seg.ID() != 0 {
		t.Errorf("seg.ID() = %d; want 0", seg.ID())
	}
	if seg0, err := msg.Segment(0); err != nil {
		t.Errorf("msg.Segment(0): %v", err)
	} else if seg0 != seg {
		t.Errorf("msg.Segment(0) = %p; want %p", seg0, seg)
	}
}

func TestFirstSegmentMessage_MultiSegment(t *testing.T) {
	msg, seg, err := NewMessage(MultiSegment(nil))
	if err != nil {
		t.Fatal(err)
	}
	if msg.NumSegments() != 1 {
		t.Errorf("msg.NumSegments() = %d; want 1", msg.NumSegments())
	}
	if seg.Message() != msg {
		t.Errorf("seg.Message() = %p; want %p", seg.Message(), msg)
	}
	if seg.ID() != 0 {
		t.Errorf("seg.ID() = %d; want 0", seg.ID())
	}
	if seg0, err := msg.Segment(0); err != nil {
		t.Errorf("msg.Segment(0): %v", err)
	} else if seg0 != seg {
		t.Errorf("msg.Segment(0) = %p; want %p", seg0, seg)
	}
}

func TestNextAlloc(t *testing.T) {
	const max32 = 1<<31 - 8
	const max64 = 1<<63 - 8
	tests := []struct {
		name string
		curr int64
		max  int64
		req  Size
		ok   bool
	}{
		{name: "zero", curr: 0, max: max64, req: 0, ok: true},
		{name: "first word", curr: 0, max: max64, req: 8, ok: true},
		{name: "first word, unaligned curr", curr: 13, max: max64, req: 8, ok: true},
		{name: "second word", curr: 8, max: max64, req: 8, ok: true},
		{name: "one byte pads to word", curr: 8, max: max64, req: 1, ok: true},
		{name: "max size", curr: 0, max: max64, req: 0xfffffff8, ok: true},
		{name: "max size + 1", curr: 0, max: max64, req: 0xfffffff9, ok: false},
		{name: "max req", curr: 0, max: max64, req: 0xffffffff, ok: false},
		{name: "max curr, request 0", curr: max64, max: max64, req: 0, ok: true},
		{name: "max curr, request 1", curr: max64, max: max64, req: 1, ok: false},
		{name: "medium curr, request 2 words", curr: 4 << 20, max: max64, req: 16, ok: true},
		{name: "large curr, request word", curr: 1 << 34, max: max64, req: 8, ok: true},
		{name: "large unaligned curr, request word", curr: 1<<34 + 13, max: max64, req: 8, ok: true},
		{name: "2<<31-8 curr, request 0", curr: 2<<31 - 8, max: max64, req: 0, ok: true},
		{name: "2<<31-8 curr, request 1", curr: 2<<31 - 8, max: max64, req: 1, ok: true},
		{name: "2<<31-8 curr, 32-bit max, request 0", curr: 2<<31 - 8, max: max32, req: 0, ok: true},
		{name: "2<<31-8 curr, 32-bit max, request 1", curr: 2<<31 - 8, max: max32, req: 1, ok: false},
	}
	for _, test := range tests {
		if test.max%8 != 0 {
			t.Errorf("%s: max must be word-aligned. Skipped.", test.name)
			continue
		}
		got, err := nextAlloc(test.curr, test.max, test.req)
		if err != nil {
			if test.ok {
				t.Errorf("%s: nextAlloc(%d, %d, %d) = _, %v; want >=%d, <nil>", test.name, test.curr, test.max, test.req, err, test.req)
			}
			continue
		}
		if !test.ok {
			t.Errorf("%s: nextAlloc(%d, %d, %d) = %d, <nil>; want _, <error>", test.name, test.curr, test.max, test.req, got)
			continue
		}
		max := test.max - test.curr
		if max < 0 {
			max = 0
		}
		if int64(got) < int64(test.req) || int64(got) > max {
			t.Errorf("%s: nextAlloc(%d, %d, %d) = %d, <nil>; want in range [%d, %d]", test.name, test.curr, test.max, test.req, got, test.req, max)
		}
		if got%8 != 0 {
			t.Errorf("%s: nextAlloc(%d, %d, %d) = %d, <nil>; want divisible by 8 (word size)", test.name, test.curr, test.max, test.req, got)
		}
	}
}

type arenaAllocTest struct {
	name string

	// Arrange
	init func() (Arena, map[SegmentID]*Segment)
	size Size

	// Assert
	id   SegmentID
	data []byte
}

func (test *arenaAllocTest) run(t *testing.T, i int) {
	arena, segs := test.init()
	id, data, err := arena.Allocate(test.size, segs)

	if err != nil {
		t.Errorf("tests[%d] - %s: Allocate error: %v", i, test.name, err)
		return
	}
	if id != test.id {
		t.Errorf("tests[%d] - %s: Allocate id = %d; want %d", i, test.name, id, test.id)
	}
	if !bytes.Equal(data, test.data) {
		t.Errorf("tests[%d] - %s: Allocate data = % 02x; want % 02x", i, test.name, data, test.data)
	}
	if Size(cap(data)-len(data)) < test.size {
		t.Errorf("tests[%d] - %s: Allocate len(data) = %d, cap(data) = %d; cap(data) should be at least %d", i, test.name, len(data), cap(data), Size(len(data))+test.size)
	}
}

func incrementingData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

func isZeroFilled(b []byte) bool {
	for _, bb := range b {
		if bb != 0 {
			return false
		}
	}
	return true
}

type readOnlyArena struct {
	Arena
}

func (ro readOnlyArena) String() string {
	return fmt.Sprintf("readOnlyArena{%v}", ro.Arena)
}

func (readOnlyArena) Allocate(sz Size, segs map[SegmentID]*Segment) (SegmentID, []byte, error) {
	return 0, nil, errReadOnlyArena
}

var errReadOnlyArena = errors.New("Allocate called on read-only arena")
