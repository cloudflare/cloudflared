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

package x448

// This should really use 64 bit limbs, but Go is fucking retarded and doesn't
// have __(u)int128_t, so the 32 bit code it is, at a hefty performance
// penalty.  Fuck my life, I'm going to have to bust out PeachPy to get this
// to go fast aren't I.

const (
	wBits     = 32
	lBits     = (wBits * 7 / 8)
	x448Limbs = (448 / lBits)
	lMask     = (1 << lBits) - 1
)

type limbUint uint32
type limbSint int32

type gf struct {
	limb [x448Limbs]uint32
}

var zero = gf{[x448Limbs]uint32{0}}
var one = gf{[x448Limbs]uint32{1}}
var p = gf{[x448Limbs]uint32{
	lMask, lMask, lMask, lMask, lMask, lMask, lMask, lMask,
	lMask - 1, lMask, lMask, lMask, lMask, lMask, lMask, lMask,
}}

// cpy copies x = y.
func (x *gf) cpy(y *gf) {
	// for i, v := range y.limb {
	//	x.limb[i] = v
	// }

	copy(x.limb[:], y.limb[:])
}

// mul multiplies c = a * b. (PERF)
func (c *gf) mul(a, b *gf) {
	var aa gf
	aa.cpy(a)

	//
	// This is *by far* the most CPU intesive routine in the code.
	//

	// var accum [x448Limbs]uint64
	// for i, bv := range b.limb {
	//	for j, aav := range aa.limb {
	//		accum[(i+j)%x448Limbs] += (uint64)(bv) * (uint64)(aav)
	//	}
	//	aa.limb[(x448Limbs-1-i)^(x448Limbs/2)] += aa.limb[x448Limbs-1-i]
	// }

	// So fucking stupid that this is actually a fairly massive gain.
	var accum0, accum1, accum2, accum3, accum4, accum5, accum6, accum7, accum8, accum9, accum10, accum11, accum12, accum13, accum14, accum15 uint64
	var bv uint64

	bv = (uint64)(b.limb[0])
	accum0 += bv * (uint64)(aa.limb[0])
	accum1 += bv * (uint64)(aa.limb[1])
	accum2 += bv * (uint64)(aa.limb[2])
	accum3 += bv * (uint64)(aa.limb[3])
	accum4 += bv * (uint64)(aa.limb[4])
	accum5 += bv * (uint64)(aa.limb[5])
	accum6 += bv * (uint64)(aa.limb[6])
	accum7 += bv * (uint64)(aa.limb[7])
	accum8 += bv * (uint64)(aa.limb[8])
	accum9 += bv * (uint64)(aa.limb[9])
	accum10 += bv * (uint64)(aa.limb[10])
	accum11 += bv * (uint64)(aa.limb[11])
	accum12 += bv * (uint64)(aa.limb[12])
	accum13 += bv * (uint64)(aa.limb[13])
	accum14 += bv * (uint64)(aa.limb[14])
	accum15 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-0)^(x448Limbs/2)] += aa.limb[x448Limbs-1-0]

	bv = (uint64)(b.limb[1])
	accum1 += bv * (uint64)(aa.limb[0])
	accum2 += bv * (uint64)(aa.limb[1])
	accum3 += bv * (uint64)(aa.limb[2])
	accum4 += bv * (uint64)(aa.limb[3])
	accum5 += bv * (uint64)(aa.limb[4])
	accum6 += bv * (uint64)(aa.limb[5])
	accum7 += bv * (uint64)(aa.limb[6])
	accum8 += bv * (uint64)(aa.limb[7])
	accum9 += bv * (uint64)(aa.limb[8])
	accum10 += bv * (uint64)(aa.limb[9])
	accum11 += bv * (uint64)(aa.limb[10])
	accum12 += bv * (uint64)(aa.limb[11])
	accum13 += bv * (uint64)(aa.limb[12])
	accum14 += bv * (uint64)(aa.limb[13])
	accum15 += bv * (uint64)(aa.limb[14])
	accum0 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-1)^(x448Limbs/2)] += aa.limb[x448Limbs-1-1]

	bv = (uint64)(b.limb[2])
	accum2 += bv * (uint64)(aa.limb[0])
	accum3 += bv * (uint64)(aa.limb[1])
	accum4 += bv * (uint64)(aa.limb[2])
	accum5 += bv * (uint64)(aa.limb[3])
	accum6 += bv * (uint64)(aa.limb[4])
	accum7 += bv * (uint64)(aa.limb[5])
	accum8 += bv * (uint64)(aa.limb[6])
	accum9 += bv * (uint64)(aa.limb[7])
	accum10 += bv * (uint64)(aa.limb[8])
	accum11 += bv * (uint64)(aa.limb[9])
	accum12 += bv * (uint64)(aa.limb[10])
	accum13 += bv * (uint64)(aa.limb[11])
	accum14 += bv * (uint64)(aa.limb[12])
	accum15 += bv * (uint64)(aa.limb[13])
	accum0 += bv * (uint64)(aa.limb[14])
	accum1 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-2)^(x448Limbs/2)] += aa.limb[x448Limbs-1-2]

	bv = (uint64)(b.limb[3])
	accum3 += bv * (uint64)(aa.limb[0])
	accum4 += bv * (uint64)(aa.limb[1])
	accum5 += bv * (uint64)(aa.limb[2])
	accum6 += bv * (uint64)(aa.limb[3])
	accum7 += bv * (uint64)(aa.limb[4])
	accum8 += bv * (uint64)(aa.limb[5])
	accum9 += bv * (uint64)(aa.limb[6])
	accum10 += bv * (uint64)(aa.limb[7])
	accum11 += bv * (uint64)(aa.limb[8])
	accum12 += bv * (uint64)(aa.limb[9])
	accum13 += bv * (uint64)(aa.limb[10])
	accum14 += bv * (uint64)(aa.limb[11])
	accum15 += bv * (uint64)(aa.limb[12])
	accum0 += bv * (uint64)(aa.limb[13])
	accum1 += bv * (uint64)(aa.limb[14])
	accum2 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-3)^(x448Limbs/2)] += aa.limb[x448Limbs-1-3]

	bv = (uint64)(b.limb[4])
	accum4 += bv * (uint64)(aa.limb[0])
	accum5 += bv * (uint64)(aa.limb[1])
	accum6 += bv * (uint64)(aa.limb[2])
	accum7 += bv * (uint64)(aa.limb[3])
	accum8 += bv * (uint64)(aa.limb[4])
	accum9 += bv * (uint64)(aa.limb[5])
	accum10 += bv * (uint64)(aa.limb[6])
	accum11 += bv * (uint64)(aa.limb[7])
	accum12 += bv * (uint64)(aa.limb[8])
	accum13 += bv * (uint64)(aa.limb[9])
	accum14 += bv * (uint64)(aa.limb[10])
	accum15 += bv * (uint64)(aa.limb[11])
	accum0 += bv * (uint64)(aa.limb[12])
	accum1 += bv * (uint64)(aa.limb[13])
	accum2 += bv * (uint64)(aa.limb[14])
	accum3 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-4)^(x448Limbs/2)] += aa.limb[x448Limbs-1-4]

	bv = (uint64)(b.limb[5])
	accum5 += bv * (uint64)(aa.limb[0])
	accum6 += bv * (uint64)(aa.limb[1])
	accum7 += bv * (uint64)(aa.limb[2])
	accum8 += bv * (uint64)(aa.limb[3])
	accum9 += bv * (uint64)(aa.limb[4])
	accum10 += bv * (uint64)(aa.limb[5])
	accum11 += bv * (uint64)(aa.limb[6])
	accum12 += bv * (uint64)(aa.limb[7])
	accum13 += bv * (uint64)(aa.limb[8])
	accum14 += bv * (uint64)(aa.limb[9])
	accum15 += bv * (uint64)(aa.limb[10])
	accum0 += bv * (uint64)(aa.limb[11])
	accum1 += bv * (uint64)(aa.limb[12])
	accum2 += bv * (uint64)(aa.limb[13])
	accum3 += bv * (uint64)(aa.limb[14])
	accum4 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-5)^(x448Limbs/2)] += aa.limb[x448Limbs-1-5]

	bv = (uint64)(b.limb[6])
	accum6 += bv * (uint64)(aa.limb[0])
	accum7 += bv * (uint64)(aa.limb[1])
	accum8 += bv * (uint64)(aa.limb[2])
	accum9 += bv * (uint64)(aa.limb[3])
	accum10 += bv * (uint64)(aa.limb[4])
	accum11 += bv * (uint64)(aa.limb[5])
	accum12 += bv * (uint64)(aa.limb[6])
	accum13 += bv * (uint64)(aa.limb[7])
	accum14 += bv * (uint64)(aa.limb[8])
	accum15 += bv * (uint64)(aa.limb[9])
	accum0 += bv * (uint64)(aa.limb[10])
	accum1 += bv * (uint64)(aa.limb[11])
	accum2 += bv * (uint64)(aa.limb[12])
	accum3 += bv * (uint64)(aa.limb[13])
	accum4 += bv * (uint64)(aa.limb[14])
	accum5 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-6)^(x448Limbs/2)] += aa.limb[x448Limbs-1-6]

	bv = (uint64)(b.limb[7])
	accum7 += bv * (uint64)(aa.limb[0])
	accum8 += bv * (uint64)(aa.limb[1])
	accum9 += bv * (uint64)(aa.limb[2])
	accum10 += bv * (uint64)(aa.limb[3])
	accum11 += bv * (uint64)(aa.limb[4])
	accum12 += bv * (uint64)(aa.limb[5])
	accum13 += bv * (uint64)(aa.limb[6])
	accum14 += bv * (uint64)(aa.limb[7])
	accum15 += bv * (uint64)(aa.limb[8])
	accum0 += bv * (uint64)(aa.limb[9])
	accum1 += bv * (uint64)(aa.limb[10])
	accum2 += bv * (uint64)(aa.limb[11])
	accum3 += bv * (uint64)(aa.limb[12])
	accum4 += bv * (uint64)(aa.limb[13])
	accum5 += bv * (uint64)(aa.limb[14])
	accum6 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-7)^(x448Limbs/2)] += aa.limb[x448Limbs-1-7]

	bv = (uint64)(b.limb[8])
	accum8 += bv * (uint64)(aa.limb[0])
	accum9 += bv * (uint64)(aa.limb[1])
	accum10 += bv * (uint64)(aa.limb[2])
	accum11 += bv * (uint64)(aa.limb[3])
	accum12 += bv * (uint64)(aa.limb[4])
	accum13 += bv * (uint64)(aa.limb[5])
	accum14 += bv * (uint64)(aa.limb[6])
	accum15 += bv * (uint64)(aa.limb[7])
	accum0 += bv * (uint64)(aa.limb[8])
	accum1 += bv * (uint64)(aa.limb[9])
	accum2 += bv * (uint64)(aa.limb[10])
	accum3 += bv * (uint64)(aa.limb[11])
	accum4 += bv * (uint64)(aa.limb[12])
	accum5 += bv * (uint64)(aa.limb[13])
	accum6 += bv * (uint64)(aa.limb[14])
	accum7 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-8)^(x448Limbs/2)] += aa.limb[x448Limbs-1-8]

	bv = (uint64)(b.limb[9])
	accum9 += bv * (uint64)(aa.limb[0])
	accum10 += bv * (uint64)(aa.limb[1])
	accum11 += bv * (uint64)(aa.limb[2])
	accum12 += bv * (uint64)(aa.limb[3])
	accum13 += bv * (uint64)(aa.limb[4])
	accum14 += bv * (uint64)(aa.limb[5])
	accum15 += bv * (uint64)(aa.limb[6])
	accum0 += bv * (uint64)(aa.limb[7])
	accum1 += bv * (uint64)(aa.limb[8])
	accum2 += bv * (uint64)(aa.limb[9])
	accum3 += bv * (uint64)(aa.limb[10])
	accum4 += bv * (uint64)(aa.limb[11])
	accum5 += bv * (uint64)(aa.limb[12])
	accum6 += bv * (uint64)(aa.limb[13])
	accum7 += bv * (uint64)(aa.limb[14])
	accum8 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-9)^(x448Limbs/2)] += aa.limb[x448Limbs-1-9]

	bv = (uint64)(b.limb[10])
	accum10 += bv * (uint64)(aa.limb[0])
	accum11 += bv * (uint64)(aa.limb[1])
	accum12 += bv * (uint64)(aa.limb[2])
	accum13 += bv * (uint64)(aa.limb[3])
	accum14 += bv * (uint64)(aa.limb[4])
	accum15 += bv * (uint64)(aa.limb[5])
	accum0 += bv * (uint64)(aa.limb[6])
	accum1 += bv * (uint64)(aa.limb[7])
	accum2 += bv * (uint64)(aa.limb[8])
	accum3 += bv * (uint64)(aa.limb[9])
	accum4 += bv * (uint64)(aa.limb[10])
	accum5 += bv * (uint64)(aa.limb[11])
	accum6 += bv * (uint64)(aa.limb[12])
	accum7 += bv * (uint64)(aa.limb[13])
	accum8 += bv * (uint64)(aa.limb[14])
	accum9 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-10)^(x448Limbs/2)] += aa.limb[x448Limbs-1-10]

	bv = (uint64)(b.limb[11])
	accum11 += bv * (uint64)(aa.limb[0])
	accum12 += bv * (uint64)(aa.limb[1])
	accum13 += bv * (uint64)(aa.limb[2])
	accum14 += bv * (uint64)(aa.limb[3])
	accum15 += bv * (uint64)(aa.limb[4])
	accum0 += bv * (uint64)(aa.limb[5])
	accum1 += bv * (uint64)(aa.limb[6])
	accum2 += bv * (uint64)(aa.limb[7])
	accum3 += bv * (uint64)(aa.limb[8])
	accum4 += bv * (uint64)(aa.limb[9])
	accum5 += bv * (uint64)(aa.limb[10])
	accum6 += bv * (uint64)(aa.limb[11])
	accum7 += bv * (uint64)(aa.limb[12])
	accum8 += bv * (uint64)(aa.limb[13])
	accum9 += bv * (uint64)(aa.limb[14])
	accum10 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-11)^(x448Limbs/2)] += aa.limb[x448Limbs-1-11]

	bv = (uint64)(b.limb[12])
	accum12 += bv * (uint64)(aa.limb[0])
	accum13 += bv * (uint64)(aa.limb[1])
	accum14 += bv * (uint64)(aa.limb[2])
	accum15 += bv * (uint64)(aa.limb[3])
	accum0 += bv * (uint64)(aa.limb[4])
	accum1 += bv * (uint64)(aa.limb[5])
	accum2 += bv * (uint64)(aa.limb[6])
	accum3 += bv * (uint64)(aa.limb[7])
	accum4 += bv * (uint64)(aa.limb[8])
	accum5 += bv * (uint64)(aa.limb[9])
	accum6 += bv * (uint64)(aa.limb[10])
	accum7 += bv * (uint64)(aa.limb[11])
	accum8 += bv * (uint64)(aa.limb[12])
	accum9 += bv * (uint64)(aa.limb[13])
	accum10 += bv * (uint64)(aa.limb[14])
	accum11 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-12)^(x448Limbs/2)] += aa.limb[x448Limbs-1-12]

	bv = (uint64)(b.limb[13])
	accum13 += bv * (uint64)(aa.limb[0])
	accum14 += bv * (uint64)(aa.limb[1])
	accum15 += bv * (uint64)(aa.limb[2])
	accum0 += bv * (uint64)(aa.limb[3])
	accum1 += bv * (uint64)(aa.limb[4])
	accum2 += bv * (uint64)(aa.limb[5])
	accum3 += bv * (uint64)(aa.limb[6])
	accum4 += bv * (uint64)(aa.limb[7])
	accum5 += bv * (uint64)(aa.limb[8])
	accum6 += bv * (uint64)(aa.limb[9])
	accum7 += bv * (uint64)(aa.limb[10])
	accum8 += bv * (uint64)(aa.limb[11])
	accum9 += bv * (uint64)(aa.limb[12])
	accum10 += bv * (uint64)(aa.limb[13])
	accum11 += bv * (uint64)(aa.limb[14])
	accum12 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-13)^(x448Limbs/2)] += aa.limb[x448Limbs-1-13]

	bv = (uint64)(b.limb[14])
	accum14 += bv * (uint64)(aa.limb[0])
	accum15 += bv * (uint64)(aa.limb[1])
	accum0 += bv * (uint64)(aa.limb[2])
	accum1 += bv * (uint64)(aa.limb[3])
	accum2 += bv * (uint64)(aa.limb[4])
	accum3 += bv * (uint64)(aa.limb[5])
	accum4 += bv * (uint64)(aa.limb[6])
	accum5 += bv * (uint64)(aa.limb[7])
	accum6 += bv * (uint64)(aa.limb[8])
	accum7 += bv * (uint64)(aa.limb[9])
	accum8 += bv * (uint64)(aa.limb[10])
	accum9 += bv * (uint64)(aa.limb[11])
	accum10 += bv * (uint64)(aa.limb[12])
	accum11 += bv * (uint64)(aa.limb[13])
	accum12 += bv * (uint64)(aa.limb[14])
	accum13 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-14)^(x448Limbs/2)] += aa.limb[x448Limbs-1-14]

	bv = (uint64)(b.limb[15])
	accum15 += bv * (uint64)(aa.limb[0])
	accum0 += bv * (uint64)(aa.limb[1])
	accum1 += bv * (uint64)(aa.limb[2])
	accum2 += bv * (uint64)(aa.limb[3])
	accum3 += bv * (uint64)(aa.limb[4])
	accum4 += bv * (uint64)(aa.limb[5])
	accum5 += bv * (uint64)(aa.limb[6])
	accum6 += bv * (uint64)(aa.limb[7])
	accum7 += bv * (uint64)(aa.limb[8])
	accum8 += bv * (uint64)(aa.limb[9])
	accum9 += bv * (uint64)(aa.limb[10])
	accum10 += bv * (uint64)(aa.limb[11])
	accum11 += bv * (uint64)(aa.limb[12])
	accum12 += bv * (uint64)(aa.limb[13])
	accum13 += bv * (uint64)(aa.limb[14])
	accum14 += bv * (uint64)(aa.limb[15])
	aa.limb[(x448Limbs-1-15)^(x448Limbs/2)] += aa.limb[x448Limbs-1-15]

	// accum[x448Limbs-1] += accum[x448Limbs-2] >> lBits
	// accum[x448Limbs-2] &= lMask
	// accum[x448Limbs/2] += accum[x448Limbs-1] >> lBits
	accum15 += accum14 >> lBits
	accum14 &= lMask
	accum8 += accum15 >> lBits

	// for j := uint(0); j < x448Limbs; j++ {
	//	accum[j] += accum[(j-1)%x448Limbs] >> lBits
	//	accum[(j-1)%x448Limbs] &= lMask
	// }
	accum0 += accum15 >> lBits
	accum15 &= lMask
	accum1 += accum0 >> lBits
	accum0 &= lMask
	accum2 += accum1 >> lBits
	accum1 &= lMask
	accum3 += accum2 >> lBits
	accum2 &= lMask
	accum4 += accum3 >> lBits
	accum3 &= lMask
	accum5 += accum4 >> lBits
	accum4 &= lMask
	accum6 += accum5 >> lBits
	accum5 &= lMask
	accum7 += accum6 >> lBits
	accum6 &= lMask
	accum8 += accum7 >> lBits
	accum7 &= lMask
	accum9 += accum8 >> lBits
	accum8 &= lMask
	accum10 += accum9 >> lBits
	accum9 &= lMask
	accum11 += accum10 >> lBits
	accum10 &= lMask
	accum12 += accum11 >> lBits
	accum11 &= lMask
	accum13 += accum12 >> lBits
	accum12 &= lMask
	accum14 += accum13 >> lBits
	accum13 &= lMask
	accum15 += accum14 >> lBits
	accum14 &= lMask

	// for j, accv := range accum {
	//	c.limb[j] = (uint32)(accv)
	// }
	c.limb[0] = (uint32)(accum0)
	c.limb[1] = (uint32)(accum1)
	c.limb[2] = (uint32)(accum2)
	c.limb[3] = (uint32)(accum3)
	c.limb[4] = (uint32)(accum4)
	c.limb[5] = (uint32)(accum5)
	c.limb[6] = (uint32)(accum6)
	c.limb[7] = (uint32)(accum7)
	c.limb[8] = (uint32)(accum8)
	c.limb[9] = (uint32)(accum9)
	c.limb[10] = (uint32)(accum10)
	c.limb[11] = (uint32)(accum11)
	c.limb[12] = (uint32)(accum12)
	c.limb[13] = (uint32)(accum13)
	c.limb[14] = (uint32)(accum14)
	c.limb[15] = (uint32)(accum15)
}

// sqr squares (c = x * x).  Just calls multiply. (PERF)
func (c *gf) sqr(x *gf) {
	c.mul(x, x)
}

// isqrt inverse square roots (y = 1/sqrt(x)), using an addition chain.
func (y *gf) isqrt(x *gf) {
	var a, b, c gf
	c.sqr(x)

	// XXX/Yawning, could unroll, but this is called only once.

	// STEP(b,x,1);
	b.mul(x, &c)
	c.cpy(&b)
	for i := 0; i < 1; i++ {
		c.sqr(&c)
	}

	// STEP(b,x,3);
	b.mul(x, &c)
	c.cpy(&b)
	for i := 0; i < 3; i++ {
		c.sqr(&c)
	}

	//STEP(a,b,3);
	a.mul(&b, &c)
	c.cpy(&a)
	for i := 0; i < 3; i++ {
		c.sqr(&c)
	}

	// STEP(a,b,9);
	a.mul(&b, &c)
	c.cpy(&a)
	for i := 0; i < 9; i++ {
		c.sqr(&c)
	}

	// STEP(b,a,1);
	b.mul(&a, &c)
	c.cpy(&b)
	for i := 0; i < 1; i++ {
		c.sqr(&c)
	}

	// STEP(a,x,18);
	a.mul(x, &c)
	c.cpy(&a)
	for i := 0; i < 18; i++ {
		c.sqr(&c)
	}

	// STEP(a,b,37);
	a.mul(&b, &c)
	c.cpy(&a)
	for i := 0; i < 37; i++ {
		c.sqr(&c)
	}

	// STEP(b,a,37);
	b.mul(&a, &c)
	c.cpy(&b)
	for i := 0; i < 37; i++ {
		c.sqr(&c)
	}

	// STEP(b,a,111);
	b.mul(&a, &c)
	c.cpy(&b)
	for i := 0; i < 111; i++ {
		c.sqr(&c)
	}

	// STEP(a,b,1);
	a.mul(&b, &c)
	c.cpy(&a)
	for i := 0; i < 1; i++ {
		c.sqr(&c)
	}

	// STEP(b,x,223);
	b.mul(x, &c)
	c.cpy(&b)
	for i := 0; i < 223; i++ {
		c.sqr(&c)
	}

	y.mul(&a, &c)
}

// inv inverses (y = 1/x).
func (y *gf) inv(x *gf) {
	var z, w gf
	z.sqr(x)     // x^2
	w.isqrt(&z)  // +- 1/sqrt(x^2) = +- 1/x
	z.sqr(&w)    // 1/x^2
	w.mul(x, &z) // 1/x
	y.cpy(&w)
}

// reduce weakly reduces mod p
func (x *gf) reduce() {
	x.limb[x448Limbs/2] += x.limb[x448Limbs-1] >> lBits

	// for j := uint(0); j < x448Limbs; j++ {
	//	x.limb[j] += x.limb[(j-1)%x448Limbs] >> lBits
	//	x.limb[(j-1)%x448Limbs] &= lMask
	// }
	x.limb[0] += x.limb[15] >> lBits
	x.limb[15] &= lMask
	x.limb[1] += x.limb[0] >> lBits
	x.limb[0] &= lMask
	x.limb[2] += x.limb[1] >> lBits
	x.limb[1] &= lMask
	x.limb[3] += x.limb[2] >> lBits
	x.limb[2] &= lMask
	x.limb[4] += x.limb[3] >> lBits
	x.limb[3] &= lMask
	x.limb[5] += x.limb[4] >> lBits
	x.limb[4] &= lMask
	x.limb[6] += x.limb[5] >> lBits
	x.limb[5] &= lMask
	x.limb[7] += x.limb[6] >> lBits
	x.limb[6] &= lMask
	x.limb[8] += x.limb[7] >> lBits
	x.limb[7] &= lMask
	x.limb[9] += x.limb[8] >> lBits
	x.limb[8] &= lMask
	x.limb[10] += x.limb[9] >> lBits
	x.limb[9] &= lMask
	x.limb[11] += x.limb[10] >> lBits
	x.limb[10] &= lMask
	x.limb[12] += x.limb[11] >> lBits
	x.limb[11] &= lMask
	x.limb[13] += x.limb[12] >> lBits
	x.limb[12] &= lMask
	x.limb[14] += x.limb[13] >> lBits
	x.limb[13] &= lMask
	x.limb[15] += x.limb[14] >> lBits
	x.limb[14] &= lMask
}

// add adds mod p. Conservatively always weak-reduces. (PERF)
func (x *gf) add(y, z *gf) {
	// for i, yv := range y.limb {
	//	x.limb[i] = yv + z.limb[i]
	// }
	x.limb[0] = y.limb[0] + z.limb[0]
	x.limb[1] = y.limb[1] + z.limb[1]
	x.limb[2] = y.limb[2] + z.limb[2]
	x.limb[3] = y.limb[3] + z.limb[3]
	x.limb[4] = y.limb[4] + z.limb[4]
	x.limb[5] = y.limb[5] + z.limb[5]
	x.limb[6] = y.limb[6] + z.limb[6]
	x.limb[7] = y.limb[7] + z.limb[7]
	x.limb[8] = y.limb[8] + z.limb[8]
	x.limb[9] = y.limb[9] + z.limb[9]
	x.limb[10] = y.limb[10] + z.limb[10]
	x.limb[11] = y.limb[11] + z.limb[11]
	x.limb[12] = y.limb[12] + z.limb[12]
	x.limb[13] = y.limb[13] + z.limb[13]
	x.limb[14] = y.limb[14] + z.limb[14]
	x.limb[15] = y.limb[15] + z.limb[15]

	x.reduce()
}

// sub subtracts mod p.  Conservatively always weak-reduces. (PERF)
func (x *gf) sub(y, z *gf) {
	// for i, yv := range y.limb {
	//	x.limb[i] = yv - z.limb[i] + 2*p.limb[i]
	// }
	x.limb[0] = y.limb[0] - z.limb[0] + 2*lMask
	x.limb[1] = y.limb[1] - z.limb[1] + 2*lMask
	x.limb[2] = y.limb[2] - z.limb[2] + 2*lMask
	x.limb[3] = y.limb[3] - z.limb[3] + 2*lMask
	x.limb[4] = y.limb[4] - z.limb[4] + 2*lMask
	x.limb[5] = y.limb[5] - z.limb[5] + 2*lMask
	x.limb[6] = y.limb[6] - z.limb[6] + 2*lMask
	x.limb[7] = y.limb[7] - z.limb[7] + 2*lMask
	x.limb[8] = y.limb[8] - z.limb[8] + 2*(lMask-1)
	x.limb[9] = y.limb[9] - z.limb[9] + 2*lMask
	x.limb[10] = y.limb[10] - z.limb[10] + 2*lMask
	x.limb[11] = y.limb[11] - z.limb[11] + 2*lMask
	x.limb[12] = y.limb[12] - z.limb[12] + 2*lMask
	x.limb[13] = y.limb[13] - z.limb[13] + 2*lMask
	x.limb[14] = y.limb[14] - z.limb[14] + 2*lMask
	x.limb[15] = y.limb[15] - z.limb[15] + 2*lMask

	x.reduce()
}

// condSwap swaps x and y in constant time.
func (x *gf) condSwap(y *gf, swap limbUint) {
	// for i, xv := range x.limb {
	//	s := (xv ^ y.limb[i]) & (uint32)(swap) // Sort of dumb, oh well.
	//	x.limb[i] ^= s
	//	y.limb[i] ^= s
	// }

	var s uint32

	s = (x.limb[0] ^ y.limb[0]) & (uint32)(swap)
	x.limb[0] ^= s
	y.limb[0] ^= s
	s = (x.limb[1] ^ y.limb[1]) & (uint32)(swap)
	x.limb[1] ^= s
	y.limb[1] ^= s
	s = (x.limb[2] ^ y.limb[2]) & (uint32)(swap)
	x.limb[2] ^= s
	y.limb[2] ^= s
	s = (x.limb[3] ^ y.limb[3]) & (uint32)(swap)
	x.limb[3] ^= s
	y.limb[3] ^= s
	s = (x.limb[4] ^ y.limb[4]) & (uint32)(swap)
	x.limb[4] ^= s
	y.limb[4] ^= s
	s = (x.limb[5] ^ y.limb[5]) & (uint32)(swap)
	x.limb[5] ^= s
	y.limb[5] ^= s
	s = (x.limb[6] ^ y.limb[6]) & (uint32)(swap)
	x.limb[6] ^= s
	y.limb[6] ^= s
	s = (x.limb[7] ^ y.limb[7]) & (uint32)(swap)
	x.limb[7] ^= s
	y.limb[7] ^= s
	s = (x.limb[8] ^ y.limb[8]) & (uint32)(swap)
	x.limb[8] ^= s
	y.limb[8] ^= s
	s = (x.limb[9] ^ y.limb[9]) & (uint32)(swap)
	x.limb[9] ^= s
	y.limb[9] ^= s
	s = (x.limb[10] ^ y.limb[10]) & (uint32)(swap)
	x.limb[10] ^= s
	y.limb[10] ^= s
	s = (x.limb[11] ^ y.limb[11]) & (uint32)(swap)
	x.limb[11] ^= s
	y.limb[11] ^= s
	s = (x.limb[12] ^ y.limb[12]) & (uint32)(swap)
	x.limb[12] ^= s
	y.limb[12] ^= s
	s = (x.limb[13] ^ y.limb[13]) & (uint32)(swap)
	x.limb[13] ^= s
	y.limb[13] ^= s
	s = (x.limb[14] ^ y.limb[14]) & (uint32)(swap)
	x.limb[14] ^= s
	y.limb[14] ^= s
	s = (x.limb[15] ^ y.limb[15]) & (uint32)(swap)
	x.limb[15] ^= s
	y.limb[15] ^= s
}

// mlw multiplies by a signed int.  NOT CONSTANT TIME wrt the sign of the int,
// but that's ok because it's only ever called with w = -edwardsD.  Just uses
// a full multiply. (PERF)
func (a *gf) mlw(b *gf, w int) {
	if w > 0 {
		ww := gf{[x448Limbs]uint32{(uint32)(w)}}
		a.mul(b, &ww)
	} else {
		// This branch is *NEVER* taken with the current code.
		panic("mul called with negative w")
		ww := gf{[x448Limbs]uint32{(uint32)(-w)}}
		a.mul(b, &ww)
		a.sub(&zero, a)
	}
}

// canon canonicalizes.
func (a *gf) canon() {
	a.reduce()

	// Subtract p with borrow.
	var carry int64
	for i, v := range a.limb {
		carry = carry + (int64)(v) - (int64)(p.limb[i])
		a.limb[i] = (uint32)(carry & lMask)
		carry >>= lBits
	}

	addback := carry
	carry = 0

	// Add it back.
	for i, v := range a.limb {
		carry = carry + (int64)(v) + (int64)(p.limb[i]&(uint32)(addback))
		a.limb[i] = uint32(carry & lMask)
		carry >>= lBits
	}
}

// deser deserializes into the limb representation.
func (s *gf) deser(ser *[x448Bytes]byte) {
	var buf uint64
	bits := uint(0)
	k := 0

	for i, v := range ser {
		buf |= (uint64)(v) << bits
		for bits += 8; (bits >= lBits || i == x448Bytes-1) && k < x448Limbs; bits, buf = bits-lBits, buf>>lBits {
			s.limb[k] = (uint32)(buf & lMask)
			k++
		}
	}
}

// ser serializes into byte representation.
func (a *gf) ser(ser *[x448Bytes]byte) {
	a.canon()
	k := 0
	bits := uint(0)
	var buf uint64
	for i, v := range a.limb {
		buf |= (uint64)(v) << bits
		for bits += lBits; (bits >= 8 || i == x448Limbs-1) && k < x448Bytes; bits, buf = bits-8, buf>>8 {
			ser[k] = (byte)(buf)
			k++
		}
	}
}

func init() {
	if x448Limbs != 16 {
		panic("x448Limbs != 16, unrolled loops likely broken")
	}
}
