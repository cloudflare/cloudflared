package cbor

// AppendStrings encodes and adds an array of strings to the dst byte array.
func (e Encoder) AppendStrings(dst []byte, vals []string) []byte {
	major := majorTypeArray
	l := len(vals)
	if l <= additionalMax {
		lb := byte(l)
		dst = append(dst, byte(major|lb))
	} else {
		dst = appendCborTypePrefix(dst, major, uint64(l))
	}
	for _, v := range vals {
		dst = e.AppendString(dst, v)
	}
	return dst
}

// AppendString encodes and adds a string to the dst byte array.
func (Encoder) AppendString(dst []byte, s string) []byte {
	major := majorTypeUtf8String

	l := len(s)
	if l <= additionalMax {
		lb := byte(l)
		dst = append(dst, byte(major|lb))
	} else {
		dst = appendCborTypePrefix(dst, majorTypeUtf8String, uint64(l))
	}
	return append(dst, s...)
}

// AppendBytes encodes and adds an array of bytes to the dst byte array.
func (Encoder) AppendBytes(dst, s []byte) []byte {
	major := majorTypeByteString

	l := len(s)
	if l <= additionalMax {
		lb := byte(l)
		dst = append(dst, byte(major|lb))
	} else {
		dst = appendCborTypePrefix(dst, major, uint64(l))
	}
	return append(dst, s...)
}

// AppendEmbeddedJSON adds a tag and embeds input JSON as such.
func AppendEmbeddedJSON(dst, s []byte) []byte {
	major := majorTypeTags
	minor := additionalTypeEmbeddedJSON

	// Append the TAG to indicate this is Embedded JSON.
	dst = append(dst, byte(major|additionalTypeIntUint16))
	dst = append(dst, byte(minor>>8))
	dst = append(dst, byte(minor&0xff))

	// Append the JSON Object as Byte String.
	major = majorTypeByteString

	l := len(s)
	if l <= additionalMax {
		lb := byte(l)
		dst = append(dst, byte(major|lb))
	} else {
		dst = appendCborTypePrefix(dst, major, uint64(l))
	}
	return append(dst, s...)
}
