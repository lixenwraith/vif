package logfile

import (
	"testing"
	"unsafe"
)

func TestParseDomainRoundTripsTheJournalNames(t *testing.T) {
	// The names are core.DomainNames, written verbatim by the journal sink.
	// A rename there must surface here, not silently degrade to DomNone.
	for _, tc := range []struct {
		in   string
		want Domain
	}{
		{"shared", DomShared},
		{"player", DomPlayer},
		{"", DomNone},
		{"Shared", DomNone},
		{"world", DomNone},
	} {
		if got := ParseDomain([]byte(tc.in)); got != tc.want {
			t.Errorf("ParseDomain(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if tc.want != DomNone && tc.want.String() != tc.in {
			t.Errorf("Domain(%v).String() = %q, want %q", tc.want, tc.want.String(), tc.in)
		}
	}
}

func TestDomainByNameAcceptsTheThirdState(t *testing.T) {
	for _, in := range []string{"", "both", "all", "any"} {
		if d, ok := DomainByName(in); !ok || d != DomNone {
			t.Errorf("DomainByName(%q) = %v, %v; want DomNone, true", in, d, ok)
		}
	}
	if d, ok := DomainByName("player"); !ok || d != DomPlayer {
		t.Errorf("DomainByName(player) = %v, %v", d, ok)
	}
	if _, ok := DomainByName("nonsense"); ok {
		t.Error("DomainByName accepted an unknown state")
	}
}

func TestDomainInitialsAreDistinct(t *testing.T) {
	seen := map[byte]bool{}
	for d := DomShared; d < DomCount; d++ {
		c := d.Initial()
		if seen[c] {
			t.Fatalf("domain %v reuses gutter initial %q", d, c)
		}
		seen[c] = true
	}
}

func TestSyntheticMsgNamesOnlyTheAnchor(t *testing.T) {
	if got := SyntheticMsg(SubAnchor); got != SubAnchor {
		t.Errorf("SyntheticMsg(anchor) = %q, want %q", got, SubAnchor)
	}
	// A journal record carries ev, and every diagnostic record carries msg, so
	// nothing else may claim a synthetic discriminator.
	for _, sub := range []string{SubJournal, SubStat, "app", ""} {
		if got := SyntheticMsg(sub); got != "" {
			t.Errorf("SyntheticMsg(%q) = %q, want empty", sub, got)
		}
	}
}

func TestDurationUnitCoversTheAnchorTickInterval(t *testing.T) {
	// The anchor writes the tick interval as tick_ns; without the unit it
	// renders as a nine-digit integer.
	if u := DurationUnit("tick_ns"); u != DurNs {
		t.Errorf("DurationUnit(tick_ns) = %v, want DurNs", u)
	}
	if got := FormatDuration(50000000, DurNs); got != "50ms" {
		t.Errorf("FormatDuration(50000000, ns) = %q, want 50ms", got)
	}
	// jseq is a counter, not a duration: no suffix may capture it.
	for _, k := range []string{"jseq", "seq", "boundary", "session"} {
		if u := DurationUnit(k); u != DurNone {
			t.Errorf("DurationUnit(%q) = %v, want DurNone", k, u)
		}
	}
}

func TestMetaStaysPointerFreeAndCompact(t *testing.T) {
	// The index holds one Meta per line of every open file, so its size is the
	// tool's memory ceiling. Dom was added into the tail padding; anything that
	// grows the struct past 48 bytes costs a byte per record for real.
	if got := unsafe.Sizeof(Meta{}); got != 48 {
		t.Errorf("sizeof(Meta) = %d, want 48", got)
	}
}
