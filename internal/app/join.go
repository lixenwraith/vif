// Package app: join handshake.
//
// A second participant reproduces a session rather than receiving it: same seed,
// same session counter, same config and corpus, so shared entity identity and
// creation order are identical from the first tick (D-11). Only the D-14 map latch
// is adopted rather than re-derived — the joiner's terminal must not decide the
// shared bounds. The carrier is JournalAnchor, which already describes exactly this
// set for replay; the join adds the latch and drops the recording terminal.
package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// ErrJoinMidRun is returned when the two participants are not at the same position.
// Nothing carries world state yet, so a joiner can only reproduce a session from
// its start; a later join needs the world snapshot Phase 7 does not transport.
var ErrJoinMidRun = errors.New("join: participants are not at the same position")

// JoinAnchor describes this session to a participant that wants to share it: the
// identity VerifyAnchor already checks, plus the live D-14 map latch.
// The terminal fields describe this instance and the joiner ignores them.
func (a *App) JoinAnchor() event.JoinAnchor {
	an := a.buildAnchor()
	an.Schema = event.JournalSchema
	a.world.RunSafe(func() {
		cfg := a.world.Resources.Config
		an.MapWidth, an.MapHeight, an.CropOnResize = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	})
	st := a.Position()
	an.Run, an.Tick = st.Run, st.Tick
	return event.JoinAnchor{Anchor: an}
}

// Join admits this App into the session an anchor describes: it verifies that this
// instance reproduces the identity, refuses a position it cannot reconstruct, and
// adopts the map latch. Call after NewHeadless, before the first Tick.
func (a *App) Join(j event.JoinAnchor) error {
	an := j.Anchor
	if err := firstAnchorMismatch("join", a.anchorIdentity(an)); err != nil {
		return err
	}
	if an.MapWidth <= 0 || an.MapHeight <= 0 {
		return fmt.Errorf("join: anchor carries no map latch (%dx%d)", an.MapWidth, an.MapHeight)
	}
	if st := a.Position(); st.Run != an.Run || st.Tick != an.Tick {
		return fmt.Errorf("%w: host at run %d tick %d, this instance at run %d tick %d",
			ErrJoinMidRun, an.Run, an.Tick, st.Run, st.Tick)
	}
	a.adoptMapLatch(an)
	return nil
}

// adoptMapLatch applies the host's bounds through the D-14 authority — the level
// setup path — rather than by writing Config, so the grid, the camera and every
// cursor reflow exactly as they would for a map script. Entities are kept: the
// joiner's world is the same seed's world, not a fresh one.
func (a *App) adoptMapLatch(an event.JournalAnchor) {
	a.SetupLevel(an.MapWidth, an.MapHeight, false, an.CropOnResize)
}

// AttachTransport binds a transport to this App, for a harness or an embedder that
// builds its own endpoint instead of taking the one NetworkService contributes.
// NetworkSystem reads the port per tick, so this needs no re-registration.
func (a *App) AttachTransport(port engine.NetworkPort) {
	a.world.RunSafe(func() { a.world.Resources.Network = engine.NewNetworkResource(port) })
}
