package capnp

import (
	"errors"
	"fmt"
)

// Canonicalize encodes a struct into its canonical form: a single-
// segment blob without a segment table.  The result will be identical
// for equivalent structs, even as the schema evolves.  The blob is
// suitable for hashing or signing.
func Canonicalize(s Struct) ([]byte, error) {
	msg, seg, _ := NewMessage(SingleSegment(nil))
	if !s.IsValid() {
		return seg.Data(), nil
	}
	root, err := NewRootStruct(seg, canonicalStructSize(s))
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %v", err)
	}
	if err := msg.SetRootPtr(root.ToPtr()); err != nil {
		return nil, fmt.Errorf("canonicalize: %v", err)
	}
	if err := fillCanonicalStruct(root, s); err != nil {
		return nil, fmt.Errorf("canonicalize: %v", err)
	}
	return seg.Data(), nil
}

func canonicalPtr(dst *Segment, p Ptr) (Ptr, error) {
	if !p.IsValid() {
		return Ptr{}, nil
	}
	switch p.flags.ptrType() {
	case structPtrType:
		ss, err := NewStruct(dst, canonicalStructSize(p.Struct()))
		if err != nil {
			return Ptr{}, err
		}
		if err := fillCanonicalStruct(ss, p.Struct()); err != nil {
			return Ptr{}, err
		}
		return ss.ToPtr(), nil
	case listPtrType:
		ll, err := canonicalList(dst, p.List())
		if err != nil {
			return Ptr{}, err
		}
		return ll.ToPtr(), nil
	case interfacePtrType:
		return Ptr{}, errors.New("cannot canonicalize interface")
	default:
		panic("unreachable")
	}
}

func fillCanonicalStruct(dst, s Struct) error {
	copy(dst.seg.slice(dst.off, dst.size.DataSize), s.seg.slice(s.off, s.size.DataSize))
	for i := uint16(0); i < dst.size.PointerCount; i++ {
		p, err := s.Ptr(i)
		if err != nil {
			return fmt.Errorf("pointer %d: %v", i, err)
		}
		cp, err := canonicalPtr(dst.seg, p)
		if err != nil {
			return fmt.Errorf("pointer %d: %v", i, err)
		}
		if err := dst.SetPtr(i, cp); err != nil {
			return fmt.Errorf("pointer %d: %v", i, err)
		}
	}
	return nil
}

func canonicalStructSize(s Struct) ObjectSize {
	if !s.IsValid() {
		return ObjectSize{}
	}
	var sz ObjectSize
	// int32 will not overflow because max struct data size is 2^16 words.
	for off := int32(s.size.DataSize &^ (wordSize - 1)); off >= 0; off -= int32(wordSize) {
		if s.Uint64(DataOffset(off)) != 0 {
			sz.DataSize = Size(off) + wordSize
			break
		}
	}
	for i := int32(s.size.PointerCount) - 1; i >= 0; i-- {
		if s.seg.readRawPointer(s.pointerAddress(uint16(i))) != 0 {
			sz.PointerCount = uint16(i + 1)
			break
		}
	}
	return sz
}

func canonicalList(dst *Segment, l List) (List, error) {
	if !l.IsValid() {
		return List{}, nil
	}
	if l.size.PointerCount == 0 {
		// Data only, just copy over.
		sz := l.allocSize()
		_, newAddr, err := alloc(dst, sz)
		if err != nil {
			return List{}, err
		}
		cl := List{
			seg:        dst,
			off:        newAddr,
			length:     l.length,
			size:       l.size,
			flags:      l.flags,
			depthLimit: maxDepth,
		}
		end, _ := l.off.addSize(sz) // list was already validated
		copy(dst.data[newAddr:], l.seg.data[l.off:end])
		return cl, nil
	}
	if l.flags&isCompositeList == 0 {
		cl, err := NewPointerList(dst, l.length)
		if err != nil {
			return List{}, err
		}
		for i := 0; i < l.Len(); i++ {
			p, err := PointerList{l}.PtrAt(i)
			if err != nil {
				return List{}, fmt.Errorf("element %d: %v", i, err)
			}
			cp, err := canonicalPtr(dst, p)
			if err != nil {
				return List{}, fmt.Errorf("element %d: %v", i, err)
			}
			if err := cl.SetPtr(i, cp); err != nil {
				return List{}, fmt.Errorf("element %d: %v", i, err)
			}
		}
		return cl.List, nil
	}

	// Struct/composite list
	var elemSize ObjectSize
	for i := 0; i < l.Len(); i++ {
		sz := canonicalStructSize(l.Struct(i))
		if sz.DataSize > elemSize.DataSize {
			elemSize.DataSize = sz.DataSize
		}
		if sz.PointerCount > elemSize.PointerCount {
			elemSize.PointerCount = sz.PointerCount
		}
	}
	cl, err := NewCompositeList(dst, elemSize, l.length)
	if err != nil {
		return List{}, err
	}
	for i := 0; i < cl.Len(); i++ {
		if err := fillCanonicalStruct(cl.Struct(i), l.Struct(i)); err != nil {
			return List{}, fmt.Errorf("element %d: %v", i, err)
		}
	}
	return cl, nil
}
