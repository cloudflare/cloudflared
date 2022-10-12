//go:build (!amd64 && !arm64) || (arm64 && !go1.16)
// +build !amd64,!arm64 arm64,!go1.16

package keccakf1600

func permuteSIMDx2(state []uint64) { permuteScalarX2(state) }

func permuteSIMDx4(state []uint64) { permuteScalarX4(state) }
