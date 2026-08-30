package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/event"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// hostParticipantID is the coordinator. It allocates every other identity, so it
// takes the first one itself and hands out the rest in arrival order.
const hostParticipantID network.PeerID = 1

var errSessionCanceled = errors.New("network session canceled")

// newInteractiveApp resolves the startup handshake before a joining App draws a seed.
func newInteractiveApp(cfg Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.JoinAddress != "" {
		return newJoiningApp(cfg)
	}
	if cfg.HostAddress != "" {
		return newHostingApp(cfg)
	}
	return New(cfg)
}

// newHostingApp installs a tick-zero acceptor before the service is initialized.
func newHostingApp(cfg Config) (*App, error) {
	a, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// hostNetworkConfig captures the App before the service starts its accept loop.
func (a *App) hostNetworkConfig() *network.Config {
	netCfg := network.DebugConfig(network.RoleHost, a.cfg.HostAddress)
	netCfg.ParticipantID = hostParticipantID
	netCfg.MaxPeers = a.remoteParticipantCount()
	netCfg.OnError = logSessionError
	netCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign:  a.assignParticipant,
		Release: a.releaseParticipant,
	}, netCfg.ConnectTimeout)
	return netCfg
}

// remoteParticipantCount is the lobby size this host waits for, excluding itself.
func (a *App) remoteParticipantCount() int {
	n := a.cfg.Participants
	if n < 2 {
		n = 2
	}
	if n > parameter.MaxPlayers {
		n = parameter.MaxPlayers
	}
	return n - 1
}

// newJoiningApp receives the host anchor, adopts it, then constructs the App.
func newJoiningApp(cfg Config) (*App, error) {
	netCfg := network.DebugConfig(network.RolePeer, cfg.JoinAddress)
	netCfg.OnError = logSessionError
	pending, offer, err := network.DialSession(cfg.JoinAddress, netCfg)
	if err != nil {
		return nil, fmt.Errorf("join %s: %w", cfg.JoinAddress, err)
	}
	reject := func(cause error) (*App, error) {
		_ = pending.Complete(cause)
		_ = pending.Close()
		return nil, cause
	}

	cfg.networkConfig = pending.TransportConfig()
	cfg, err = ConfigForJoin(cfg, offer)
	if err != nil {
		return reject(err)
	}
	a, err := New(cfg)
	if err != nil {
		return reject(err)
	}
	a.pendingJoin = pending
	a.sessionOffer = offer
	// Identity now, roster at the start gate: a mismatched joiner must be refused
	// before the host spends the rest of the lobby waiting for it.
	if err := a.Join(offer.Anchor); err != nil {
		_ = pending.Complete(err)
		a.Close()
		return nil, err
	}
	if err := pending.Complete(nil); err != nil {
		a.Close()
		return nil, fmt.Errorf("join reply: %w", err)
	}
	return a, nil
}

// assignParticipant allocates the next identity and returns the offer carrying it.
// One call per accepted connection: the roster is the lobby so far, and the roster a
// participant actually builds from arrives later, at the start gate.
func (a *App) assignParticipant() (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	limit := a.remoteParticipantCount() + 1
	if len(a.sessionRoster) == 0 {
		a.sessionRoster = []network.SessionParticipant{{ID: hostParticipantID, Slot: 0}}
	}
	if len(a.sessionRoster) >= limit {
		return network.SessionOffer{}, fmt.Errorf("session is full at %d participants", limit)
	}
	assigned := a.nextParticipantLocked()
	a.sessionRoster = append(a.sessionRoster, assigned)

	a.sessionOffer = a.offerLocked(anchor, assigned.ID)
	return a.sessionOffer, a.sessionOffer.Validate()
}

// nextParticipantLocked takes the lowest free identity and the lowest free slot, so
// a lobby that loses a joiner reuses its place rather than exhausting the roster.
func (a *App) nextParticipantLocked() network.SessionParticipant {
	taken := func(pick func(network.SessionParticipant) int, want int) bool {
		return slices.ContainsFunc(a.sessionRoster, func(p network.SessionParticipant) bool {
			return pick(p) == want
		})
	}
	var out network.SessionParticipant
	for id := 1; id <= parameter.MaxPlayers; id++ {
		if !taken(func(p network.SessionParticipant) int { return int(p.ID) }, id) {
			out.ID = network.PeerID(id)
			break
		}
	}
	for slot := range parameter.MaxPlayers {
		if !taken(func(p network.SessionParticipant) int { return int(p.Slot) }, slot) {
			out.Slot = uint8(slot)
			break
		}
	}
	return out
}

// releaseParticipant32 is the departure hook the network resource calls when a
// participant leaves, returning its identity so a later connection can take it.
func (a *App) releaseParticipant32(id uint32) { a.releaseParticipant(network.PeerID(id)) }

// releaseParticipant returns an identity whose handshake did not complete, or whose
// participant has left.
func (a *App) releaseParticipant(id network.PeerID) {
	if id == 0 || id == hostParticipantID {
		return
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessionRoster = slices.DeleteFunc(a.sessionRoster,
		func(p network.SessionParticipant) bool { return p.ID == id })
}

// offerLocked builds the offer addressed to one participant. Caller holds sessionMu,
// which is why the anchor is passed in rather than read here: JoinAnchor takes the
// world lock, and a departure released from under that lock takes sessionMu.
func (a *App) offerLocked(anchor event.JoinAnchor, assigned network.PeerID) network.SessionOffer {
	return network.SessionOffer{
		Anchor:            anchor,
		Host:              hostParticipantID,
		Assigned:          assigned,
		Participants:      slices.Clone(a.sessionRoster),
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
}

// hostOffer closes the lobby and returns the roster every participant builds from.
// Addressed to the host itself; each joiner receives the same roster with Assigned
// set to its own identity.
func (a *App) hostOffer() (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if len(a.sessionRoster) == 0 {
		// No joiner ever arrived; describe the two-participant lobby this host opened.
		a.sessionRoster = []network.SessionParticipant{
			{ID: hostParticipantID, Slot: 0}, {ID: 2, Slot: 1},
		}
	}
	assigned := hostParticipantID
	for _, p := range a.sessionRoster {
		if p.ID != hostParticipantID {
			assigned = p.ID
			break
		}
	}
	a.sessionOffer = a.offerLocked(anchor, assigned)
	return a.sessionOffer, a.sessionOffer.Validate()
}

// startHostSession holds tick zero until every offered remote participant is ready.
func (a *App) startHostSession(signals <-chan os.Signal) error {
	port, err := a.socketPort()
	if err != nil {
		return err
	}
	return a.startHostSessionOn(port, signals)
}

// startHostSessionOn runs the production gate against the supplied endpoint. The
// lobby closes only when every expected participant has arrived, because the roster
// it closes on is what every instance builds its cursors from.
func (a *App) startHostSessionOn(port *network.SocketPort, signals <-chan os.Signal) error {
	remoteCount := a.remoteParticipantCount()
	addr := a.cfg.HostAddress
	if bound := port.Addr(); bound != nil {
		addr = bound.String()
	}
	vlog.Info("app", "msg", "network host waiting", "address", addr, "participants", remoteCount+1)
	a.showStartupStatus(fmt.Sprintf("Hosting on %s; waiting for %d participant(s) (Ctrl-C cancels)",
		addr, remoteCount))

	if err := a.waitForStartup(port, signals, remoteCount, false,
		func() bool { return port.PeerCount() == remoteCount }); err != nil {
		return err
	}
	offer, err := a.hostOffer()
	if err != nil {
		return err
	}
	if len(offer.Participants)-1 != remoteCount {
		return fmt.Errorf("host admitted %d peers for a %d-peer offer", remoteCount, len(offer.Participants)-1)
	}
	if err := a.HostSession(offer); err != nil {
		return err
	}
	// Each joiner receives the closed roster addressed to itself. Sending the same
	// participant list to everyone is what makes shared creation order identical.
	for _, participant := range offer.Participants {
		if participant.ID == offer.Host {
			continue
		}
		addressed := offer
		addressed.Assigned = participant.ID
		body, err := json.Marshal(addressed)
		if err != nil {
			return err
		}
		if !port.Send(uint32(participant.ID), uint8(network.MsgStart), body) {
			return fmt.Errorf("host could not release participant %d", participant.ID)
		}
	}
	if err := a.waitForStartup(port, signals, remoteCount, true, func() bool {
		return port.PeerCount() == remoteCount && port.ReadyCount() == remoteCount
	}); err != nil {
		return err
	}
	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participants", len(offer.Participants)))
	return nil
}

// startJoinSession completes the tick-zero gate before the socket port owns the
// stream. The roster arrives with the gate, not with the offer: a joiner that
// dialled early saw only the participants ahead of it.
func (a *App) startJoinSession() error {
	if a.pendingJoin == nil {
		return errors.New("join session has no pending stream")
	}
	a.showStartupStatus("Join accepted; waiting for host start gate")
	final, err := a.pendingJoin.WaitStart()
	if err != nil {
		return fmt.Errorf("join start gate: %w", err)
	}
	a.sessionOffer = final
	if err := a.JoinSession(final); err != nil {
		return fmt.Errorf("join roster: %w", err)
	}
	if err := a.pendingJoin.Ready(); err != nil {
		return fmt.Errorf("join ready gate: %w", err)
	}
	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participants", len(final.Participants)))
	return nil
}

// waitForStartup treats rejected handshakes as recoverable while no peer was admitted.
func (a *App) waitForStartup(port *network.SocketPort, signals <-chan os.Signal,
	expectedPeers int, failOnDisconnect bool, ready func() bool) error {
	var events <-chan terminal.Event
	if a.termSvc != nil {
		events = a.termSvc.Events()
	}
	for !ready() {
		select {
		case <-signals:
			return errSessionCanceled
		case ev := <-events:
			switch ev.Type {
			case terminal.EventClosed, terminal.EventError:
				return errSessionCanceled
			case terminal.EventResize:
				a.handleResize(ev.Width, ev.Height)
			case terminal.EventKey:
				if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ {
					return errSessionCanceled
				}
			}
		case err := <-port.Errors():
			logSessionError(err)
			a.showStartupStatus("Join rejected: " + err.Error() + "; still waiting")
		case <-port.Changes():
			if failOnDisconnect && port.PeerCount() < expectedPeers {
				return errors.New("participant disconnected during startup")
			}
		}
	}
	return nil
}

// socketPort returns the concrete startup endpoint contributed by NetworkService.
func (a *App) socketPort() (*network.SocketPort, error) {
	if a.networkSvc == nil || a.networkSvc.Port() == nil {
		return nil, errors.New("network session has no socket port")
	}
	return a.networkSvc.Port(), nil
}

// activateNetworkSession closes the crossing window before terminal input is read.
func (a *App) activateNetworkSession() {
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if activator, ok := sys.(interface{ ActivateSession() }); ok {
				activator.ActivateSession()
			}
		}
	})
}

// showStartupStatus renders a frozen tick-zero lobby message.
func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, 0, false)
	if a.orchestrator != nil {
		a.frame()
	}
}

func logSessionError(err error) {
	if err != nil {
		vlog.Warn("app", "msg", "network session", "error", err.Error())
	}
}
