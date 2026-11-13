package membership

import (
	"fmt"
	"strings"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// openNearby makes this run's part in finding games on the local network: it
// announces while its player's switch is on and the run hosts at a door, and looks
// while the menu or :lan asks. A bot's run does neither, and a build with no
// socket has none.
func (m *Membership) openNearby() {
	if !config.SocketNetwork {
		return
	}
	cfg, seat := network.DiscoveryConfig(), m.wiring.Holder != 0
	cfg.Armed = func() bool { return !seat && m.ctx.Lan.Load() }
	cfg.Beacon = m.Announced
	cfg.Subnets, cfg.Listening = m.wiring.Subnets, m.wiring.Listening
	cfg.OnError = func(err error) {
		m.log.Warn("session", "msg", "local network not reached", "error", err.Error())
	}
	m.nearby = link.NewDiscovery(cfg)
}

// CloseNearby ends announcing and looking, and closes their socket.
func (m *Membership) CloseNearby() { m.nearby.Close() }

// announce has the discovery read at once what this run announces, where its
// player has it announce at all: the run began or stopped hosting.
func (m *Membership) announce() {
	if m.ctx.Lan.Load() {
		m.nearby.Wake()
	}
}

// Announced is the session this run announces to the local network now, nil while
// it hosts none at a door. It takes the world lock, once a beacon period on the
// discovery's goroutine and for no datagram.
func (m *Membership) Announced() *link.Beacon {
	var b *link.Beacon
	m.world.RunSafe(func() { b = m.announcedLocked() })
	return b
}

func (m *Membership) announcedLocked() *link.Beacon {
	switch m.sessionRoleLocked() {
	case roleHost, roleServer, roleSuccessor:
	default:
		return nil
	}
	port, _ := m.sessionTransportLocked().(*link.SocketPort)
	if port == nil {
		return nil
	}
	doors := link.BeaconDoors(port.Doors())
	if len(doors) == 0 {
		return nil
	}
	// Who is at the game now: a place a Wraith keeps is one a newcomer may be given.
	m.sessionMu.Lock()
	players := 0
	for _, p := range m.sessionRoster {
		if !p.Away && p.Slot != parameter.NoPlayerSlot {
			players++
		}
	}
	m.sessionMu.Unlock()
	if players == 0 && !m.run.Mode().Serves() {
		players = 1 // a lobby nobody reached yet holds its host
	}
	return &link.Beacon{
		Name:     m.cfg.SessionName,
		Scenario: link.BeaconText(m.scenario.Shown()),
		Players:  uint16(players),
		Ceiling:  uint16(m.playerCapacity()),
		Waiting:  m.ctx.TimeCtl.Holds(engine.HoldStart) || !m.lateJoins.Load(),
		Protocol: network.ProtocolVersion,
		Build:    link.BeaconText(asset.Build()),
		Doors:    doors,
	}
}

// Announce has the run read its player's switch again (GameContext.Lan).
func (c Control) Announce() { c.m.nearby.Wake() }

// Nearby lists the games found on the local network and keeps the run looking a
// while longer. One of another protocol is listed with why it cannot be joined.
func (c Control) Nearby() []engine.NearbySession {
	found := c.m.nearby.Look()
	out := make([]engine.NearbySession, len(found))
	for i, f := range found {
		out[i] = engine.NearbySession{
			ID: f.Nonce + "/" + f.Source.String(), Name: f.Name, Scenario: f.Scenario,
			Address: f.Address(), Target: f.Target(),
			Players: int(f.Players), Ceiling: int(f.Ceiling), Waiting: f.Waiting,
		}
		if f.Protocol != network.ProtocolVersion {
			out[i].Refusal = fmt.Sprintf("Another build, vif %s there and vif %s here", f.Build, asset.Build())
		}
	}
	return out
}

// ReportNearby says the games found on the status line: at once where the run was
// looking already, else once the hosts on the network had a moment to answer.
func (c Control) ReportNearby() {
	m := c.m
	if games := c.Nearby(); len(games) > 0 {
		m.ctx.SetStatusMessage(nearbyLine(games), parameter.StatusNotice)
		return
	}
	m.ctx.SetStatusMessage("Looking for games on this network...", parameter.StatusProgress)
	core.Go(func() {
		time.Sleep(parameter.NetworkLanAnswerWait) // [wall] the network's answer, not the game's
		m.ctx.SetStatusMessage(nearbyLine(c.Nearby()), parameter.StatusNotice)
	})
}

// nearbyShown is how many games the status line names; the menu lists every one.
const nearbyShown = 3

func nearbyLine(games []engine.NearbySession) string {
	if len(games) == 0 {
		return "No games found on this network"
	}
	var parts []string
	for _, g := range games[:min(len(games), nearbyShown)] {
		parts = append(parts, fmt.Sprintf("%s at %s (%d/%d)", g.Scenario, g.Address, g.Players, g.Ceiling))
	}
	line := "Nearby games: " + strings.Join(parts, ", ")
	if more := len(games) - nearbyShown; more > 0 {
		line += fmt.Sprintf(" and %d more", more)
	}
	return line + "; :join <address> or Menu / Multiplayer joins one"
}
