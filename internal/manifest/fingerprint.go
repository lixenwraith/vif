package manifest

import (
	"strconv"
	"sync"
)

// Fingerprint identifies the simulation this build assembles.
//
// Two participants in one session run the same code over the same seed and expect
// the same world. What decides that world is not the binary — a debug build and a
// stripped one simulate identically — but the manifest: which components exist,
// which systems are constructed, in which order, in which domain, and which of them
// carry state a capture has to move. Change any of those and the two runs are
// different games that will agree for a while and then not.
//
// So this hashes exactly that set, and deliberately not the renderers: presentation
// decides nothing about the simulation, and a fingerprint that included it would
// refuse a session for a reason that could not have caused a divergence.
//
// It is an identity, not a version. It says two builds differ; it cannot say which
// is newer, and nothing should try to order two of them.
var Fingerprint = sync.OnceValue(computeFingerprint)

// computeFingerprint is Fingerprint without the memoisation, so a test can watch
// it react to a changed definition list.
func computeFingerprint() uint64 {
	const (
		offset uint64 = 14695981039346656037
		prime  uint64 = 1099511628211
	)
	h := offset
	write := func(s string) {
		for i := range len(s) {
			h ^= uint64(s[i])
			h *= prime
		}
		// Length-terminated, so ("ab","c") and ("a","bc") do not collide.
		h ^= uint64(len(s))
		h *= prime
	}

	write("components")
	for _, c := range Components {
		write(c.Field)
		write(c.Type)
		write(c.Domain)
	}
	// Order is part of the identity: ActiveSystems() preserves it, and construction
	// order decides which system observes which within one tick. A slice rather
	// than a map, because Go randomises map iteration and a fingerprint that
	// changed every process start would refuse every session.
	for _, set := range []struct {
		label   string
		systems []SystemDef
	}{{"systems", Systems}, {"context", ContextSystems}} {
		write(set.label)
		for _, sys := range set.systems {
			write(sys.Name)
			write(sys.Domain)
			write(sys.Snapshot)
			for _, r := range sys.Requires {
				write(r)
			}
			for _, o := range sys.Optional {
				write(o)
			}
		}
	}
	return h
}

// FingerprintString is the fingerprint as a log and wire value. Hex, because it is
// an identity that gets compared and pasted rather than a number that gets read.
func FingerprintString() string { return strconv.FormatUint(Fingerprint(), 16) }
