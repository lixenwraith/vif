package manifest

import (
	"os/exec"
	"strings"
	"testing"
)

// TestFingerprintIsStableAcrossProcesses is the property the whole thing rests on.
// A session refuses a peer whose fingerprint differs, so a fingerprint that varied
// between two runs of the same binary would refuse every session — which is what a
// map iteration or an unordered set inside it would produce.
func TestFingerprintIsStableAcrossProcesses(t *testing.T) {
	t.Parallel()
	want := FingerprintString()
	if want == "" || want == "0" {
		t.Fatalf("fingerprint = %q", want)
	}
	for range 8 {
		if got := FingerprintString(); got != want {
			t.Fatalf("fingerprint changed within one process: %q then %q", want, got)
		}
	}

	// A second process is the real test: map ordering is randomised per process,
	// so a repeat inside this one would agree with itself regardless.
	out, err := exec.Command("go", "run", "./testdata/fingerprint").CombinedOutput()
	if err != nil {
		t.Skipf("second process unavailable: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("another process computed %q, this one %q", got, want)
	}
}

// TestFingerprintTracksTheSimulationSet covers what it is supposed to notice and
// what it is supposed to ignore. Changing a system's domain changes the simulation;
// changing a renderer does not, and refusing a session over presentation would
// refuse it for something that cannot cause a divergence.
func TestFingerprintTracksTheSimulationSet(t *testing.T) {
	// Not parallel and not using the cached Fingerprint: these mutate the package
	// definition lists and restore them.
	base := computeFingerprint()

	systems := Systems
	t.Cleanup(func() { Systems = systems })

	Systems = append(append([]SystemDef{}, systems[1:]...), systems[0])
	if computeFingerprint() == base {
		t.Fatal("reordering the systems did not change the fingerprint")
	}

	Systems = append([]SystemDef{}, systems...)
	Systems[0].Domain = "player"
	if computeFingerprint() == base {
		t.Fatal("changing a system's domain did not change the fingerprint")
	}

	Systems = append([]SystemDef{}, systems...)
	Systems[0].Constructor = "NewSomethingElse"
	if computeFingerprint() != base {
		t.Fatal("a constructor rename changed the fingerprint; it names no simulation difference")
	}

	Systems = systems
	renderers := Renderers
	t.Cleanup(func() { Renderers = renderers })
	Renderers = renderers[1:]
	if computeFingerprint() != base {
		t.Fatal("dropping a renderer changed the fingerprint; presentation decides no simulation")
	}
}
