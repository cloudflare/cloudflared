package common

// Constant time select.
// if pick == 1 (out = in1)
// if pick == 0 (out = in2)
// else out is undefined
func Cpick(pick int, out, in1, in2 []byte) {
	var which = byte((int8(pick << 7)) >> 7)
	for i := range out {
		out[i] = (in1[i] & which) | (in2[i] & ^which)
	}
}

// Read 2*bytelen(p) bytes into the given ExtensionFieldElement.
//
// It is an error to call this function if the input byte slice is less than 2*bytelen(p) bytes long.
func BytesToFp2(fp2 *Fp2, input []byte, bytelen int) {
	if len(input) < 2*bytelen {
		panic("input byte slice too short")
	}

	for i := 0; i < bytelen; i++ {
		j := i / 8
		k := uint64(i % 8)
		fp2.A[j] |= uint64(input[i]) << (8 * k)
		fp2.B[j] |= uint64(input[i+bytelen]) << (8 * k)
	}
}

// Convert the input to wire format.
//
// The output byte slice must be at least 2*bytelen(p) bytes long.
func Fp2ToBytes(output []byte, fp2 *Fp2, bytelen int) {
	if len(output) < 2*bytelen {
		panic("output byte slice too short")
	}

	// convert to bytes in little endian form
	for i := 0; i < bytelen; i++ {
		// set i = j*8 + k
		tmp := i / 8
		k := uint64(i % 8)
		output[i] = byte(fp2.A[tmp] >> (8 * k))
		output[i+bytelen] = byte(fp2.B[tmp] >> (8 * k))
	}
}
