// The membership's startup gates, waited out on a run whose lobby nobody types into.

package app

import (
	"errors"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
)

// TestEachStartupGateCarriesItsOwnDeadline is the boundary between the two gates a
// session start runs. The lobby's deadline is the allocated first-guest window and
// ends the session; the start gate's is one world install and ends only the wait, by
// giving up on whoever has not confirmed. Neither is read from the policy inside.
func TestEachStartupGateCarriesItsOwnDeadline(t *testing.T) {
	t.Parallel()
	life := lifecycle.New(lifecycle.Policy{FirstJoin: 80 * time.Millisecond})
	life.Start(time.Now())
	m := membership{run: stubRun{mode: ModeServer}}

	port := link.NewSocketPort(&network.DebugConfig(link.RoleHost, "127.0.0.1:0").Config)
	defer port.Close()

	never := func() bool { return false }
	expired := func(now time.Time) error { life.State(now); return errSessionExpired }
	err := m.waitForStartup(port, nil, life.State(time.Now()).Deadline, expired, never)
	if !errors.Is(err, errSessionExpired) {
		t.Fatalf("the lobby wait returned %v, want errSessionExpired", err)
	}

	// The start gate: its bound settles the condition instead of ending the run,
	// which is what stops a peer that connects and goes silent from holding a fresh
	// session for the Job's whole deadline.
	const bound = 60 * time.Millisecond
	settled := false
	start := time.Now()
	err = m.waitForStartup(port, nil, start.Add(bound),
		func(time.Time) error { settled = true; return nil },
		func() bool { return settled })
	if err != nil {
		t.Fatalf("the start gate returned %v, want it to settle on its own bound", err)
	}
	if elapsed := time.Since(start); elapsed < bound {
		t.Fatalf("the start gate settled after %s, before its %s bound", elapsed, bound)
	}
}
