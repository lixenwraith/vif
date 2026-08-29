package status

import "testing"

func TestRegistryTriggerIsScoped(t *testing.T) {
	a, b := NewRegistry(), NewRegistry()
	a.EnableRecorder(8)
	b.EnableRecorder(8)

	a.Trigger(TrigManual)
	if !a.rec.Load().pending.Load() {
		t.Fatal("registry a recorder has no pending trigger")
	}
	if b.rec.Load().pending.Load() {
		t.Fatal("registry b recorder received registry a's trigger")
	}
}
