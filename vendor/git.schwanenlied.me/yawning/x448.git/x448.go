// The MIT License (MIT)
//
// Copyright (c) 2014-2015 Cryptography Research, Inc.
// Copyright (c) 2015 Yawning Angel.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// Package x448 provides an implementation of scalar multiplication on the
// elliptic curve known as curve448.
//
// See https://tools.ietf.org/html/draft-irtf-cfrg-curves-11
package x448 // import "git.schwanenlied.me/yawning/x448.git"

const (
	x448Bytes = 56
	edwardsD  = -39081
)

var basePoint = [56]byte{
	5, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

func ScalarMult(out, scalar, base *[56]byte) int {
	var x1, x2, z2, x3, z3, t1, t2 gf
	x1.deser(base)
	x2.cpy(&one)
	z2.cpy(&zero)
	x3.cpy(&x1)
	z3.cpy(&one)

	var swap limbUint

	for t := int(448 - 1); t >= 0; t-- {
		sb := scalar[t/8]

		// Scalar conditioning.
		if t/8 == 0 {
			sb &= 0xFC
		} else if t/8 == x448Bytes-1 {
			sb |= 0x80
		}

		kT := (limbUint)((sb >> ((uint)(t) % 8)) & 1)
		kT = -kT // Set to all 0s or all 1s

		swap ^= kT
		x2.condSwap(&x3, swap)
		z2.condSwap(&z3, swap)
		swap = kT

		t1.add(&x2, &z2) // A = x2 + z2
		t2.sub(&x2, &z2) // B = x2 - z2
		z2.sub(&x3, &z3) // D = x3 - z3
		x2.mul(&t1, &z2) // DA
		z2.add(&z3, &x3) // C = x3 + z3
		x3.mul(&t2, &z2) // CB
		z3.sub(&x2, &x3) // DA-CB
		z2.sqr(&z3)      // (DA-CB)^2
		z3.mul(&x1, &z2) // z3 = x1(DA-CB)^2
		z2.add(&x2, &x3) // (DA+CB)
		x3.sqr(&z2)      // x3 = (DA+CB)^2

		z2.sqr(&t1)      // AA = A^2
		t1.sqr(&t2)      // BB = B^2
		x2.mul(&z2, &t1) // x2 = AA*BB
		t2.sub(&z2, &t1) // E = AA-BB

		t1.mlw(&t2, -edwardsD) // E*-d = a24*E
		t1.add(&t1, &z2)       // AA + a24*E
		z2.mul(&t2, &t1)       // z2 = E(AA+a24*E)
	}

	// Finish
	x2.condSwap(&x3, swap)
	z2.condSwap(&x3, swap)
	z2.inv(&z2)
	x1.mul(&x2, &z2)
	x1.ser(out)

	// As with X25519, both sides MUST check, without leaking extra
	// information about the value of K, whether the resulting shared K is
	// the all-zero value and abort if so.
	var nz limbSint
	for _, v := range out {
		nz |= (limbSint)(v)
	}
	nz = (nz - 1) >> 8 // 0 = succ, -1 = fail

	// return value: 0 = succ, -1 = fail
	return (int)(nz)
}

func ScalarBaseMult(out, scalar *[56]byte) int {
	return ScalarMult(out, scalar, &basePoint)
}
