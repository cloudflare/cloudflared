// Package keccakf1600 provides a two and four-way Keccak-f[1600] permutation in parallel.
//
// Keccak-f[1600] is the permutation underlying several algorithms such as
// Keccak, SHA3 and SHAKE. Running two or four permutations in parallel is
// useful in some scenarios like in hash-based signatures.
//
// # Limitations
//
// Note that not all the architectures support SIMD instructions. This package
// uses AVX2 instructions that are available in some AMD64 architectures
// and  NEON instructions that are available in some ARM64 architectures.
//
// For those systems not supporting these, the package still provides the
// expected functionality by means of a generic and slow implementation.
// The recommendation is to beforehand verify IsEnabledX4() and IsEnabledX2()
// to determine if the current system supports the SIMD implementation.
package keccakf1600

import (
	"unsafe"

	"github.com/cloudflare/circl/internal/sha3"
	"golang.org/x/sys/cpu"
)

// StateX4 contains state for the four-way permutation including the four
// interleaved [25]uint64 buffers. Call Initialize() before use to initialize
// and get a pointer to the interleaved buffer.
type StateX4 struct {
	// Go guarantees a to be aligned on 8 bytes, whereas we need it to be
	// aligned on 32 bytes for bet performance.  Thus we leave some headroom
	// to be able to move the start of the state.

	// 4 x 25 uint64s for the interleaved states and three uint64s headroom
	// to fix alignment.
	a [103]uint64

	// Offset into a that is 32 byte aligned.
	offset int
}

// StateX2 contains state for the two-way permutation including the two
// interleaved [25]uint64 buffers. Call Initialize() before use to initialize
// and get a pointer to the interleaved buffer.
type StateX2 struct {
	// Go guarantees a to be aligned on 8 bytes, whereas we need it to be
	// aligned on 32 bytes for bet performance.  Thus we leave some headroom
	// to be able to move the start of the state.

	// 2 x 25 uint64s for the interleaved states and three uint64s headroom
	// to fix alignment.
	a [53]uint64

	// Offset into a that is 32 byte aligned.
	offset int
}

// IsEnabledX4 returns true if the architecture supports a four-way SIMD
// implementation provided in this package.
func IsEnabledX4() bool { return cpu.X86.HasAVX2 }

// IsEnabledX2 returns true if the architecture supports a two-way SIMD
// implementation provided in this package.
func IsEnabledX2() bool {
	// After Go 1.16 the flag cpu.ARM64.HasSHA3 is no longer exposed.
	return false
}

// Initialize the state and returns the buffer on which the four permutations
// will act: a uint64 slice of length 100.  The first permutation will act
// on {a[0], a[4], ..., a[96]}, the second on {a[1], a[5], ..., a[97]}, etc.
func (s *StateX4) Initialize() []uint64 {
	rp := unsafe.Pointer(&s.a[0])

	// uint64s are always aligned by a multiple of 8.  Compute the remainder
	// of the address modulo 32 divided by 8.
	rem := (int(uintptr(rp)&31) >> 3)

	if rem != 0 {
		s.offset = 4 - rem
	}

	// The slice we return will be aligned on 32 byte boundary.
	return s.a[s.offset : s.offset+100]
}

// Initialize the state and returns the buffer on which the two permutations
// will act: a uint64 slice of length 50.  The first permutation will act
// on {a[0], a[2], ..., a[48]} and the second on {a[1], a[3], ..., a[49]}.
func (s *StateX2) Initialize() []uint64 {
	rp := unsafe.Pointer(&s.a[0])

	// uint64s are always aligned by a multiple of 8.  Compute the remainder
	// of the address modulo 32 divided by 8.
	rem := (int(uintptr(rp)&31) >> 3)

	if rem != 0 {
		s.offset = 4 - rem
	}

	// The slice we return will be aligned on 32 byte boundary.
	return s.a[s.offset : s.offset+50]
}

// Permute performs the four parallel Keccak-f[1600]s interleaved on the slice
// returned from Initialize().
func (s *StateX4) Permute() {
	if IsEnabledX4() {
		permuteSIMDx4(s.a[s.offset:])
	} else {
		permuteScalarX4(s.a[s.offset:]) // A slower generic implementation.
	}
}

// Permute performs the two parallel Keccak-f[1600]s interleaved on the slice
// returned from Initialize().
func (s *StateX2) Permute() {
	if IsEnabledX2() {
		permuteSIMDx2(s.a[s.offset:])
	} else {
		permuteScalarX2(s.a[s.offset:]) // A slower generic implementation.
	}
}

func permuteScalarX4(a []uint64) {
	var buf [25]uint64
	for i := 0; i < 4; i++ {
		for j := 0; j < 25; j++ {
			buf[j] = a[4*j+i]
		}
		sha3.KeccakF1600(&buf)
		for j := 0; j < 25; j++ {
			a[4*j+i] = buf[j]
		}
	}
}

func permuteScalarX2(a []uint64) {
	var buf [25]uint64
	for i := 0; i < 2; i++ {
		for j := 0; j < 25; j++ {
			buf[j] = a[2*j+i]
		}
		sha3.KeccakF1600(&buf)
		for j := 0; j < 25; j++ {
			a[2*j+i] = buf[j]
		}
	}
}
