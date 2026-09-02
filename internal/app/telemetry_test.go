package app

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

func telemetryKeySet(reg *status.Registry) []string {
	keys := make([]string, 0, reg.TotalCount())
	for _, typed := range []struct {
		prefix string
		keys   []string
	}{
		{"bool:", reg.Bools.Keys()},
		{"float:", reg.Floats.Keys()},
		{"int:", reg.Ints.Keys()},
		{"string:", reg.Strings.Keys()},
	} {
		for _, key := range typed.keys {
			keys = append(keys, typed.prefix+key)
		}
	}
	slices.Sort(keys)
	return keys
}

func TestTelemetryRegistryStaysFrozenAcrossTicksAndReset(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	reg := a.World().Resources.Status
	if !reg.Frozen() {
		t.Fatal("ClockScheduler.Prepare did not freeze the status registry")
	}
	want := telemetryKeySet(reg)

	a.Settle()
	a.Tick(4)
	a.Reset(false)
	a.Tick(2)

	if got := telemetryKeySet(reg); !slices.Equal(got, want) {
		t.Fatalf("metric key set changed after freeze:\nwant %v\n got %v", want, got)
	}
	if late := reg.Ints.Get("stat.late").Load(); late != 0 {
		t.Fatalf("stat.late = %d; a metric was registered after Freeze", late)
	}
}

const (
	telemetryResetIntSentinel    int64  = 31337
	telemetryResetStringSentinel string = "telemetry-reset-sentinel"
)

func persistentTelemetryKey(kind, key string) bool {
	// snapshot.* is what a capture cost this process — the read, the encode, the
	// bytes, the install, the ticks a join had to catch up. It describes a transfer
	// rather than a game, and :new does not undo a join, so it survives a reset for
	// the same reason the corpus fingerprint and the recorder's own counters do.
	if strings.HasPrefix(key, "content.") || strings.HasPrefix(key, "rec.") ||
		strings.HasPrefix(key, "stat.") || strings.HasPrefix(key, "snapshot.") {
		return true
	}
	switch kind {
	case "int":
		switch key {
		case "context.frame", "context.screen_h", "context.screen_w", "engine.fps", "engine.speed_pct":
			return true
		}
	case "bool":
		// Host loss changes this process into an explicit local continuation. A
		// game reset starts a new run inside that continuation; it does not restore
		// the missing authority, so the player-facing fact must survive.
		return key == "network.host_lost"
	case "string":
		switch key {
		case "context.mode", "engine.breakpoint", "engine.speed", "glyph.density", "glyph.rate_mult":
			return true
		}
	}
	return false
}

func resetBootstrapInt(key string) bool {
	switch key {
	case "audio.mask",
		"energy.current", "energy.damage_multiplier", "heat.current",
		"event.dispatches", "event.settle_reset",
		"nav.buf_groups_hwm",
		"player.count",
		"spatial.cell_occupancy_hwm", "spatial.position_batch_hwm", "spatial.positions_hwm",
		"storm.buf_ellipse_offsets_hwm":
		return true
	}
	if !strings.HasPrefix(key, "player.") {
		return false
	}
	return strings.HasSuffix(key, ".control") || strings.HasSuffix(key, ".entity") ||
		strings.HasSuffix(key, ".energy.current") || strings.HasSuffix(key, ".heat.current")
}

func TestTelemetrySessionCountersReset(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	// Capture the canonical post-reset values, including live gauges rebuilt by
	// reset bootstrap events. The first reset drains startup-only work; the
	// second establishes the steady reset baseline reproduced below.
	a.Reset(false)
	a.Tick(0)
	a.Reset(false)
	a.Tick(0)
	reg := a.World().Resources.Status
	baselineBools := make(map[string]bool)
	baselineStrings := make(map[string]string)
	reg.Bools.Range(func(key string, v *atomic.Bool) {
		if !persistentTelemetryKey("bool", key) {
			baselineBools[key] = v.Load()
		}
	})
	reg.Strings.Range(func(key string, v *status.AtomicString) {
		if !persistentTelemetryKey("string", key) {
			baselineStrings[key] = v.Load()
		}
	})

	runScript(t, a)
	a.World().RunSafe(func() {
		reg.Ints.Range(func(key string, v *atomic.Int64) {
			if !persistentTelemetryKey("int", key) {
				v.Store(telemetryResetIntSentinel)
			}
		})
		reg.Bools.Range(func(key string, v *atomic.Bool) {
			if !persistentTelemetryKey("bool", key) {
				v.Store(!baselineBools[key])
			}
		})
		reg.Strings.Range(func(key string, v *status.AtomicString) {
			if !persistentTelemetryKey("string", key) {
				v.Store(telemetryResetStringSentinel)
			}
		})
	})

	// Reset dispatches EventGameResetRequest through the system router; Tick(0)
	// services the scheduler reset without advancing the new session.
	a.Reset(false)
	a.Tick(0)

	var stale []string
	reg.Ints.Range(func(key string, v *atomic.Int64) {
		if persistentTelemetryKey("int", key) {
			return
		}
		got := v.Load()
		if got == telemetryResetIntSentinel {
			stale = append(stale, fmt.Sprintf("int:%s=%d (not reset)", key, got))
			return
		}
		if got != 0 && !resetBootstrapInt(key) {
			stale = append(stale, fmt.Sprintf("int:%s=%d, want 0", key, got))
		}
	})
	reg.Bools.Range(func(key string, v *atomic.Bool) {
		if persistentTelemetryKey("bool", key) {
			return
		}
		if got, want := v.Load(), baselineBools[key]; got != want {
			stale = append(stale, fmt.Sprintf("bool:%s=%t, want %t", key, got, want))
		}
	})
	reg.Strings.Range(func(key string, v *status.AtomicString) {
		if persistentTelemetryKey("string", key) {
			return
		}
		if got, want := v.Load(), baselineStrings[key]; got != want {
			stale = append(stale, fmt.Sprintf("string:%s=%q, want %q", key, got, want))
		}
	})
	if len(stale) != 0 {
		t.Fatalf("session metrics survived reset: %v", stale)
	}
}

func TestTelemetryHeadlessSessionReportsActivity(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	runScript(t, a)
	a.Tick(parameter.StatSnapshotTicks)

	reg := a.World().Resources.Status
	for _, key := range []string{
		"engine.ticks",
		"entity.created_total",
		"event.dispatches",
		"event.settle_pre",
		"spatial.positions_hwm",
	} {
		if got := reg.Ints.Get(key).Load(); got == 0 {
			t.Errorf("%s remained zero in an active headless session", key)
		}
	}
	if got := reg.Strings.Get("event.dispatch_by_type").Load(); got == "" || got == "-" {
		t.Errorf("event.dispatch_by_type was not published: %q", got)
	}
	if got, want := reg.Ints.Get("entity.count").Load(), int64(a.World().Positions.CountEntities()); got != want {
		t.Errorf("entity.count = %d, live position count = %d", got, want)
	}
	if got, want := reg.Ints.Get("event.queue_len").Load(), int64(a.World().Resources.Event.Queue.Len()); got != want {
		t.Errorf("event.queue_len = %d, live queue length = %d", got, want)
	}
	requests := reg.Ints.Get("death.batch_count").Load()
	entities := reg.Ints.Get("death.batch_entities_total").Load()
	entitiesPerRequest := float64(0)
	if requests != 0 {
		entitiesPerRequest = float64(entities) / float64(requests)
	}
	t.Logf("headless telemetry: ticks=%d dispatches=%d entities=%d created=%d typing=%d/%d",
		reg.Ints.Get("engine.ticks").Load(), reg.Ints.Get("event.dispatches").Load(),
		reg.Ints.Get("entity.count").Load(), reg.Ints.Get("entity.created_total").Load(),
		reg.Ints.Get("typing.correct").Load(), reg.Ints.Get("typing.errors").Load())
	t.Logf("death requests: count=%d entities=%d entities_per_request=%.2f silent=%d flash=%d blossom=%d decay=%d fadeout=%d dust=%d other=%d",
		requests, entities, entitiesPerRequest,
		reg.Ints.Get("death.batch_silent").Load(), reg.Ints.Get("death.batch_flash").Load(),
		reg.Ints.Get("death.batch_blossom").Load(), reg.Ints.Get("death.batch_decay").Load(),
		reg.Ints.Get("death.batch_fadeout").Load(), reg.Ints.Get("death.batch_dust").Load(),
		reg.Ints.Get("death.batch_other").Load())
}

// TestSharedDigestCarriesDetailOnlyOnRequest pins the cost side of the diagnosis:
// the per-record breakdown is what turns a differing category into a differing
// record, and it is roughly a hundred hashes, so a healthy session must not pay
// for it. NetworkSystem asks for it only once a sample has already disagreed.
func TestSharedDigestCarriesDetailOnlyOnRequest(t *testing.T) {
	a := mustHeadless(t, 0xD1FF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	var plain, detailed engine.SharedStateDigest
	a.World().RunSafe(func() {
		plain = a.sharedDigestLocked(false)
		detailed = a.sharedDigestLocked(true)
	})
	if plain.Groups != nil {
		t.Fatalf("a plain digest carried %d record hashes, want none", len(plain.Groups))
	}
	if len(detailed.Groups) < 8 {
		t.Fatalf("a detailed digest carried %d record hashes, want the whole surface", len(detailed.Groups))
	}
	if plain.Hash != detailed.Hash || plain.Status != detailed.Status {
		t.Fatal("asking for detail changed the digest it explains")
	}
}
