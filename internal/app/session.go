package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

const (
	hostParticipantID network.PeerID = 1
	joinParticipantID network.PeerID = 2
)

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
	netCfg.MaxPeers = 1
	netCfg.OnError = logSessionError
	netCfg.AcceptSession = network.HostAcceptor(a.hostOffer, netCfg.ConnectTimeout)
	return netCfg
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
	if err := a.JoinSession(offer); err != nil {
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

// hostOffer snapshots the coordinator position for one startup handshake.
func (a *App) hostOffer() (network.SessionOffer, error) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessionOffer = network.SessionOffer{
		Anchor:   a.JoinAnchor(),
		Host:     hostParticipantID,
		Assigned: joinParticipantID,
		Participants: []network.SessionParticipant{
			{ID: hostParticipantID, Slot: 0},
			{ID: joinParticipantID, Slot: 1},
		},
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
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

// startHostSessionOn runs the production gate against the supplied endpoint.
func (a *App) startHostSessionOn(port *network.SocketPort, signals <-chan os.Signal) error {
	const remoteCount = 1
	addr := a.cfg.HostAddress
	if bound := port.Addr(); bound != nil {
		addr = bound.String()
	}
	vlog.Info("app", "msg", "network host waiting", "address", addr, "participants", remoteCount+1)
	a.showStartupStatus("Hosting on " + addr + "; waiting for participant (Ctrl-C cancels)")

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
	for _, participant := range offer.Participants {
		if participant.ID == offer.Host {
			continue
		}
		if !port.Send(uint32(participant.ID), uint8(network.MsgStart), nil) {
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

// startJoinSession completes the tick-zero gate before the socket port owns the stream.
func (a *App) startJoinSession() error {
	if a.pendingJoin == nil {
		return errors.New("join session has no pending stream")
	}
	a.showStartupStatus("Join accepted; waiting for host start gate")
	if err := a.pendingJoin.WaitStart(); err != nil {
		return fmt.Errorf("join start gate: %w", err)
	}
	if err := a.pendingJoin.Ready(); err != nil {
		return fmt.Errorf("join ready gate: %w", err)
	}
	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participants", len(a.sessionOffer.Participants)))
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
