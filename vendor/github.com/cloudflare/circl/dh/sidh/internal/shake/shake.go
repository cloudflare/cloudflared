// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package shake

// This file defines the CShake struct, and provides
// functions for creating SHAKE and cSHAKE instances, as well as utility
// functions for hashing bytes to arbitrary-length output.
//
//
// SHAKE implementation is based on FIPS PUB 202 [1]
// cSHAKE implementations is based on NIST SP 800-185 [2]
//
// [1] https://nvlpubs.nist.gov/nistpubs/FIPS/NIST.FIPS.202.pdf
// [2] https://doi.org/10.6028/NIST.SP.800-185

import (
	"encoding/binary"
)

// cSHAKE specific context
type CShake struct {
	state // SHA-3 state context and Read/Write operations

	// initBlock is the cSHAKE specific initialization set of bytes. It is initialized
	// by newCShake function and stores concatenation of N followed by S, encoded
	// by the method specified in 3.3 of [1].
	// It is stored here in order for Reset() to be able to put context into
	// initial state.
	initBlock []byte
}

// Consts for configuring initial SHA-3 state
const (
	dsbyteShake  = 0x1f
	dsbyteCShake = 0x04
	rate128      = 168
	rate256      = 136
)

func bytepad(input []byte, w int) []byte {
	// leftEncode always returns max 9 bytes
	buf := make([]byte, 0, 9+len(input)+w)
	buf = append(buf, leftEncode(uint64(w))...)
	buf = append(buf, input...)
	padlen := w - (len(buf) % w)
	return append(buf, make([]byte, padlen)...)
}

func leftEncode(value uint64) []byte {
	var b [9]byte
	binary.BigEndian.PutUint64(b[1:], value)
	// Trim all but last leading zero bytes
	i := byte(1)
	for i < 8 && b[i] == 0 {
		i++
	}
	// Prepend number of encoded bytes
	b[i-1] = 9 - i
	return b[i-1:]
}

func newCShake(N, S []byte, rate int, dsbyte byte) *CShake {

	// leftEncode returns max 9 bytes
	initBlock := make([]byte, 0, 9*2+len(N)+len(S))
	initBlock = append(initBlock, leftEncode(uint64(len(N)*8))...)
	initBlock = append(initBlock, N...)
	initBlock = append(initBlock, leftEncode(uint64(len(S)*8))...)
	initBlock = append(initBlock, S...)

	c := CShake{
		state:     state{rate: rate, dsbyte: dsbyte},
		initBlock: bytepad(initBlock, rate),
	}
	c.Write(c.initBlock)
	return &c
}

// Reset resets the hash to initial state.
func (c *CShake) Reset() {
	c.state.Reset()
	c.Write(c.initBlock)
}

// Clone returns copy of a cSHAKE context within its current state.
func (c *CShake) Clone() CShake {
	var ret CShake
	c.clone(&ret.state)
	ret.initBlock = make([]byte, len(c.initBlock))
	copy(ret.initBlock, c.initBlock)
	return ret
}

// NewShake128 creates a new SHAKE128 variable-output-length CShake.
// Its generic security strength is 128 bits against all attacks if at
// least 32 bytes of its output are used.
func NewShake128() *CShake {
	return &CShake{state{rate: rate128, dsbyte: dsbyteShake}, nil}
}

// NewShake256 creates a new SHAKE256 variable-output-length CShake.
// Its generic security strength is 256 bits against all attacks if
// at least 64 bytes of its output are used.
func NewShake256() *CShake {
	return &CShake{state{rate: rate256, dsbyte: dsbyteShake}, nil}
}

// NewCShake128 creates a new instance of cSHAKE128 variable-output-length CShake,
// a customizable variant of SHAKE128.
// N is used to define functions based on cSHAKE, it can be empty when plain cSHAKE is
// desired. S is a customization byte string used for domain separation - two cSHAKE
// computations on same input with different S yield unrelated outputs.
// When N and S are both empty, this is equivalent to NewShake128.
func NewCShake128(N, S []byte) *CShake {
	if len(N) == 0 && len(S) == 0 {
		return NewShake128()
	}
	return newCShake(N, S, rate128, dsbyteCShake)
}

// NewCShake256 creates a new instance of cSHAKE256 variable-output-length CShake,
// a customizable variant of SHAKE256.
// N is used to define functions based on cSHAKE, it can be empty when plain cSHAKE is
// desired. S is a customization byte string used for domain separation - two cSHAKE
// computations on same input with different S yield unrelated outputs.
// When N and S are both empty, this is equivalent to NewShake256.
func NewCShake256(N, S []byte) *CShake {
	if len(N) == 0 && len(S) == 0 {
		return NewShake256()
	}
	return newCShake(N, S, rate256, dsbyteCShake)
}
