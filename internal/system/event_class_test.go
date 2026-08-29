package system

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// pushMethods are the World methods that put an event on the queue. PushEventFull
// and PushEventDomain name their domain explicitly, so a profile mismatch there is
// deliberate and the class check still applies to the type they push.
var pushMethods = map[string]bool{
	"PushEvent": true, "PushLocal": true, "PushCrossing": true,
	"PushEventDomain": true, "PushEventFull": true, "PushEventOrigin": true,
}

// stampingPush names a domain at the call site, rather than inheriting the ambient
// one. A Stamped type is only meaningful if its producers use one of these, or push
// inside a WithDomain scope.
var stampingPush = map[string]bool{
	"PushLocal": true, "PushEventDomain": true, "PushEventFull": true, "PushCrossing": true,
}

// crossingPushes is the D-3 table as code: every owner-resolved push of a
// replicated event, keyed "system:EventName", with the artifact that crosses.
// Adding a player-domain push of a shared or bus event without an entry here fails
// TestEventClassMatchesSystemProfile; an entry that stops describing real code
// fails TestCrossingPushesAreLive.
var crossingPushes = map[string]string{
	// D-3 table, area effect: the explosion request carries centers, radius,
	// duration, attack family and owner cursor.
	"missile:EventExplosionRequest":   "missile impact; the explosion centers cross",
	"dust:EventExplosionBatchRequest": "dust detonation; the explosion centers cross",
	"weapon:EventExplosionRequest":    "disruptor pulse; center, ellipse radius, attack family and owner cross",

	// D-3 table, drain fusion: the spawn request carries the header cell only.
	"fuse:EventQuasarSpawnRequest": "drain fusion; the quasar header cell crosses",
	"fuse:EventSwarmSpawnRequest":  "drain fusion; the swarm header cell crosses",

	// D-3 table, gold: the typed member and its typist cross.
	"typing:EventCompositeMemberDestroyed": "gold member typed; header, member and typist cross",
	"nugget:EventCursorMoveRequest":        "a personal nugget jump moves the shared cursor",

	// Crossings the D-3 table does not name. Each is a player mechanic whose
	// shared outcome is determined by the artifact it pushes, so each needs a
	// wire path in Phase 7 exactly as the rows above do.
	"drain:EventCombatHealRequest":  "a dying drain donating its hit points; target and amount cross",
	"drain:EventDrainDefeated":      "one personal drain death advances shared progression",
	"fuse:EventDrainDefeated":       "each fused personal drain advances shared progression",
	"typing:EventCursorMoveRequest": "the post-typing advance moves the shared cursor",
	"energy:EventCursorDefeatState": "the owner's combined energy/heat lifecycle state crosses",
	"heat:EventCursorDefeatState":   "the owner's combined energy/heat lifecycle state crosses",

	// A shared species reads only the locally owned shield and crosses the exact
	// target/member set; periodic remote shield state never resolves shared combat.
	"quasar:EventCombatAttackAreaCrossingRequest": "owner-resolved shield impact on a shared quasar",
	"swarm:EventCombatAttackAreaCrossingRequest":  "owner-resolved shield impact on a shared swarm",
	"storm:EventCombatAttackAreaCrossingRequest":  "owner-resolved shield impact on a shared storm",
	"eye:EventCombatAttackAreaCrossingRequest":    "owner-resolved shield impact on a shared eye",
	"pylon:EventCombatAttackAreaCrossingRequest":  "owner-resolved shield impact on a shared pylon",
	"snake:EventCombatAttackAreaCrossingRequest":  "owner-resolved shield impact on a shared snake",
}

// systemPushes records, per event constant one system's file pushes, the World
// methods it pushes it with. The method matters: a D-3 crossing must stamp.
type systemPushes struct {
	name   string
	file   string
	domain string
	events map[string]map[string]bool
}

// TestEventClassMatchesSystemProfile checks every statically resolvable push
// against the D-10 class of the event and the D-15 profile of the pusher.
//
// One direction is a rule: a player-profile system pushing a replicated event is
// a D-3 crossing, and every one of them must be named in crossingPushes with the
// artifact it crosses. That list is the D-3 table, kept honest by the compiler
// rather than by review.
//
// The other direction is not a rule. A shared system routinely pushes local-class
// events: D-6 effects it requests and D-13 owner-authored state it damages both
// land per-instance. Those pushes are correct and are not checked here.
func TestEventClassMatchesSystemProfile(t *testing.T) {
	event.EnsureRegistry()

	pushes := parseSystemPushes(t, ".")
	if len(pushes) < 30 {
		t.Fatalf("collected pushes for %d systems; the parser has drifted", len(pushes))
	}

	var bad []string
	checked := 0
	for _, p := range pushes {
		for _, name := range sortedPushed(p.events) {
			et, ok := event.GetEventType(name)
			if !ok {
				t.Errorf("%s: pushes %s, which the registry does not know", p.file, name)
				continue
			}
			checked++

			class := event.ClassOf(et)
			switch {
			case class == event.ClassUnset:
				bad = append(bad, p.file+": "+name+" carries no replication class")
			case p.domain == "player" && (class == event.ClassShared || class == event.ClassBus):
				if crossingPushes[p.name+":"+name] == "" {
					bad = append(bad, p.file+": player-profile "+p.name+" pushes replicated "+
						name+" and is not in crossingPushes; name the artifact it crosses (D-3)")
				}
			}
		}
	}

	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("event class disagrees with the pushing system's profile:\n  %s",
			strings.Join(bad, "\n  "))
	}
	t.Logf("checked %d system/event pushes against the class table", checked)
}

// TestCrossingPushesAreLive fails on an entry no longer describing real code, and
// on a crossing that does not stamp. The stamp is what the wire reads: every Bus
// type also has shared producers re-deriving their own copy, and a crossing left in
// the ambient domain is indistinguishable from one of those (event.OnWire).
func TestCrossingPushesAreLive(t *testing.T) {
	pushes := parseSystemPushes(t, ".")
	methods := make(map[string]map[string]bool)
	for _, p := range pushes {
		for name, m := range p.events {
			methods[p.name+":"+name] = m
		}
	}
	for key := range crossingPushes {
		m, ok := methods[key]
		if !ok {
			t.Errorf("crossingPushes[%q] describes a push that no longer exists", key)
			continue
		}
		if !m["PushCrossing"] {
			t.Errorf("crossingPushes[%q] does not stamp: push it with World.PushCrossing (D-3)", key)
		}
	}
}

// parseSystemPushes walks every non-test file and records the event constants it
// pushes by name. A push whose type is a variable cannot be resolved here and is
// left to the runtime tap in internal/app.
func parseSystemPushes(t *testing.T, dir string) []systemPushes {
	t.Helper()

	domains := parseSystemDomains(t, "../manifest/definition.go")
	var out []systemPushes
	fset := token.NewFileSet()
	for _, n := range packageFiles(t, dir) {
		f, err := parser.ParseFile(fset, dir+"/"+n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		name := declaredName(f)
		if name == "" {
			continue // a helper file declares no system
		}
		domain, ok := domains[name]
		if !ok {
			continue // unregistered; TestSystemDomainProfiles already reports it
		}

		p := systemPushes{name: name, file: n, domain: domain, events: map[string]map[string]bool{}}
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !pushMethods[sel.Sel.Name] {
				return true
			}
			if ev := eventConstName(call.Args[0]); ev != "" {
				if p.events[ev] == nil {
					p.events[ev] = map[string]bool{}
				}
				p.events[ev][sel.Sel.Name] = true
			}
			return true
		})
		out = append(out, p)
	}
	return out
}

// eventConstName resolves "event.EventFoo" to "EventFoo"; anything else is a
// variable or expression this walk cannot resolve.
func eventConstName(arg ast.Expr) string {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "event" || !strings.HasPrefix(sel.Sel.Name, "Event") {
		return ""
	}
	return sel.Sel.Name
}

// sortedPushed orders one system's pushed event names for deterministic diagnostics
// TestStampedEventsAreExplicitlyStamped asserts a Stamped declaration is earned.
// The class defers the replication decision to GameEvent.Domain, which is only
// information if a producer set it: core.DomainShared is the zero value and the
// ambient domain defaults to it, so a bare PushEvent leaves every record reading
// "shared" whatever produced it. A type nothing stamps is misdeclared.
//
// The scan covers internal/system and internal/mode, the two packages that push
// simulation events; app and fsm push through the same World methods and are
// covered by the runtime audit instead.
func TestStampedEventsAreExplicitlyStamped(t *testing.T) {
	event.EnsureRegistry()

	stamped := make(map[string]bool) // event name -> some producer resolves a domain
	pushed := make(map[string]bool)
	for _, dir := range []string{".", "../mode"} {
		collectStampingEvidence(t, dir, pushed, stamped)
	}

	var bad []string
	for name := range pushed {
		et, ok := event.GetEventType(name)
		if !ok || event.ClassOf(et) != event.ClassStamped {
			continue
		}
		if !stamped[name] {
			bad = append(bad, name+": declared stamped, but every producer pushes it in the ambient domain")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("stamped types no producer resolves:\n  %s", strings.Join(bad, "\n  "))
	}
}

// collectStampingEvidence records which events a package pushes and which of them
// some call site stamps, either through a domain-naming method or inside WithDomain.
func collectStampingEvidence(t *testing.T, dir string, pushed, stamped map[string]bool) {
	t.Helper()

	fset := token.NewFileSet()
	for _, n := range packageFiles(t, dir) {
		f, err := parser.ParseFile(fset, dir+"/"+n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		// A file that opens a WithDomain scope stamps everything pushed inside it,
		// which this walk cannot delimit, so the whole file counts as stamping.
		scoped := false
		ast.Inspect(f, func(node ast.Node) bool {
			if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithDomain" {
				scoped = true
			}
			return true
		})
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !pushMethods[sel.Sel.Name] {
				return true
			}
			ev := eventConstName(call.Args[0])
			if ev == "" {
				return true
			}
			pushed[ev] = true
			if scoped || stampingPush[sel.Sel.Name] {
				stamped[ev] = true
			}
			return true
		})
	}
}

func sortedPushed(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
