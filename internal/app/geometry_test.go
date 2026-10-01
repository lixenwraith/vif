package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
)

func geometryServer(t *testing.T, w, h int) *App {
	t.Helper()
	a, err := New(Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0", Width: w, Height: h,
		Resources: resource.Options{Embedded: true}, Seed: 0x6E01,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

func reportGeometry(t *testing.T, a *App, report network.JoinerReport) {
	t.Helper()
	offer, err := a.assignParticipant()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.noteJoinerReport(offer.Assigned, report); err != nil {
		t.Fatal(err)
	}
}

func mapSize(a *App) (int, int) {
	var w, h int
	a.World().RunSafe(func() {
		cfg := a.World().Resources.Config
		w, h = cfg.MapWidth, cfg.MapHeight
	})
	return w, h
}

// Without an operator size, the first admitted guest supplies the real geometry.
func TestAServerWithNoSizeTakesItsMapFromItsFirstGuest(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 0, 0)

	beforeW, beforeH := mapSize(a)
	if beforeW > 80 {
		t.Fatalf("a server with no -size started on a %dx%d map, expected the default", beforeW, beforeH)
	}

	reportGeometry(t, a, network.JoinerReport{Width: 160, Height: 45})
	a.adoptLobbyGeometry()

	w, h := mapSize(a)
	if w <= beforeW || h <= beforeH {
		t.Fatalf("map stayed at %dx%d after a 160x45 guest reported", w, h)
	}
	if w >= 160 || h >= 45 {
		t.Fatalf("map %dx%d did not subtract the margins from the guest's 160x45", w, h)
	}
	if a.Context().Width != 160 || a.Context().Height != 45 {
		t.Fatalf("the run's geometry is %dx%d, want the guest's 160x45",
			a.Context().Width, a.Context().Height)
	}
}

// TestFirstGuestWins holds the choice still. Guests arrive throughout the run
// through the mid-run gate, so sizing from the smallest would mean shrinking the
// map under participants already playing on it — which D-14 forbids for the same
// reason a terminal may not crop a shared map.
func TestFirstGuestWins(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 0, 0)

	reportGeometry(t, a, network.JoinerReport{Width: 160, Height: 45})
	reportGeometry(t, a, network.JoinerReport{Width: 80, Height: 24})
	a.adoptLobbyGeometry()

	if got := a.Context().Width; got != 160 {
		t.Fatalf("geometry = %d, want the first guest's 160", got)
	}
}

// TestAnOperatorSizeIsNotOverridden: -size is a statement, and a guest's terminal
// does not get to contradict it.
func TestAnOperatorSizeIsNotOverridden(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 100, 30)

	beforeW, beforeH := mapSize(a)
	reportGeometry(t, a, network.JoinerReport{Width: 200, Height: 60})
	a.adoptLobbyGeometry()

	if w, h := mapSize(a); w != beforeW || h != beforeH {
		t.Fatalf("an explicit -size was overridden: %dx%d became %dx%d", beforeW, beforeH, w, h)
	}
}

// TestAnUnreportedOrUnusableGeometryChangesNothing keeps the adoption advisory: a
// guest that says nothing, or says something a viewport could not fit in, leaves
// the session sized as it was rather than breaking it.
func TestAnUnreportedOrUnusableGeometryChangesNothing(t *testing.T) {
	t.Parallel()
	for _, report := range []network.JoinerReport{
		{},
		{Width: 160},
		{Width: 2, Height: 2},
		{Width: -5, Height: -5},
	} {
		a := geometryServer(t, 0, 0)
		beforeW, beforeH := mapSize(a)
		reportGeometry(t, a, report)
		a.adoptLobbyGeometry()
		if w, h := mapSize(a); w != beforeW || h != beforeH {
			t.Fatalf("report %+v moved the map from %dx%d to %dx%d", report, beforeW, beforeH, w, h)
		}
	}
}
