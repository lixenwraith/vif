// The membership's own decisions, read off a run that answers nothing but its mode
// and a lobby nobody types into.

package membership

import (
	"errors"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
)

// stubRun is a run as a membership decision that reads only its mode and its lobby
// sees it; asked anything else, it panics on the nil seam it embeds.
type stubRun struct {
	Run
	mode config.Mode
}

func (r stubRun) Mode() config.Mode { return r.mode }

// Scripted is false: no script drives the run.
func (stubRun) Scripted() bool { return false }

// LobbyEvents is a lobby nobody types into.
func (stubRun) LobbyEvents() <-chan terminal.Event { return nil }

// TestPlayersAndTheScenarioRangeSetTheCeilingAndTheParty: -players and max_players
// are cursor ceilings, the lower holding, of which SessionCapacity is the guest half.
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
			mode, sc := config.ModeHeadless, embedded
			if tt.serves {
				mode = config.ModeServer
			}
			if tt.low != 0 {
				sc.MinPlayers = max(tt.low, 0)
			}
			if tt.high != 0 {
				sc.MaxPlayers = tt.high
			}
			m := Membership{run: stubRun{mode: mode}, scenario: &sc,
				cfg: &config.Membership{Participants: tt.participants}, wiring: &Wiring{SessionPlayers: tt.joined}}
			if err := m.PlayersFit(sc); (err != nil) != tt.refused {
				t.Fatalf("fit = %v, want refused %t", err, tt.refused)
			}
			if got := m.SessionCapacity(); got != tt.capacity {
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

// TestASessionsOnePlaceWaitsForItsOwnPlayer: the place of a session's one player who
// left waits for that player's key, as a reloaded page dials back with: the probe
// reads ready for that dial, and a stranger's or a bot's is refused full, dismissing
// nobody, where a session of more places gives the place up (yieldLocked).
func TestASessionsOnePlaceWaitsForItsOwnPlayer(t *testing.T) {
	t.Parallel()
	sc, err := resource.LoadScenario(resource.Options{Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	key := network.NewKey().ID()
	m := &Membership{run: stubRun{mode: config.ModeServer}, scenario: &sc, cfg: &config.Membership{Participants: 1},
		wiring: &Wiring{}, life: lifecycle.New(lifecycle.Policy{}), awayOrder: []link.PeerID{2},
		sessionRoster: []network.RosterEntry{{ID: engine.CoordinatorParticipant, Slot: parameter.NoPlayerSlot},
			{ID: 2, Slot: 0, Away: true, Key: key}}}
	if !m.MayYield() {
		t.Fatal("a one-place session whose player left reads as seating no dial; its door takes that player back")
	}
	for range 2 {
		_, err := m.assignFor(event.JoinAnchor{}, 0, nil, []string{network.NewKey().ID()}, nil, nil, seatAnyone)
		if !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) || m.dismissWraith.Load() != 0 {
			t.Fatalf("a newcomer to a one-place session whose player left was answered %v, participant %d dismissed; want it full, nobody dismissed",
				err, m.dismissWraith.Load())
		}
	}
	if p, err := m.keyedLocked([]string{key}, 0, nil, false); err != nil || p.ID != 2 {
		t.Fatalf("the player's own key took back place %d (%v); want its own, 2", p.ID, err)
	}
}

// TestAHostsOwnOneSeatRefusesADialForGood: a host playing a scenario of one player
// holds its one seat itself, so a dial is refused for good, the door a spectator's.
func TestAHostsOwnOneSeatRefusesADialForGood(t *testing.T) {
	t.Parallel()
	sc, err := resource.LoadScenario(resource.Options{Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	sc.MaxPlayers = 1
	m := &Membership{run: stubRun{mode: config.ModeHeadless}, scenario: &sc, cfg: &config.Membership{}, wiring: &Wiring{}}
	if err := m.fullLocked(); !errors.Is(err, &network.Refusal{Code: network.RefusalNoSeat}) || network.RetryOf(err) != network.RetryNever ||
		!strings.Contains(err.Error(), "seats one player, so others can only spectate") {
		t.Fatalf("a dial at a host whose one seat is its own was refused %v; want no seat, for good, others spectating", err)
	}
}
