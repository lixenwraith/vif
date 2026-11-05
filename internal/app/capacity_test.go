// The membership's own decisions, read off a run that answers nothing but its mode
// and a lobby nobody types into.

package app

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
)

// stubRun is a run as a membership decision that reads only its mode and its lobby
// sees it; asked anything else, it panics on the nil seam it embeds.
type stubRun struct {
	memberRun
	mode Mode
}

func (r stubRun) Mode() Mode { return r.mode }

// Scripted is false: no script drives the run.
func (stubRun) Scripted() bool { return false }

// LobbyEvents is a lobby nobody types into.
func (stubRun) LobbyEvents() <-chan terminal.Event { return nil }

// TestPlayersAndTheScenarioRangeSetTheCeilingAndTheParty: -players and max_players
// are cursor ceilings, the lower holding, of which sessionCapacity is the guest half.
// An explicit -players on a scripted or bot-driven host is also the lobby's party,
// min_players raises any lobby's, and a -players under min_players is refused. A run
// that joined holds to the -players its session was opened with, should it host.
func TestPlayersAndTheScenarioRangeSetTheCeilingAndTheParty(t *testing.T) {
	t.Parallel()
	embedded, err := resource.LoadScenario(resource.Options{Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name         string
		serves       bool
		participants int
		joined       int // the -players the offer this run joined on carried
		low, high    int // the scenario's range: zero keeps its default, a negative low is 0
		capacity     int
		players      int
		quorum       int
		refused      bool
	}{
		{name: "host/unset", capacity: parameter.MaxPlayers - 1,
			players: parameter.MaxPlayers, quorum: 1},
		{name: "host/two", participants: 2, capacity: 1, players: 2, quorum: 1},
		{name: "host/four", participants: 4, capacity: 3, players: 4, quorum: 3},
		{name: "host/over", participants: parameter.MaxPlayers + 4,
			capacity: parameter.MaxPlayers - 1, players: parameter.MaxPlayers,
			quorum: parameter.MaxPlayers - 1},
		{name: "serve/unset", serves: true, capacity: parameter.MaxPlayers,
			players: parameter.MaxPlayers, quorum: 1},
		{name: "serve/three", serves: true, participants: 3, capacity: 3,
			players: 3, quorum: 1},
		{name: "serve/one", serves: true, participants: 1, capacity: 1,
			players: 1, quorum: 1},
		{name: "host/scenario max", participants: 4, high: 2, capacity: 1, players: 2, quorum: 1},
		{name: "host/scenario min", low: 3, capacity: parameter.MaxPlayers - 1,
			players: parameter.MaxPlayers, quorum: 2},
		{name: "host/under min", participants: 2, low: 3, capacity: 1, players: 2, quorum: 1, refused: true},
		{name: "joined/under min", joined: 2, low: 3, capacity: 1, players: 2, quorum: 2, refused: true},
		{name: "serve/scenario range", serves: true, participants: 4, low: 2, high: 3,
			capacity: 3, players: 3, quorum: 2},
		{name: "serve/no players", serves: true, low: -1, capacity: parameter.MaxPlayers,
			players: parameter.MaxPlayers, quorum: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mode, sc := ModeHeadless, embedded
			if tt.serves {
				mode = ModeServer
			}
			if tt.low != 0 {
				sc.MinPlayers = max(tt.low, 0)
			}
			if tt.high != 0 {
				sc.MaxPlayers = tt.high
			}
			m := membership{run: stubRun{mode: mode}, scenario: &sc,
				cfg: &Membership{Participants: tt.participants}, wiring: &membershipWiring{sessionPlayers: tt.joined}}
			if err := m.playersFit(sc); (err != nil) != tt.refused {
				t.Fatalf("fit = %v, want refused %t", err, tt.refused)
			}
			if got := m.sessionCapacity(); got != tt.capacity {
				t.Fatalf("guest capacity = %d, want %d", got, tt.capacity)
			}
			if got := m.playerCapacity(); got != tt.players {
				t.Fatalf("cursor capacity = %d, want %d", got, tt.players)
			}
			if got := m.lobbyQuorum(); got != tt.quorum {
				t.Fatalf("quorum = %d, want %d", got, tt.quorum)
			}
		})
	}
}
