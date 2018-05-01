// +build gofuzz

// Fuzz test harness.  To run:
// go-fuzz-build zombiezen.com/go/capnproto2/internal/fuzztest
// go-fuzz -bin=fuzztest-fuzz.zip -workdir=internal/fuzztest

package fuzztest

import (
	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
)

func Fuzz(data []byte) int {
	// TODO(someday): find a way to fuzz multiple segments
	if len(data)%8 != 0 {
		// Less interested in outcomes of misaligned segments.  Zero-pad.
		data = append([]byte(nil), data...)
		for len(data)%8 != 0 {
			data = append(data, 0)
		}
	}
	msg := &capnp.Message{Arena: capnp.SingleSegment(data)}
	z, err := air.ReadRootZ(msg)
	if err != nil {
		return 0
	}
	switch z.Which() {
	case air.Z_Which_void:
	case air.Z_Which_zz:
		if _, err := z.Zz(); err != nil || !z.HasZz() {
			return 0
		}
	case air.Z_Which_f64:
		z.F64()
	case air.Z_Which_f32:
		z.F32()
	case air.Z_Which_i64:
		z.I64()
	case air.Z_Which_i32:
		z.I32()
	case air.Z_Which_i16:
		z.I16()
	case air.Z_Which_i8:
		z.I8()
	case air.Z_Which_u64:
		z.U64()
	case air.Z_Which_u32:
		z.U32()
	case air.Z_Which_u16:
		z.U16()
	case air.Z_Which_u8:
		z.U8()
	case air.Z_Which_bool:
		z.Bool()
	case air.Z_Which_text:
		if t, err := z.Text(); err != nil || t == "" {
			return 0
		}
	case air.Z_Which_blob:
		if b, err := z.Blob(); err != nil || len(b) == 0 {
			return 0
		}
	case air.Z_Which_f64vec:
		v, err := z.F64vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_f32vec:
		v, err := z.F32vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_i64vec:
		v, err := z.I64vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_i32vec:
		v, err := z.I32vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_i16vec:
		v, err := z.I16vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_i8vec:
		v, err := z.I8vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_u64vec:
		v, err := z.U64vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_u32vec:
		v, err := z.U32vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_u16vec:
		v, err := z.U16vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_u8vec:
		v, err := z.U8vec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_boolvec:
		v, err := z.Boolvec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_datavec:
		v, err := z.Datavec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			if _, err := v.At(i); err != nil {
				return 0
			}
		}
	case air.Z_Which_textvec:
		v, err := z.Textvec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			if _, err := v.At(i); err != nil {
				return 0
			}
		}
	case air.Z_Which_zvec:
		v, err := z.Zvec()
		if err != nil || v.Len() == 0 {
			return 0
		}
		for i := 0; i < v.Len(); i++ {
			v.At(i)
		}
	case air.Z_Which_airport:
		z.Airport()
	default:
		return 0
	}
	return 1
}
