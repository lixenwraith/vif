package keys

import "testing"

// TestBindingsAreUnambiguous is the guard on the one table every key, help row
// and footer hint comes from: a new binding that shadows an old one is silent
// at run time, because the resolver's map keeps whichever was compiled last.
func TestBindingsAreUnambiguous(t *testing.T) {
	single := map[skey]Action{}
	seq := map[qkey]Action{}
	prefix := map[skey]bool{}

	for _, b := range Bindings {
		switch len(b.Seq) {
		case 1:
			k := skey{b.Mode, b.Seq[0]}
			if prev, dup := single[k]; dup && prev != b.Act {
				t.Errorf("%s is bound to two actions in mode %d", SeqString(b.Seq), b.Mode)
			}
			single[k] = b.Act
		case 2:
			k := qkey{b.Mode, b.Seq[0], b.Seq[1]}
			if prev, dup := seq[k]; dup && prev != b.Act {
				t.Errorf("%s is bound to two actions in mode %d", SeqString(b.Seq), b.Mode)
			}
			seq[k] = b.Act
			prefix[skey{b.Mode, b.Seq[0]}] = true
		default:
			t.Fatalf("binding %v has %d chords; the resolver handles 1 or 2", b.Act, len(b.Seq))
		}
	}
	// A chord that both stands alone and opens a sequence swallows the standalone
	// action: the resolver arms the prefix and returns ActNone.
	for k := range prefix {
		if _, clash := single[k]; clash {
			t.Errorf("%s is both a binding and a sequence prefix in mode %d",
				ChordString(k.c), k.m)
		}
	}
}

func TestDomainKeyResolves(t *testing.T) {
	r := NewResolver()
	if got := r.Resolve(ModeNormal, R('D')); got != ActDomain {
		t.Errorf("D resolved to %v, want ActDomain", got)
	}
	if got := KeysFor(ModeNormal, ActDomain); got != "D" {
		t.Errorf("KeysFor(ActDomain) = %q, want D", got)
	}
}

func TestHelpRowsCoverTheJournalGroup(t *testing.T) {
	var found bool
	for _, row := range HelpRows(ModeNormal) {
		if row.Group == "journal" {
			found = true
		}
	}
	if !found {
		t.Error("the journal group is missing from the help overlay")
	}
}
