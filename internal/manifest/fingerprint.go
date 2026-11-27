package manifest

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"strconv"
	"sync"
)

// Fingerprint identifies the simulation this build assembles, which the manifest
// decides and not the binary: the components, and the systems in order with their
// domains, dependencies and snapshot obligations. Renderers and a local-only
// capability decide nothing, so including them would refuse a compatible session.
// An identity, not a version: it says two builds differ, never which is newer.
var Fingerprint = sync.OnceValue(computeFingerprint)

// computeFingerprint is Fingerprint without the memoization, so a test can watch
// it react to a changed definition list.
func computeFingerprint() uint64 {
	h := vmath.FNVOffset64
	write := func(s string) {
		for i := range len(s) {
			h ^= uint64(s[i])
			h *= vmath.FNVPrime64
		}
		// Length-terminated, so ("ab","c") and ("a","bc") do not collide.
		h ^= uint64(len(s))
		h *= vmath.FNVPrime64
	}

	write("components")
	for _, c := range Components {
		write(c.Field)
		write(c.Type)
		write(c.Domain)
	}
	// Order is part of the identity: ActiveSystems() preserves it, and construction
	// order decides which system observes which within one tick. A slice rather
	// than a map, because Go randomizes map iteration and a fingerprint that
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
