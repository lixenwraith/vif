package vmath

import (
	"encoding/binary"
	"math/bits"
)

// Hash64 is a running 64-bit hash fixed across platforms and processes. A word
// folds in with one multiply, rotate and multiply, so eight bytes cost what one
// costs FNV-1a; Sum avalanches the state, and only a sum is a hash to compare.
type Hash64 uint64

// The xxHash64 primes, which this hash's round and avalanche are.
const (
	hashPrime1 = 0x9E3779B185EBCA87
	hashPrime2 = 0xC2B2AE3D27D4EB4F
	hashPrime3 = 0x165667B19E3779F9
	hashPrime5 = 0x27D4EB2F165667C5
)

// NewHash64 is the empty hash.
func NewHash64() Hash64 { return hashPrime5 }

// Word folds one word in.
func (h Hash64) Word(v uint64) Hash64 {
	return Hash64(bits.RotateLeft64(uint64(h)+v*hashPrime2, 31) * hashPrime1)
}

// Bytes folds b in a word at a time, a short tail as one word, then the length,
// so two sequences folded in turn cannot meet where one ends and the next begins.
func (h Hash64) Bytes(b []byte) Hash64 {
	n := len(b)
	for len(b) >= 8 {
		h = h.Word(binary.LittleEndian.Uint64(b))
		b = b[8:]
	}
	if len(b) > 0 {
		h = h.Word(tailWord(b))
	}
	return h.Word(uint64(n))
}

// String folds s in as Bytes folds its bytes, without copying it.
func (h Hash64) String(s string) Hash64 {
	n := len(s)
	for len(s) >= 8 {
		h = h.Word(uint64(s[0]) | uint64(s[1])<<8 | uint64(s[2])<<16 | uint64(s[3])<<24 |
			uint64(s[4])<<32 | uint64(s[5])<<40 | uint64(s[6])<<48 | uint64(s[7])<<56)
		s = s[8:]
	}
	if len(s) > 0 {
		var tail [8]byte
		copy(tail[:], s)
		h = h.Word(tailWord(tail[:]))
	}
	return h.Word(uint64(n))
}

// Sum avalanches the state, so every folded bit reaches every bit of the result.
func (h Hash64) Sum() uint64 {
	x := uint64(h)
	x ^= x >> 33
	x *= hashPrime2
	x ^= x >> 29
	x *= hashPrime3
	x ^= x >> 32
	return x
}

// tailWord packs up to eight bytes little-endian, the missing high bytes zero.
func tailWord(b []byte) uint64 {
	var tail [8]byte
	copy(tail[:], b)
	return binary.LittleEndian.Uint64(tail[:])
}
