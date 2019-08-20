/*
 * Go implementation of Google city hash (MIT license)
 * https://code.google.com/p/cityhash/
 *
 * MIT License http://www.opensource.org/licenses/mit-license.php
 *
 * I don't even want to pretend to understand the details of city hash.
 * I am only reproducing the logic in Go as faithfully as I can.
 *
 */

package cityhash102

import (
	"encoding/binary"
)

const (
	k0 uint64 = 0xc3a5c85c97cb3127
	k1 uint64 = 0xb492b66fbe98f273
	k2 uint64 = 0x9ae16a3b2f90404f
	k3 uint64 = 0xc949d7c7509e6557

	kMul uint64 = 0x9ddfea08eb382d69
)

func fetch64(p []byte) uint64 {
	return binary.LittleEndian.Uint64(p)
	//return uint64InExpectedOrder(unalignedLoad64(p))
}

func fetch32(p []byte) uint32 {
	return binary.LittleEndian.Uint32(p)
	//return uint32InExpectedOrder(unalignedLoad32(p))
}

func rotate64(val uint64, shift uint32) uint64 {
	if shift != 0 {
		return ((val >> shift) | (val << (64 - shift)))
	}

	return val
}

func rotate32(val uint32, shift uint32) uint32 {
	if shift != 0 {
		return ((val >> shift) | (val << (32 - shift)))
	}

	return val
}

func swap64(a, b *uint64) {
	*a, *b = *b, *a
}

func swap32(a, b *uint32) {
	*a, *b = *b, *a
}

func permute3(a, b, c *uint32) {
	swap32(a, b)
	swap32(a, c)
}

func rotate64ByAtLeast1(val uint64, shift uint32) uint64 {
	return (val >> shift) | (val << (64 - shift))
}

func shiftMix(val uint64) uint64 {
	return val ^ (val >> 47)
}

type Uint128 [2]uint64

func (this *Uint128) setLower64(l uint64) {
	this[0] = l
}

func (this *Uint128) setHigher64(h uint64) {
	this[1] = h
}

func (this Uint128) Lower64() uint64 {
	return this[0]
}

func (this Uint128) Higher64() uint64 {
	return this[1]
}

func (this Uint128) Bytes() []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b, this[0])
	binary.LittleEndian.PutUint64(b[8:], this[1])
	return b
}

func hash128to64(x Uint128) uint64 {
	// Murmur-inspired hashing.
	var a = (x.Lower64() ^ x.Higher64()) * kMul
	a ^= (a >> 47)
	var b = (x.Higher64() ^ a) * kMul
	b ^= (b >> 47)
	b *= kMul
	return b
}

func hashLen16(u, v uint64) uint64 {
	return hash128to64(Uint128{u, v})
}

func hashLen16_3(u, v, mul uint64) uint64 {
	// Murmur-inspired hashing.
	var a = (u ^ v) * mul
	a ^= (a >> 47)
	var b = (v ^ a) * mul
	b ^= (b >> 47)
	b *= mul
	return b
}

func hashLen0to16(s []byte, length uint32) uint64 {
	if length > 8 {
		var a = fetch64(s)
		var b = fetch64(s[length-8:])

		return hashLen16(a, rotate64ByAtLeast1(b+uint64(length), length)) ^ b
	}

	if length >= 4 {
		var a = fetch32(s)
		return hashLen16(uint64(length)+(uint64(a)<<3), uint64(fetch32(s[length-4:])))
	}

	if length > 0 {
		var a uint8 = uint8(s[0])
		var b uint8 = uint8(s[length>>1])
		var c uint8 = uint8(s[length-1])

		var y uint32 = uint32(a) + (uint32(b) << 8)
		var z uint32 = length + (uint32(c) << 2)

		return shiftMix(uint64(y)*k2^uint64(z)*k3) * k2
	}

	return k2
}

// This probably works well for 16-byte strings as well, but it may be overkill
func hashLen17to32(s []byte, length uint32) uint64 {
	var a = fetch64(s) * k1
	var b = fetch64(s[8:])
	var c = fetch64(s[length-8:]) * k2
	var d = fetch64(s[length-16:]) * k0

	return hashLen16(rotate64(a-b, 43)+rotate64(c, 30)+d,
		a+rotate64(b^k3, 20)-c+uint64(length))
}

func weakHashLen32WithSeeds(w, x, y, z, a, b uint64) Uint128 {
	a += w
	b = rotate64(b+a+z, 21)
	var c uint64 = a
	a += x
	a += y
	b += rotate64(a, 44)
	return Uint128{a + z, b + c}
}

func weakHashLen32WithSeeds_3(s []byte, a, b uint64) Uint128 {
	return weakHashLen32WithSeeds(fetch64(s), fetch64(s[8:]), fetch64(s[16:]), fetch64(s[24:]), a, b)
}

func hashLen33to64(s []byte, length uint32) uint64 {
	var z uint64 = fetch64(s[24:])
	var a uint64 = fetch64(s) + (uint64(length)+fetch64(s[length-16:]))*k0
	var b uint64 = rotate64(a+z, 52)
	var c uint64 = rotate64(a, 37)

	a += fetch64(s[8:])
	c += rotate64(a, 7)
	a += fetch64(s[16:])

	var vf uint64 = a + z
	var vs = b + rotate64(a, 31) + c

	a = fetch64(s[16:]) + fetch64(s[length-32:])
	z = fetch64(s[length-8:])
	b = rotate64(a+z, 52)
	c = rotate64(a, 37)
	a += fetch64(s[length-24:])
	c += rotate64(a, 7)
	a += fetch64(s[length-16:])

	wf := a + z
	ws := b + rotate64(a, 31) + c
	r := shiftMix((vf+ws)*k2 + (wf+vs)*k0)
	return shiftMix(r*k0+vs) * k2
}

func CityHash64(s []byte, length uint32) uint64 {
	if length <= 32 {
		if length <= 16 {
			return hashLen0to16(s, length)
		} else {
			return hashLen17to32(s, length)
		}
	} else if length <= 64 {
		return hashLen33to64(s, length)
	}

	var x uint64 = fetch64(s)
	var y uint64 = fetch64(s[length-16:]) ^ k1
	var z uint64 = fetch64(s[length-56:]) ^ k0

	var v Uint128 = weakHashLen32WithSeeds_3(s[length-64:], uint64(length), y)
	var w Uint128 = weakHashLen32WithSeeds_3(s[length-32:], uint64(length)*k1, k0)

	z += shiftMix(v.Higher64()) * k1
	x = rotate64(z+x, 39) * k1
	y = rotate64(y, 33) * k1

	length = (length - 1) & ^uint32(63)
	for {
		x = rotate64(x+y+v.Lower64()+fetch64(s[16:]), 37) * k1
		y = rotate64(y+v.Higher64()+fetch64(s[48:]), 42) * k1

		x ^= w.Higher64()
		y ^= v.Lower64()

		z = rotate64(z^w.Lower64(), 33)
		v = weakHashLen32WithSeeds_3(s, v.Higher64()*k1, x+w.Lower64())
		w = weakHashLen32WithSeeds_3(s[32:], z+w.Higher64(), y)

		swap64(&z, &x)
		s = s[64:]
		length -= 64

		if length == 0 {
			break
		}
	}

	return hashLen16(hashLen16(v.Lower64(), w.Lower64())+shiftMix(y)*k1+z, hashLen16(v.Higher64(), w.Higher64())+x)
}

func CityHash64WithSeed(s []byte, length uint32, seed uint64) uint64 {
	return CityHash64WithSeeds(s, length, k2, seed)
}

func CityHash64WithSeeds(s []byte, length uint32, seed0, seed1 uint64) uint64 {
	return hashLen16(CityHash64(s, length)-seed0, seed1)
}

func cityMurmur(s []byte, length uint32, seed Uint128) Uint128 {
	var a uint64 = seed.Lower64()
	var b uint64 = seed.Higher64()
	var c uint64 = 0
	var d uint64 = 0
	var l int32 = int32(length) - 16

	if l <= 0 { // len <= 16
		a = shiftMix(a*k1) * k1
		c = b*k1 + hashLen0to16(s, length)

		if length >= 8 {
			d = shiftMix(a + fetch64(s))
		} else {
			d = shiftMix(a + c)
		}

	} else { // len > 16
		c = hashLen16(fetch64(s[length-8:])+k1, a)
		d = hashLen16(b+uint64(length), c+fetch64(s[length-16:]))
		a += d

		for {
			a ^= shiftMix(fetch64(s)*k1) * k1
			a *= k1
			b ^= a
			c ^= shiftMix(fetch64(s[8:])*k1) * k1
			c *= k1
			d ^= c
			s = s[16:]
			l -= 16

			if l <= 0 {
				break
			}
		}
	}
	a = hashLen16(a, c)
	b = hashLen16(d, b)
	return Uint128{a ^ b, hashLen16(b, a)}
}

func CityHash128WithSeed(s []byte, length uint32, seed Uint128) Uint128 {
	if length < 128 {
		return cityMurmur(s, length, seed)
	}

	// We expect length >= 128 to be the common case.  Keep 56 bytes of state:
	// v, w, x, y, and z.
	var v, w Uint128
	var x uint64 = seed.Lower64()
	var y uint64 = seed.Higher64()
	var z uint64 = uint64(length) * k1

	var pos uint32
	var t = s

	v.setLower64(rotate64(y^k1, 49)*k1 + fetch64(s))
	v.setHigher64(rotate64(v.Lower64(), 42)*k1 + fetch64(s[8:]))
	w.setLower64(rotate64(y+z, 35)*k1 + x)
	w.setHigher64(rotate64(x+fetch64(s[88:]), 53) * k1)

	// This is the same inner loop as CityHash64(), manually unrolled.
	for {
		x = rotate64(x+y+v.Lower64()+fetch64(s[16:]), 37) * k1
		y = rotate64(y+v.Higher64()+fetch64(s[48:]), 42) * k1

		x ^= w.Higher64()
		y ^= v.Lower64()
		z = rotate64(z^w.Lower64(), 33)
		v = weakHashLen32WithSeeds_3(s, v.Higher64()*k1, x+w.Lower64())
		w = weakHashLen32WithSeeds_3(s[32:], z+w.Higher64(), y)
		swap64(&z, &x)
		s = s[64:]
		pos += 64

		x = rotate64(x+y+v.Lower64()+fetch64(s[16:]), 37) * k1
		y = rotate64(y+v.Higher64()+fetch64(s[48:]), 42) * k1
		x ^= w.Higher64()
		y ^= v.Lower64()
		z = rotate64(z^w.Lower64(), 33)
		v = weakHashLen32WithSeeds_3(s, v.Higher64()*k1, x+w.Lower64())
		w = weakHashLen32WithSeeds_3(s[32:], z+w.Higher64(), y)
		swap64(&z, &x)
		s = s[64:]
		pos += 64
		length -= 128

		if length < 128 {
			break
		}
	}

	y += rotate64(w.Lower64(), 37)*k0 + z
	x += rotate64(v.Lower64()+z, 49) * k0

	// If 0 < length < 128, hash up to 4 chunks of 32 bytes each from the end of s.
	var tailDone uint32
	for tailDone = 0; tailDone < length; {
		tailDone += 32
		y = rotate64(y-x, 42)*k0 + v.Higher64()

		//TODO why not use origin_len ?
		w.setLower64(w.Lower64() + fetch64(t[pos+length-tailDone+16:]))
		x = rotate64(x, 49)*k0 + w.Lower64()
		w.setLower64(w.Lower64() + v.Lower64())
		v = weakHashLen32WithSeeds_3(t[pos+length-tailDone:], v.Lower64(), v.Higher64())
	}
	// At this point our 48 bytes of state should contain more than
	// enough information for a strong 128-bit hash.  We use two
	// different 48-byte-to-8-byte hashes to get a 16-byte final result.
	x = hashLen16(x, v.Lower64())
	y = hashLen16(y, w.Lower64())

	return Uint128{hashLen16(x+v.Higher64(), w.Higher64()) + y,
		hashLen16(x+w.Higher64(), y+v.Higher64())}
}

func CityHash128(s []byte, length uint32) (result Uint128) {
	if length >= 16 {
		result = CityHash128WithSeed(s[16:length], length-16, Uint128{fetch64(s) ^ k3, fetch64(s[8:])})
	} else if length >= 8 {
		result = CityHash128WithSeed(nil, 0, Uint128{fetch64(s) ^ (uint64(length) * k0), fetch64(s[length-8:]) ^ k1})
	} else {
		result = CityHash128WithSeed(s, length, Uint128{k0, k1})
	}
	return
}
