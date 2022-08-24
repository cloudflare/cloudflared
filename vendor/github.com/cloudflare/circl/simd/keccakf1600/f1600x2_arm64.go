//go:build arm64 && go1.16
// +build arm64,go1.16

package keccakf1600

import "github.com/cloudflare/circl/internal/sha3"

func permuteSIMDx2(state []uint64) { f1600x2ARM(&state[0], &sha3.RC) }

func permuteSIMDx4(state []uint64) { permuteScalarX4(state) }

//go:noescape
func f1600x2ARM(state *uint64, rc *[24]uint64)
