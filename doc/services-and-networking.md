# Services and Networking

Services own process/host resources whose lifecycle differs from ECS systems:
terminal raw mode, audio output, the immutable content corpus, and an optional
network transport. Networking is assembled for a trusted-peer session of up to
`parameter.MaxPlayers` participants; a normal run still contributes no active
network capability. The current failure model and recovery alternatives are
analysed in [Desynchronisation and recovery](desync.md).

## 1. Service lifecycle contract

```go
type Service interface {
    Name() string
    Dependencies() []string
    Init() error
    Start() error
    Stop() error
}
```

| Phase | Contract |
|---|---|
| Registration | Single-threaded composition; names must be unique. |
| `Init` | Acquire/configure resources but do not start background goroutines. |
| Resource binding | Optional `Contribute(*engine.Resource)` exposes a typed capability after every service initialized. |
| `Start` | Begin goroutines/process work after the whole application is assembled. |
| `Stop` | Idempotently stop work and release both Init- and Start-acquired resources. |

The separation prevents a service goroutine from observing a half-constructed
world. `Hub.StartAll` runs before the scheduler, input, and render loops; reverse
shutdown completes before process exit.

## 2. Dependency ordering and rollback

```mermaid
flowchart TD
    Register["register named services"] --> Sort["deterministic topological sort"]
    Sort --> Init["Init in dependency order"]
    Init --> Bind["bind typed resources"]
    Bind --> Start["Start in dependency order"]
    Start --> Stop["Stop initialized services in reverse order"]
```

The hub delegates to `core.TopoSort`, the same Kahn implementation the system
dependency graph resolves with. Names and dependent lists are sorted so
unrelated siblings have deterministic order; service init order feeds RNG
seeding, so it must not vary between runs. Missing dependencies and cycles are
startup errors, and the resolver reports them as typed errors the hub renders
in service terms.

If `Init` fails, already initialized services are stopped in reverse order. If
`Start` fails, already started services are rolled back; the app's final close
then stops the initialized superset. Normal `StopAll` visits every initialized
service in reverse order and logs individual errors without preventing later
cleanup.

All current services declare no dependency, so the registered subset initializes
alphabetically. The dependency mechanism exists for future capabilities that
actually require another service; do not invent dependencies merely to choose
an arbitrary order.

## 3. Current service inventory

| Service | `Init` | `Start` | Resource contribution |
|---|---|---|---|
| `terminal` | Construct terminal, enter raw/alternate-screen state, detect/force color mode. | Start blocking poll loop. | Exposed directly to app/render/input rather than as an ECS resource. |
| `content` | Resolve, load, sanitize, and freeze corpus/cursor. | No-op. | `ContentResource.Provider`. |
| `audio` | Build engine/config and load optional documents. | Freeze/register sounds, probe backend, start mixer/supervisor. | `AudioResource.Engine` when available. |
| `network` | If enabled, build transport/handlers. | Listen or connect. | `NetworkResource.Port`; absent in default disabled role. |

Assembly is mode-dependent:

| App mode | Registered services |
|---|---|
| `ModePlay` | terminal, content, audio, and network; `RoleNone` normally, `RoleHost`/`RolePeer` with startup flags |
| `ModeHeadless` | content only for ordinary harnesses; `app.RunScript` additionally registers `RoleHost`/`RolePeer` network when a startup flag is present |
| `ModeReplay` | terminal, content, and audio |
| `ModeScript` | terminal, content, audio, and the same `RoleHost`/`RolePeer` network a headless script registers |
| `ModeServer` | content and `RoleHost` network; no terminal and no audio |

The predicates in `internal/app/config.go` are authoritative for terminal and
audio capabilities. Network is role-selected separately: play always constructs
the no-op-capable adapter, while an authored headless script constructs it only
for `-host`/`-join`. Replay input controls playback rather than the mode router,
and its terminal resize affects presentation rather than recorded geometry.

Content and audio details are covered in
[Content, assets, and tools](content-assets-and-tools.md) and
[Audio architecture](audio.md).

## 4. Terminal service

The external `lixenwraith/terminal` module owns OS terminal capabilities. The
adapter in this repository owns application lifecycle:

- `Init` creates the terminal and calls its initialization exactly once;
- `Start` spawns one poll goroutine;
- terminal events enter a buffered 256-element channel;
- Stop closes the loop by posting a synthetic closed event, waits for the
  goroutine, and always finalizes the terminal—even when Init succeeded but
  Start did not;
- a panic in the poll loop performs an emergency terminal reset, flushes stdio,
  prints a stack, and exits to avoid leaving the shell unusable.

The polling channel applies backpressure rather than silently dropping normal
terminal events: if the app stops consuming, the poll loop waits until space or
shutdown. Terminal flush is performed by the render orchestrator outside the
world lock.

## 5. Capability injection

Services do not receive a `World` constructor argument. After `InitAll`, the hub
calls contributors in dependency order and fills only the capability fields
they own. `GameContext` then creates the remaining simulation resources.

```mermaid
flowchart LR
    Service["service-owned implementation"] --> Adapter["narrow engine resource interface"]
    Adapter --> Systems["systems consume capability"]
```

This direction prevents the I/O layer from directly mutating component stores.
Content exposes `NextBlock`; audio exposes the audio engine; networking exposes
a `NetworkPort` that drains notifications and sends opaque framed messages.

## 6. Operator session surface

`ModePlay` registers `NetworkService` on every run. With neither startup flag it
uses `RoleNone`, so Init/Start are no-ops and no `NetworkResource` is contributed.
`app.RunScript` can register the same active service without a terminal; ordinary
`NewHeadless` deliberately rejects address flags so no caller can bypass the
start/ready gate. Two flags activate the shared composition path:

| Entry point | Behavior |
|---|---|
| `-host <bind-address>` | Build the host App, start a listener, show or log the lobby, and hold the scheduler at tick zero until the requested peers are ready. |
| `-join <host:port>` | Dial and receive the anchor before App construction, adopt host identity, then take the world and the roster from the start gate. |
| `:host <addr>` | Open a run that is **already playing**. The port is created, started and attached; the world latches as shared (D-14) and the barrier takes ownership of this instance's crossings from that tick. |
| `:session` | Report the role, address, participant identity, peer count and tick. |

The flags and the command reach the same place. `-host` freezes tick zero for a
fixed lobby, which is the right shape when every participant is present before the
run starts; `:host` opens the same acceptor on a run that is hundreds of ticks in.
Both hand a joiner the same thing: the closed roster, then the world that roster
names, as a chunked `MsgStateSnapshot`. A host can be canceled in the lobby with
`Ctrl-C`/`Ctrl-Q`. After a connected peer leaves, the remote cursor is removed and
the survivor continues; the listener stays active and the same participant may
dial again, which is the reconnect path and is not a separate mechanism.

`:host` runs inside `App.handleIntent`'s critical section, because the whole
router path does — so `engine.SessionController`'s methods are the *locked* forms
and `App.BeginHosting` is the wrapper for a caller holding nothing. Getting that
backwards deadlocks the instance at the tick the command lands on, which is what it
did the first time it was wired through a script.

**The join, in order.** The ordering is D-22 and it is not the obvious one:

1. The acceptor allocates an identity and exchanges offer/reply.
2. The stream becomes a **peer** — so this instance's crossings start reaching it.
3. *Then* the world is read, under one acquisition of the world lock, and sent with
   the closed roster.
4. The joiner holds the session traffic that arrives while it reads, installs the
   capture into a staging world, swaps at a tick boundary, hands the held frames to
   its port, and simulates the ticks the transfer cost.
5. It confirms with `MsgReady`, and the coordinator crosses `EventParticipantJoined`
   so every instance creates its cursor at one agreed tick.

Reading the world before admitting the peer would lose every artifact produced in
between — not in the capture, because the capture predates it; not on the wire,
because the participant was not yet a peer. Nothing would detect that.

The join anchor owns the seed, RNG session, tick rate, config, corpus fingerprint
and D-14 map latch. `ConfigForJoin` runs before `New`, so `initWorld` cannot draw a
different seed. The coordinator assigns canonical participant IDs and roster
slots; both instances create the roster in slot order and mark only their own
cursor human-controlled.

The status bar shows `NET:WAIT/LOCK`, `NET:1P/LOCK`, or `NET:DOWN/LOCK`: the
D-14 latch is a property of the run, not of the current peer count, so a session
run keeps it from before its first joiner to after its last one leaves. `OPEN`
belongs to a solo run, which is the only one whose terminal may still crop.
The `network.session` debug card exposes state, peer count, connected state and
map latch separately.

## 7. Transport roles and lifecycle

| Role | Current behavior |
|---|---|
| `RoleNone` | Disabled/no-op. |
| `RoleServer` | Generic TCP/TLS listener. |
| `RoleHost` | Listener with `HostAcceptor`: admits the dialling address against a per-host budget, then allocates a participant identity and slot and offers the anchor. Each handshake runs on a goroutine of its own, bounded by `Config.MaxHandshakes`; `Config.OnAdmit` then runs the mid-run gate, which `App.midRunGate` serialises. |
| `RoleClient` | Generic dialer. |
| `RolePeer` | Dialed/preconnected stream admitted after the join and start gates. |
| `RoleRelay` | A *session* role rather than a transport mode: a participant with more than one link, which forwards the authority's artifacts to the participants behind it and retains what it forwards so it can answer their selective requests. The transport treats it exactly as `RolePeer` — it dials — because what makes it a relay is what it does with the artifacts, not how the stream was established. `network.SessionRole(authoring, links)` is what the protocol reads. |

```mermaid
flowchart TD
    Transport["Transport"] --> Listener["host accept loop"]
    Transport --> Dial["peer dial or preconnected stream"]
    Listener --> Manager["PeerManager"]
    Dial --> Manager
    Manager --> Peer["read, write and close goroutines"]
```

Host uses `tls.Listen` when a TLS config is supplied programmatically, otherwise
`net.Listen`; peer uses the corresponding dialer. The CLI deliberately supplies
no TLS configuration in this trusted-peer proof. Every admitted stream is keyed
by the coordinator's participant ID rather than accept order. The peer manager
enforces the configured cap and duplicate-ID rejection.

Each peer owns a bounded send queue, exact-frame read loop, encode/flush loop and
close monitor. `Stop` closes the listener, then the handshakes still in flight,
then the peers, and waits for all of them; peer close removes it from the manager
and emits one poll notification.

### 7.1 Admission

The handshake reads from a socket the far end controls, so it does not run on the
accept goroutine. One dialer that connected and never wrote used to hold every
other join for a whole `ConnectTimeout` — the cheapest denial there is, one socket
per five seconds of lobby. Each accepted connection now gets a goroutine, and
`Config.MaxHandshakes` bounds how many may be in flight at once; past the budget a
dial is refused rather than queued, because queueing would put the accept loop back
behind the same read. The budget is released the moment `AcceptSession` returns:
what it bounds is unauthenticated work, and everything after that point concerns a
peer the roster ceiling already bounds.

The acceptance a joiner sends back carries its own terminal-equivalent geometry
(`network.JoinerReport`). It travels with the reply rather than the dial because
only after construction does a joiner know the terminal it got, and it is advisory:
a coordinator that was given a `-size` ignores it, and one that was not uses the
first to size the session (see [Runtime](runtime.md) §1.2).

`Coordinator.Admit` runs before `Assign`, and it is the one decision made about a
dialer before it costs the session anything. The expensive part of a join is not
the handshake but what follows it: on a running host the admission reads, encodes
and sends a whole world, so a peer that joins and leaves in a loop spends one
connect per capture. `app.admissionLimiter` gives each dialling host
`parameter.NetworkAdmitBurst` admissions per `NetworkAdmitWindow`, keyed by address
rather than by identity — an identity is what the attack consumes and is released
the moment the connection drops. A refusal is written back as a `MsgJoinReply`
carrying its reason, for the same reason a mid-succession refusal is: a dialer that
can read why it was turned away backs off, where one that only sees the stream end
retries immediately.

The limiter's own table is bounded by `NetworkAdmitTracked` and swept when an
unseen host arrives. Past that ceiling it fails closed: a completed TCP handshake
proves the source address, so a table that wide is many real hosts at once rather
than one forging them.

## 8. Wire frame and session messages

Every message begins with a fixed 12-byte big-endian header:

| Byte range | Width | Field |
|---|---:|---|
| 0 | 1 | message type |
| 1 | 1 | flags |
| 2–5 | 4 | sender sequence |
| 6–9 | 4 | latest received sequence/ack |
| 10–11 | 2 | payload length |
| 12 onward | 0–65,535 | payload |

`Encode` completes short writes and `Decode` uses `io.ReadFull` for header and
payload, so a partial stream read never reaches the game. Per-peer send assigns
sequence and ack values at actual write time; broadcast clones a message per peer.
Ack is observational—there is no retransmission policy.

Control messages carry heartbeat, join offer/reply, start/ready gates and
disconnect notices. A join refused before an identity is allocated — a dial that
landed while the session was electing a new authority — is answered with a
`MsgJoinReply` carrying the reason rather than by closing the stream, so the dialer
can tell "retry against the new authority" from a connection that ended for no
stated reason (`network.IsHandoffRefusal`). Gameplay uses `MsgEvent` for a closed crossing
epoch, `MsgStateSync` for one owner-authored cursor snapshot, and
`MsgStateDigest` for the periodic shared-world parity probe. Authority travels as
`MsgStateSnapshot`/`MsgStateCorrection` chunks and, in the steady state, as the
`MsgStateManifest`/`MsgStateRequest`/`MsgStateShard` exchange below, with
`MsgStateUnserved` (0x2B) as the answer a retention holder gives to a request it
cannot produce pages for. `MsgAuthorityReport` (0x2C), `MsgAuthorityVote` (0x2D)
and `MsgAuthorityHandoff` (0x2E) carry the succession. The epoch is JSON
containing journal-registry TOML payloads; representative complete-frame budgets
are pinned by `TestWireEncodingBudget`. The remaining codes in `protocol.go` are
reserved placeholders that nothing sends and `NetworkSystem` counts as drops.
Raw participant input is not among them, and 0x10 stays reserved — a peer sends
the resolved D-3 artifact, never the keystroke that produced it.

`MsgLinkProbe` (0x14) and `MsgLinkEcho` (0x15) are the only round trip this
protocol makes. Everything else is one-directional — what this instance sent, and
how far behind the newest tick it has *heard about* it stands — which is why the
correction cadence had a constant and nothing else to be chosen from before
Phase 5. They are unusual in one way that matters: **neither ever reaches the
game**. `SocketPort` answers a probe from the read goroutine and swallows an echo
there, `MeshPort` does the same inside `Drain`, so what the measurement describes
is the wire rather than how often an instance runs a tick — and no timing value
has to enter the world for the cadence to exist.

| Frame | Bytes | Layout |
|---|---:|---|
| probe | 12 | `[Seq:4][SentNano:8]` |
| echo | 45 | the probe's 12 bytes untouched, then `[InBytes:8]` and a 25-byte `LinkReport` |
| `LinkReport` | 25 | `[Tick:8][LagTicks:4][Magnitude:4][CursorX:4][CursorY:4][Flags:1]` |

The probe's own timestamp comes back untouched, so the round trip is computed
against the clock that started it and neither end has to agree with the other
about what time it is. `InBytes` is what the answering end has received on this
link, which turns two consecutive echoes into a delivery rate and one echo into a
backlog — the difference between a fast link and an idle sender. The `LinkReport`
is the only game state that travels here, it is opaque to the transport, and every
field in it is a scheduling hint: a host may publish to that participant sooner
because of one, and a wrong or stale one costs a correction sent early and nothing
else.

A probe arriving during a join handshake is answered by the gate rather than
refused. A host admits a participant before it reads the world for it (D-22), so
the stream is a peer — and therefore probed — while the gate is still reading;
ignoring the probe would score the whole transfer as loss on the link it is
measuring, which is exactly backwards, since the transfer is the busiest that link
will ever be.

`MsgStateSnapshot` (0x26, the code the retired replay-based join reserved) carries
one chunk of a shared-world capture. It is the only message whose size is a
function of the world rather than of the format, so it is the only one that may be
split. The schema JSON first enters a 10-byte bounded compression envelope:
`[magic:4][version:1][codec:1][plain bytes:4]`, followed by deflate data. The
declared plain size and the reassembled wire size are both capped at
`MaxSnapshotBytes`. The current storm high water compresses from about 176 KiB to
15.4 KiB, but chunking remains required for larger or less compressible worlds.

That cap is a sanity bound rather than the defence. A declared length is a claim by
whoever sent the chunk, and `NetworkSystem` holds one `SnapshotAssembly` per
source: reserving for the declared total let one twenty-byte header make a receiver
hold the whole ceiling, once per participant. The reservation is now bounded by
`snapshotReserve` and the buffer grows with the bytes that actually arrive, so what
a peer can make a receiver hold is what that peer sends. Each
chunk carries a 20-byte header before its payload:

| Byte range | Width | Field |
|---|---:|---|
| 0–7 | 8 | the tick this capture describes |
| 8–11 | 4 | chunk index |
| 12–15 | 4 | chunk count |
| 16–19 | 4 | total body length |

`SnapshotAssembly` admits chunks in order only and refuses a skipped predecessor, a
frame that names a different transfer, a body that overruns its declared length and
a frame shorter than the header. Each of those would otherwise reassemble into a
world that looks installed and is not, which is worse than a refused join. The
gate's `SessionOffer` names `snapshot_tick` and `snapshot_bytes` before the
transfer starts, so a stream that stops halfway is a failed join rather than a
world installed from a prefix.

`MsgStateManifest` (0x28), `MsgStateRequest` (0x29) and `MsgStateShard` (0x2A) are
the Phase 6 selective exchange, and they are the only three messages in this
protocol that form a request/response pair carrying game state. A manifest is a
compact index over the same capture a correction would have carried — root, one
summary per section, and the capture header. A request is a receiver's answer to
one: either an acknowledgement that the roots agreed, or the page hashes of the
sections that did not. A shard set is the repair the second kind provokes.

None of the three is chunked. Each is bounded to one transport frame by
construction — `parameter.SnapshotShardBytesMax` caps a repair well inside the
65,535-byte payload field — and a repair too wide for one frame is not a repair:
the host answers it with a keyframe, which is chunked, self-sufficient and already
part of the protocol. All three use the same 10-byte compression envelope as a
capture, so the wire figures the cadence is priced from are compressed bytes.

A manifest travels one hop past the authority and no further. A participant with
more than one link forwards it to the participants behind it — but only for a tick
it can itself answer, so a question is never passed to someone whose neighbour
would have to refuse it — and their requests come back to that neighbour, which
serves them from its retention. A request and a repair are never forwarded: a relay
that does not hold the manifest a request names answers `MsgStateUnserved` rather
than routing the request onward, and the receiver degrades to the whole world the
keyframe cadence is flooding anyway.

Retention is what makes a relayed answer sound. Every instance keeps an index over
each authoritative capture it can *prove* it holds — a whole correction that
re-checked its own integrity hash, or a comparison that reproduced the authority's
root — bounded by `parameter.SnapshotManifestRetention`. A relay therefore never
holds a baseline of its own to serve from. The answer carries the authority's
header, root and section summaries and names the serving participant in `Served`;
a receiver refuses a set whose root is not the one its manifest declared, and then
re-derives that root from the repaired capture. Substitution, truncation and a
self-consistent set from another baseline each fail one of those two checks, by the
mechanism that already catches a corrupt wire.

The same ring is a successor's eligibility evidence, which is why the two Phase 7
deliverables are one primitive rather than two features.

### The authority term and the succession

Every authoritative artifact carries the term it was produced under: the session
offer, the capture header a correction and a keyframe travel in, the manifest, the
request and the shard set. A receiver ignores an artifact from a term it has left
(`network.term_stale`), acts on one from the term it holds, and *refuses* one from
a term it was never handed (`network.term_refused`) — a term is granted by a
handoff record and never adopted from an artifact, so an unheralded higher term is
a split brain to report rather than a fast successor to follow.

| Message | Payload | Sent by |
|---|---|---|
| `MsgAuthorityReport` (0x2C) | term, sender, the participant lost, the roster members it is directly linked to, and the newest authoritative tick it retains with how many records | every survivor, flooded and revisable |
| `MsgAuthorityVote` (0x2D) | term, voter, candidate | every survivor, once per term, never revised |
| `MsgAuthorityHandoff` (0x2E) | term, authority, predecessor, the voters it was elected on, the roster, the anchor, the barrier delay, and the newest tick the successor retains | the elected successor, before it publishes anything |

All three are flooded and deduplicated by content rather than by a hop count: a
report is idempotent, a vote is immutable and a handoff is adopted once. The flood
is also how a participant that never saw the disconnect learns of it, since the
departure crossing that would have carried that news has exactly one producer and
that producer is what went. The successor's first act under the new term is to
cross that departure itself.

Eligibility is two conditions and no timers: a candidate must be directly linked to
a strict majority of the closed roster, and must hold retention as new as the
newest any survivor reports. One vote per participant per term is what makes two
authorities in one term impossible rather than unlikely. A survivor that reaches no
majority elects nothing, continues locally, and says so.

The closed roster the succession counts against is read from the *world* rather
than from the offer that admitted this instance: a mid-run joiner is offered the
lobby as it stood when it dialled, so two participants admitted a minute apart hold
two different lists, while the cursor roster is barrier-bound and therefore the same
list everywhere.

The join gate holds these three frames for replay after the port takes the stream
over, because who is allowed to author is exactly what a joiner cannot re-derive.
It swallows the correction and selective kinds instead, for the reason it swallows
a correction chunk: they are questions about a world this stream's owner does not
hold yet.

## 9. Poll boundary and receive lead

Socket goroutines never touch `World`, the event queue or component stores.
`SocketPort` appends `Inbound` notifications to its configured bounded receive
channel (256 by default); a full buffer increments `Dropped` rather than blocking
the socket. `NetworkService` contributes that port through `NetworkResource`.

`NetworkSystem` is manifest-registered with a `dual` profile. At tick open,
`WireSink.Receive` drains notifications and applies scheduled local/peer artifacts;
at tick close, `Flush` sends the closed production epoch. For an ordinary D-3
crossing, `Cross` retains the peer copy but lets the producer publish its original
immediately; remote copies apply after the fixed receive lead. Arrival, departure
and full reset remain barrier-bound on their producer because they create or destroy
shared identity. The default lead is three 50 ms ticks and never waits for a
per-tick round trip.
An arriving epoch is admitted only inside a forward window. The schedule keeps what
is not yet due, so an apply tick beyond anything this run reaches is not a schedule
but a reservation nothing retires — and `epochWindow.admit` takes any tick above a
source's high-water mark, so one frame naming a large one would carry that mark
there and make every ordinary epoch that followed look late, on this instance and
on everything it relays to. The window is therefore checked before the epoch window
rather than after it, and it is `NetworkApplyWindowTicks`: the join catch-up
ceiling, which is the largest gap two participants legitimately hold at once, plus
one convergence floor for a peer running ahead. `NetworkScheduledMax` and
`NetworkScheduledBytes` bound what is held inside that window — a count for many
small artifacts and a byte total for few large ones, the same pair the replay
suffix uses. Both refusals are counted separately from a decode failure
(`network.artifacts_refused_tick`, `network.artifacts_schedule_full`), because a
frame this instance read and refused is a different diagnosis from one it could not
read.

The barrier is engaged for the life of a session run rather than while a peer is
attached: the tick an artifact applies at is what a replay or a mid-run catch-up
has to reach, and a reproduction holds no link. A journaled crossing is already
stamped past the lead, so a replay republishes it directly; the crossings the
simulation re-derives take the lead as the recorded run took it.
`ActivateSession` closes the lobby-to-first-tick input window before the main loop
reads terminal events. The game clock is frozen before FSM deadlines are created
and released only after the common start gate, so the host's lobby wait cannot age
shared timers before a later-created joiner reaches tick one.

An instance sends only to the peers it is linked to, so an epoch reaches the rest
of the session by being forwarded: each node floods an epoch it has not seen to
every link but the one it arrived on, and the per-source epoch window is what stops
the flood. Because every frame names the absolute tick it applies at, a relayed
artifact still lands on the same tick as the producer's own copy. A roster change
travels the same way, produced only by the coordinator so it has one apply tick.

`MsgStateSync` periodically copies only the D-13 owner-authored cursor set, and is
applied only when the payload's entity and roster slot agree and its sequence is
newer than that slot's last. Disconnect drains through the same poll boundary and
raises a local eight-second status message. While the coordinator remains
reachable, its roster crossing despawns only cursors owned by that participant,
releases that slot's sync sequence, and leaves both the barrier and the map latch
in place: a session's
playout lead and its bounds are properties of the run, so a stretch with no peer
attached still defers its crossings by the same lead and still keeps the bounds
every participant adopted. A departure is itself a crossing and lands at that lead
rather than where the lost link was observed.

At the same six-tick cadence, direct neighbours exchange a run/tick/hash sample
over the exact `SnapshotShared` comparison surface, plus category hashes that
identify position, kinetic, combat, context or status as the first differing
surface. A mismatch increments `network.digest_mismatches` and names the surface in
`network.drift_part` with `network.drift_tick`, and that is the whole of what it
does. It used to escalate — amber `DESYNC` after two consecutive disagreements, red
`DIVERGED` after five — and that was a statement about two instances re-deriving one
world from one artifact stream: a disagreement meant a lost artifact and nothing
re-derives one. Under an authoritative host a guest is *expected* to disagree
between corrections, so the verdict was retired with the failure state it described.
The digest does not flood, select an authority, repair state, or cross a partition;
what repairs a disagreement is the next correction. Losing the comparison edge is
still reported directly. A game guest that loses participant one continues its
local fork from the last authoritative state and keeps `HOST LOST:LOCAL` visible;
that is not coordinated election or state migration.

The host publishes its world as a whole capture and a delta against the last whole
one in between, chunked under `MsgStateCorrection` and reassembled per peer. A node
relays the chunks it admitted, on the same argument the epoch flood uses, so the
authority reaches a participant the host is not linked to directly. A guest queues
what arrives and installs the newest that resolves between two ticks, into a staging
world built once and re-used; the `snapshot.correction` group carries what it cost
and how far its prediction had drifted. Measured at the storm high water, the
schema's 29 KiB delta and 176 KiB keyframe compress to about 7.1 KiB and 15.4 KiB.
With one keyframe in ten that is about 39.6 KiB/s at the 5 Hz nominal cadence;
full compressed keyframes would be about 76.9 KiB/s.

**The cadence is chosen, not fixed.** `SnapshotCorrectionTicks` and
`SnapshotKeyframeCorrections` are the *nominal* operating point now; what a peer
actually receives comes from a bounded controller fed by that link's round trip,
its variation and the rate bytes are arriving at (D-24). Under pressure the
keyframe interval stretches before the cadence slows, because a keyframe costs
several times a delta and stretching it spends recovery time rather than
freshness. The session publishes on one timeline with one baseline — the base
cadence is the fastest peer's and the keyframe period the longest any peer
planned, capped by the floor — so a correction is still computed once and is still
exact; what is per peer is which corrections a peer is sent. A keyframe goes to
every peer whatever its cadence, since a guest that missed one refuses every delta
that follows it.

Both are bounded by `SnapshotFloorKeyframeTicks`, the convergence floor: the most
ticks a participant may go without a whole authoritative world. Adaptation may
never cross it. A link that cannot carry one whole world per floor window is
refused at admission — measured from the join's own transfer, which is the only
rate available before a probe has completed a round trip — and reported as an
unrecoverable operating condition mid-session, on the host from its estimate and
on the guest from `snapshot.cadence_keyframe_age_ticks`, which is whether one
actually arrived.

Beside them the authority surface says who is publishing at all.
`network.authority` groups the term this instance holds, the participant it
believes is authoring, how many handoffs it has adopted, how many artifacts the
term gate ignored or refused, and two run-level facts: `network.fork`, set when
this instance is a local continuation rather than part of a session, and
`network.migrating`, which draws a transient `MIGRATING` badge for
`NetworkMigrationBadgeTicks` after a handoff is adopted. `HOST LOST:LOCAL` remains
for the case where no succession is possible, and `:session` prints the term, the
authority, the handoff count and the fork state.

The relay's own surface is `snapshot.relay`: how many authoritative records this
instance is holding for a neighbour to ask about, how many repairs it answered from
them, how many requests it had to refuse, and the bytes it forwarded and served.
Those bytes are priced into *this* participant's link plan, never the authority's.

Three measurements are in the status bar now. `network.lag_ticks` is how far
behind the newest tick any peer has been seen closing this instance stands, taken
every tick rather than once at admission, with `network.stale` set past the playout
lead — the point at which this participant's own crossings reach the host after the
ticks they name. `snapshot.correction_entities` is how much of the world the last
correction moved. And the `snapshot.cadence` group with `network.link` beside it is
the operating point: the cadence in force, the ticks between whole worlds, the
round trip and its variation, the measured rate, and which of two conditions holds
— `cadence_constrained`, which is the design working, or `cadence_floor_breached`,
which is not. The bar draws them as `LNK` and `LINK!` for that reason, and
`:session` prints the whole set.

Loss that happens outside the barrier is published rather than swallowed, because
either direction would otherwise desynchronise silently:
`network.transport_lost_in` counts
inbound notifications a full poll buffer discarded, `network.transport_lost_out`
counts outbound frames a peer's bounded send queue refused. A new loss is logged
once as well as counted.

## 10. Timeouts, limits and security

`network.Config` applies connection/read/write deadlines, heartbeat and silent
disconnect intervals, read/write buffer sizes, bounded send/receive queues, peer
cap, barrier delay and optional TLS. Heartbeats are framed control messages and a
silent connection closes through the normal disconnect path. The defaults send a
heartbeat every 10 seconds and declare the peer silent after 30 seconds; clean EOF
is observed immediately.

What the operator surface still does not cover:

- `-join` dials one address, so the links a shipped binary builds form a star even
  though the relay makes any graph work. Per-peer correction cadence follows the
  same shape: it is a property of a direct link, and a participant reached by relay
  rides its neighbour's schedule for *corrections* while being answered by that
  neighbour's retention for *repairs*. It is also why the socket acceptance can
  only demonstrate the succession's fallback: a star's leaves reach one participant
  out of three, which is not a majority;
- the playout lead is a constant rather than a function of the graph's diameter,
  and a partition has no digest edge between its components. The *correction
  cadence* is measured and adaptive (D-24); the lead deliberately is not, because
  it decides the tick an artifact applies at;
- live pause/speed/step are refused, because a suspended participant has no way
  back into the running session;
- no lag compensation;
- trusted plaintext peers; no authentication or CLI TLS identity;
- host loss elects a successor when a strict majority of the closed roster is
  reachable, and creates an explicit independent local fork when it is not.
  Partition merging is still not built, and refusing to merge is what happens
  instead: a fork that meets a higher term refuses it and reports it;
- the succession is unauthenticated. A peer that lies about its links or its
  retention, votes twice, or fabricates a handoff record's voter list is not
  caught; the structural checks bound races rather than hostility;
- no cross-version compatibility negotiation beyond anchor schema/tick/config/
  corpus equality;
- sequence/ack fields detect ordering but do not retransmit, and a frame refused
  by a full send queue is counted, not resent.

## 11. Verification and manual run

`TestTwoLiveParticipantsConvergeOnCorrections` proves two independent drivers over
the in-process mesh; `TestTwoLiveParticipantsConvergeOverTCP` repeats 1,200
paired boundaries over `127.0.0.1`, uses the production startup gates, checks
disconnect continuation while the listener stays up.
`TestChainRelayReachesANonAdjacentParticipant` and
`TestMeshPropagatesEveryParticipantToEveryOther` cover relayed propagation,
`TestDepartureReachesTheWholeMesh` its membership half, and
`TestThreeParticipantLobbyClosesOnOneRoster` the socket handshake for a lobby
larger than a pair. `TestActivatedSessionDefersCrossingBeforeFirstTick` covers
input immediately after the lobby gate.
`TestRuntimeDigestReportsAndClearsSharedDivergence` deliberately corrupts and
restores shared state to cover the alert/clear path;
`TestSharedSnapshotExcludesLocalSchedulerTiming` keeps wall origins, tick slips
and display-only deadline remainder out of that surface. Framing, timeout,
mismatch and encoding budgets have focused tests in `internal/network` and
`internal/event`.
`TestCoordinatorLossRaisesLocalStatus` pins the direct guest warning that does not
depend on a surviving digest edge.

The adaptive cadence splits its verification in three, because the three questions
are different. `pkg/linkpace` holds the arithmetic and is tested on an explicit
clock the caller carries: the estimator's three claims, the controller's
degradation order and hysteresis, and — swept and then fuzzed — that no plan
leaves its declared bounds or the convergence floor. `internal/network` covers the
round trip itself, including that a probe and its echo are answered inside the
transport and never reach a game-side drain. `internal/app` covers the
composition over a deterministically shaped in-process link:
`TestAHealthyLinkStaysAtTheNominalOperatingPoint`,
`TestAConstrainedLinkSlowsTheCadenceAndSaysSo`,
`TestASlowPeerDoesNotSlowAFastOne`,
`TestCorrectionMagnitudeStaysBoundedOnAConstrainedLink`,
`TestAGuestRecoversAtTheFloorAfterTheLinkComesBack`,
`TestAJoinIsRefusedWhenTheLinkCannotCarryTheFloor` and
`TestLinkMeasurementNeverEntersTheComparedSurface`.

The in-process shape models when a frame becomes visible, whether it arrives, and
how many bytes a tick will pass — deterministically, with no clock. It does not
model a kernel queue, a retransmit or a send buffer that fills, which is where a
cadence chosen from a delivery-rate estimate is most likely to be wrong. So the
same claims are taken once over a real socket the kernel is shaping:
`TestStagedLinkShapingKeepsCorrectionsBoundedAndRecovers` walks four `tc netem`
stages — latency, jitter, loss, bandwidth — and requires bounded magnitude through
all of them and recovery when the qdisc is removed. It is opt-in behind
`VIF_NETEM=1` and needs root, because a qdisc on `lo` shapes every loopback flow
on the machine. `script/phase5-linkshape.sh` is the operator form of the same run,
driving the checked-in `script/phase5-host.toml` and `script/phase5-guest.toml`
pair and reading the answer out of both journals.

The authority term and the succession have their own set, and it is split the same
way for the same reason. The election rule is a pure function of a roster and a set
of reports, so `TestASuccessorWithStaleRetentionIsNotElected` and
`TestOneTermHasOneAuthority` take it apart directly: a candidate that is behind is
skipped, a minority elects nothing, and no split of five votes lets two candidates
reach a majority. The whole handoff is driven over the in-process mesh, where a
topology that is not a star can be expressed —
`TestSuccessionElectsOneParticipantOnEverySurvivor`,
`TestMembershipIsByteIdenticalAcrossAHandoff`,
`TestTheFirstCorrectionAfterAHandoffIsHashOnly` and
`TestMeshParityAcrossAHandoff`. The wire gate and the refusals are
`TestTheTermGateIgnoresTheOldAndRefusesTheUnheralded`,
`TestASecondHandoffForOneTermIsRefused` and
`TestALocalForkRejoiningAHigherTermIsRefused`; the admission half is
`TestAJoinerDiallingMidHandoffIsRefusedAndRetries`, over a real socket.

The relay role's are `TestARelayedParticipantKeepsTheSelectiveStream`,
`TestARelayCannotForgeAPage`, `TestARelayThatDroppedTheManifestSaysSo` and
`TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies`.

`script/phase7-migration.sh` is the operator form: three real processes over
sockets, the same `tc netem` stages, the coordinator killed mid-storm, and the
term, authority, migration and repair counters read out of all three logs. It says
which half it can prove — a star's leaves cannot reach a majority, so what it
demonstrates over sockets is the fallback and the no-two-authorities invariant, and
the elected-successor half belongs to the mesh suite.

The mid-run join has its own set. `TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun`
runs the whole thing over a socket — a solo run opens a port hundreds of ticks in,
a joiner installs the world it is sent, closes the tick gap the transfer opened and
takes its cursor from the arrival crossing — with the host ticking throughout, so
the gap is real rather than arranged away. `TestAReconnectIsTheSameJoin` runs it
twice against one host with a disconnect between.
`TestHostCommandRunsUnderTheWorldLock` types `:host` through the intent pipeline,
which is the only path that runs it where the runtime does, and is the regression
for the deadlock a direct call cannot see. `TestSnapshotChunksRoundTrip` and
`TestSnapshotAssemblyRefusesAConfusedTransfer` cover the chunk framing in
`internal/network`.

```bash
# terminal 1
./bin/vif -d -host 127.0.0.1:7777

# terminal 2
./bin/vif -join 127.0.0.1:7777
```

Both sides should reach `NET:1P/LOCK`, display two cursors and agree on shared
actors, scoring and progression while both participants move/type/fire; a healthy
run shows a small `COR` and never `LAG`. Give the two terminals different sizes and
resize one mid-run: the map must not move and neither side may fall behind. The host's
`:new` resets both rosters and a guest's is refused. Quit the guest; the host must
show a participant-disconnected message and continue at `NET:DOWN/LOCK`. Quit the
host; with only one guest left there is no majority to elect from, so it must say
that it is continuing locally, retain `HOST LOST:LOCAL`, and keep ticking at
`NET:DOWN/LOCK` — `:session` then names the term it still holds and that it is a
local fork. Restarting the host and rejoining is a *new* session, not a merge: an
old fork left running would refuse its artifacts and say so. Add `-players <n>` to a
startup host for a larger closed lobby, or to a solo launch as the cap a later
`:host` inherits; without a solo cap, later hosting uses `MaxPlayers`. Bind the
host to `:7777` for a LAN. Internet routing uses the same TCP
path but is not safe for untrusted peers until authentication and TLS
configuration are exposed.

Use an unmodified loadout when checking for duplicated presentation. `:god` is an
intentional stress mode: it sets the invoking cursor's energy to 100 billion,
grants rod, launcher and disruptor, and the default auto-fire then drives the main
cleaners, specials and every ready weapon about four times per second. Those
entities and effects are the invoking peer's Player domain; they are deliberately
visible there and are not evidence that a correction copied another participant's
visual world.

To open a session on a run already in progress, type `:host :7777` instead of
passing the flag; the guest dials it the same way. `:session` on either side
reports what it is part of.

For a repeatable no-terminal run, combine the flags with the paired authored
scripts in `script/phase3-host.toml` and `script/phase3-guest.toml`. Their manual
clocks are wall-paced after the ready gate so socket delivery observes the same
tick cadence as play mode. `script/phase4-host.toml` and `script/phase4-guest.toml`
are the mid-run pair: the host half takes **no** flag, runs flat out to tick 400,
opens hosting there with `:host`, and is wall-paced from that point — pacing is a
property of the run rather than of the flags, so a script that opens a session
starts keeping step with its peer the moment it has one. See
[Development](development.md) for the exact commands and script schema.

## 12. The supervised run's endpoint

`-probe <address>` binds `internal/probe`, a stdlib HTTP server carrying three
paths. It exists because a dedicated host has no screen: what a person reads from
a status bar an orchestrator has to read over a socket, and the periodic session
summary is inside a process nothing can reach.

| Path | 200 when | 503 when |
|---|---|---|
| `/healthz` | the tick counter is advancing, the clock has not started yet, or the run is paused | the scheduler is running, unpaused, and the tick has not moved in `parameter.ProbeStallInterval` |
| `/readyz` | a dial would be admitted — live, not in the lobby's closing window, and the roster is below `sessionCapacity()` | the run is not live, the lobby is closing, or the session is at capacity |
| `/metrics` | always; renders the status registry in the Prometheus text format | — |

The two probes answer different questions on purpose. A run that is not live
should be restarted; one that is merely full should stop being sent participants,
and a Service that kept routing to it would be sending them to a roster with no
room — which they would discover only after the connect and a slot allocation.
Readiness therefore includes a lobby that has not started: being dialled is what
it is waiting for.

Liveness is sampled across reads rather than measured inside one, because a probe
cannot wait for a tick. Each read compares against the last, and a pause resets the
window rather than accumulating under it: pause is an operator state, not a fault.

The server binds before the lobby wait, so a run that is starting answers rather
than refusing connections for the whole window in which it is starting.

`/metrics` renames registry keys onto the Prometheus grammar — `vif_` plus the key
with every character outside `[A-Za-z0-9_]` replaced by an underscore — and reports
every value as a gauge, because the counters among them are monotone only within a
run and a reset re-bases them. String cells are omitted: they are states whose
natural exposition is a label set this does not model. Nothing is instrumented for
it; it is a rendering of `internal/status`, unchanged.

## 13. Adding a service

- Give it a stable unique name and declare only real dependencies.
- Keep `Init` goroutine-free and make `Stop` safe after partial initialization.
- Do not expose a concrete I/O implementation when a narrow resource interface
  suffices.
- Ensure service callbacks never mutate the ECS directly; drain at a controlled
  world-owned boundary.
- Bound all channels/queues and define overflow/backpressure policy.
- Roll back subprocesses, terminal modes, files, listeners, and goroutines on
  every error path.
- Add lifecycle tests for Init failure, Start failure, duplicate Stop, and
  shutdown with blocked I/O.

## 14. Source map

| Concern | Primary source |
|---|---|
| Service contract/hub | `internal/service/interface.go`, `hub.go` |
| Mode-to-service policy | `internal/app/config.go`, `app.go` |
| Terminal adapter | `internal/service/adapter_terminal.go` |
| Content/audio adapters | `internal/service/adapter_content.go`, `adapter_audio.go` |
| Network adapter | `internal/service/adapter_network.go` |
| Transport/protocol | `internal/network/*.go` |
| Tick-owned ECS wire bridge | `internal/system/network.go` |
| Startup session assembly | `internal/app/session.go`, `app.go`, `loop.go` |
