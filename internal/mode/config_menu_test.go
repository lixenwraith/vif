package mode

import (
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
)

// TestEveryNodeHasTheKindItsFieldsServeAndAnIdOfItsOwn: a node's kind matches what
// it holds, a group its rows, a toggle two choices or a change or a bare command, a
// choice more than two, a step its steps or a change, a pick and an action what acts,
// a form its spec and an info row a value alone; and no two nodes share an id, each
// resolving back to itself, so a host's echo names one row.
func TestEveryNodeHasTheKindItsFieldsServeAndAnIdOfItsOwn(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &awaySession{menuSession: menuSession{bots: []engine.BotSeat{{ID: 10, Slot: 2, Graph: "default"}, {ID: 11, Slot: 3, Graph: "idle", Joining: true}}},
		participants: []engine.SessionParticipant{{Entity: 7, Slot: 1}, {Entity: 8, Slot: 4, Holder: 1}}}
	ExecuteCommand(ctx, "g")
	seen := map[string]bool{}
	var walk func(prefix string, nodes []configNode)
	walk = func(prefix string, nodes []configNode) {
		for _, n := range nodes {
			id := nodeID(prefix, n.key)
			if seen[id] {
				t.Errorf("%s: two nodes share the id", id)
			}
			seen[id] = true
			if got, ok := configNodeFor(ctx, id); !ok || got.key != n.key || got.kind != n.kind {
				t.Errorf("%s resolves to %q of kind %d, not itself", id, got.key, got.kind)
			}
			ok := n.kind == kindGroup || n.read != nil
			switch n.kind {
			case kindGroup:
				ok = (len(n.nodes) > 0 || n.extra != nil) && n.read == nil && n.command == "" && n.activate == nil
			case kindToggle:
				ok = ok && (len(n.choices) == 2 || n.change != nil || n.command != "" && n.choices == nil && n.steps == nil)
			case kindChoice:
				ok = ok && len(n.choices) > 2 && n.command != ""
			case kindStep:
				ok = ok && (len(n.steps) > 0 || n.change != nil)
			case kindPick:
				ok = ok && n.activate != nil
			case kindAction:
				ok = ok && (n.command != "" || n.activate != nil)
			case kindForm:
				_, found := configForms[n.form]
				ok = ok && found
			case kindInfo:
				ok = ok && n.command == "" && n.change == nil && n.activate == nil && n.choices == nil
			}
			if !ok {
				t.Errorf("%s: kind %d does not match its fields", id, n.kind)
			}
			if n.kind == kindGroup {
				walk(id, n.children(ctx))
			}
		}
	}
	walk("", configPages)
	for _, want := range []string{"game/pause", "scenarios/#Installed/pick/tower", "scenarios/start", "multiplayer/#Players/player/8", "multiplayer/#Bots/drop/11", "developer/#Recording/journal"} {
		if !seen[want] {
			t.Errorf("the tree holds no %s", want)
		}
	}
}
