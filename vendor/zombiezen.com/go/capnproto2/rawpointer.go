package capnp

// pointerOffset is an address offset in multiples of word size.
type pointerOffset int32

// resolve returns an absolute address relative to a base address.
// For near pointers, the base is the end of the near pointer.
// For far pointers, the base is zero (the beginning of the segment).
func (off pointerOffset) resolve(base Address) (_ Address, ok bool) {
	if off == 0 {
		return base, true
	}
	addr := base + Address(off*pointerOffset(wordSize))
	return addr, (addr > base || off < 0) && (addr < base || off > 0)
}

// nearPointerOffset computes the offset for a pointer at paddr to point to addr.
func nearPointerOffset(paddr, addr Address) pointerOffset {
	return pointerOffset(addr/Address(wordSize) - paddr/Address(wordSize) - 1)
}

// rawPointer is an encoded pointer.
type rawPointer uint64

// rawStructPointer returns a struct pointer.  The offset is from the
// end of the pointer to the start of the struct.
func rawStructPointer(off pointerOffset, sz ObjectSize) rawPointer {
	return rawPointer(structPointer) | rawPointer(uint32(off)<<2) | rawPointer(sz.dataWordCount())<<32 | rawPointer(sz.PointerCount)<<48
}

// rawListPointer returns a list pointer.  The offset is the number of
// words relative to the end of the pointer that the list starts.  If
// listType is compositeList, then length is the number of words
// that the list occupies, otherwise it is the number of elements in
// the list.
func rawListPointer(off pointerOffset, listType listType, length int32) rawPointer {
	return rawPointer(listPointer) | rawPointer(uint32(off)<<2) | rawPointer(listType)<<32 | rawPointer(length)<<35
}

// rawInterfacePointer returns an interface pointer that references
// a capability number.
func rawInterfacePointer(capability CapabilityID) rawPointer {
	return rawPointer(otherPointer) | rawPointer(capability)<<32
}

// rawFarPointer returns a pointer to a pointer in another segment.
func rawFarPointer(segID SegmentID, off Address) rawPointer {
	return rawPointer(farPointer) | rawPointer(off&^7) | (rawPointer(segID) << 32)
}

// rawDoubleFarPointer returns a pointer to a pointer in another segment.
func rawDoubleFarPointer(segID SegmentID, off Address) rawPointer {
	return rawPointer(doubleFarPointer) | rawPointer(off&^7) | (rawPointer(segID) << 32)
}

// landingPadNearPointer converts a double-far pointer landing pad into
// a near pointer in the destination segment.  Its offset will be
// relative to the beginning of the segment.  tag must be either a
// struct or a list pointer.
func landingPadNearPointer(far, tag rawPointer) rawPointer {
	// Replace tag's offset with far's offset.
	// far's offset (29-bit unsigned) just needs to be shifted down to
	// make it into a signed 30-bit value.
	return tag&^0xfffffffc | rawPointer(uint32(far&^3)>>1)
}

type pointerType int

// Raw pointer types.
const (
	structPointer    pointerType = 0
	listPointer      pointerType = 1
	farPointer       pointerType = 2
	doubleFarPointer pointerType = 6
	otherPointer     pointerType = 3
)

func (p rawPointer) pointerType() pointerType {
	t := pointerType(p & 3)
	if t == farPointer {
		return pointerType(p & 7)
	}
	return t
}

func (p rawPointer) structSize() ObjectSize {
	c := uint16(p >> 32)
	d := uint16(p >> 48)
	return ObjectSize{
		DataSize:     Size(c) * wordSize,
		PointerCount: d,
	}
}

type listType int

// Raw list pointer types.
const (
	voidList      listType = 0
	bit1List      listType = 1
	byte1List     listType = 2
	byte2List     listType = 3
	byte4List     listType = 4
	byte8List     listType = 5
	pointerList   listType = 6
	compositeList listType = 7
)

func (p rawPointer) listType() listType {
	return listType((p >> 32) & 7)
}

func (p rawPointer) numListElements() int32 {
	return int32(p >> 35)
}

// elementSize returns the size of an individual element in the list referenced by p.
func (p rawPointer) elementSize() ObjectSize {
	switch p.listType() {
	case voidList:
		return ObjectSize{}
	case bit1List:
		// Size is ignored on bit lists.
		return ObjectSize{}
	case byte1List:
		return ObjectSize{DataSize: 1}
	case byte2List:
		return ObjectSize{DataSize: 2}
	case byte4List:
		return ObjectSize{DataSize: 4}
	case byte8List:
		return ObjectSize{DataSize: 8}
	case pointerList:
		return ObjectSize{PointerCount: 1}
	default:
		panic("elementSize not supposed to be called on composite or unknown list type")
	}
}

// totalListSize returns the total size of the list referenced by p.
func (p rawPointer) totalListSize() (sz Size, ok bool) {
	n := p.numListElements()
	switch p.listType() {
	case voidList:
		return 0, true
	case bit1List:
		return Size((n + 7) / 8), true
	case compositeList:
		// For a composite list, n represents the number of words (excluding the tag word).
		return wordSize.times(n + 1)
	default:
		return p.elementSize().totalSize().times(n)
	}
}

// offset returns a pointer's offset.  Only valid for struct or list
// pointers.
func (p rawPointer) offset() pointerOffset {
	return pointerOffset(int32(p) >> 2)
}

// withOffset replaces a pointer's offset.  Only valid for struct or
// list pointers.
func (p rawPointer) withOffset(off pointerOffset) rawPointer {
	return p&^0xfffffffc | rawPointer(uint32(off<<2))
}

// farAddress returns the address of the landing pad pointer.
func (p rawPointer) farAddress() Address {
	// Far pointer offset is 29 bits, starting after the low 3 bits.
	// It's an unsigned word offset, which would be equivalent to a
	// logical left shift by 3.
	return Address(p) &^ 7
}

// farSegment returns the segment ID that the far pointer references.
func (p rawPointer) farSegment() SegmentID {
	return SegmentID(p >> 32)
}

// otherPointerType returns the type of "other pointer" from p.
func (p rawPointer) otherPointerType() uint32 {
	return uint32(p) >> 2
}

// capabilityIndex returns the index of the capability in the message's capability table.
func (p rawPointer) capabilityIndex() CapabilityID {
	return CapabilityID(p >> 32)
}
