# Authentication and session design

A design for future implementation: who a vif player is, what admits a dial to a
session, how a participant comes back after a dropped link without authenticating
again, and how a host and its participants prove themselves to each other. None of
it is built. It collects the open work that needs it (§3), what `lixenwraith/auth`
offers (§4) and how `lixenwraith/chess` uses it (§5), sets a preliminary plan (§7)
and what it asks of the router and the deployment (§8) against the questions still
to decide (§9); §10 catalogs the files an implementation reads and changes. As each
part lands it moves into the reference documents ([Multiplayer](multi-player.md),
[Services and networking](services-and-networking.md),
[The session fleet](kubernetes-fleet.md),
[Deploying the session fleet](kube-docker-deploy.md)) and leaves this one.

## 1. Words

*Session* keeps its vif senses ([term index](term-index.md#s)), above all one world
timeline under one authority. The words of signing in are kept apart from it:

- An **account** is a lasting player identity an identity service holds: an id, a
  name and a credential.
- A **sign-in** is an account's proof on one device, a record the service can
  revoke. chess calls it a session.
- An **admission ticket** is a short-lived capability for one session: signed,
  naming the session, the kind of admission (play, watch, create, publish) and whom
  it admits, an account or nobody in particular.
- A **participant key** is an Ed25519 key pair a participant makes for its place,
  whose public half the roster carries.
- A **session pass** is a session authority's signed statement that a participant
  key was admitted, and as what. A redial shows it, with its key's signature, in
  place of authenticating again.
- A **passphrase door** is a door that admits a dial only after an Argon2-SCRAM
  exchange over a passphrase its host set.

## 2. What authenticates today

Nothing proves who a peer is: links are plaintext and unauthenticated by decision
([Multiplayer §8](multi-player.md#deferred-by-decision)), and the fleet hardens the
open port instead ([fleet §4](kubernetes-fleet.md#4-security-posture)). What each
surface checks:

- **The site's API**, `GET` and `POST /vif/api/sessions` (`tool/vif-router`
  `handleSessions`): no credential. A create is budgeted per address
  (`-client-creates` a `-first-join` window, keyed on `-client-address-header`).
  `network.RequestSession` already shows a refusal's message, a 401 or 403 among
  them.
- **The browser route**, `wss://<site>/vif/ws/<session>` (`handleSessionSocket`):
  the method, the id's syntax, the upgrade and an exact `Origin` (`-web-origin`).
  TLS ends at the site's edge; the stream is then placed as the front door places
  one, on a pod or on a published door.
- **The front door**, TCP 7777 (`tcpRouter.place`): the first frame,
  `MsgSessionRoute`, places the connection by the session name, a routing key every
  link carries in clear; `-client-joins` a minute per address and `-route-max`
  connections a session. Plaintext.
- **A session's own NodePort** (31700–31709): the pod's `AdmissionLimiter`, 6 joins
  a minute per address (an IPv6 /64), 1024 addresses tracked, and `MaxHandshakes`
  (8) in flight. A routed dial reaches it from the node, so it budgets only the
  direct route.
- **A player's doors**, `-host` and `-serve` at tcp, ws and wss. A wss door serves
  `-cert`/`-key` or a `-selfsign` certificate; a dialer trusts the system's roots, a
  self-signed certificate as it is, or what `-ca` names: an issuer's PEM, or
  `sha256:<hex>` pinning the leaf, which the host prints and `:session` reads
  (`network.TrustTLS`). A ws door checks no `Origin`.
- **A relay door published through the router** (`-relay vif://<site>:7777`):
  `PublishRequest` names the session, and its `Resume` token, a bearer secret, keeps
  the door's name for `NetworkTunnelResumeWindow` (10 min) after a lost control
  connection; `-doors-per-address`, and the join budget per publication. The run
  takes each dial over a connection it opens back to the router, plaintext as its
  control connection is.
- **The join handshake** (`network.HostAcceptor`): route, budget
  (`Coordinator.Admit`), claims (`MsgJoinClaims`), preview and seat, `Assign`, the
  offer, then the joiner's `PeerIdentity`, a build and session match
  ([Services §7.2](services-and-networking.md#72-join-identity-and-who-decides-it)).
  It identifies a binary and a world, never a person.
- **A claim** (`network.NewClaimToken`): 32 random bytes whose SHA-256 the roster
  carries; a dial names the token in clear to the coordinator, through its relay if
  it has one. A page keeps its claims in the tab's `sessionStorage`; a terminal and
  Android keep them for the process's life, Android wiring no `ClaimStore`. A claim
  keeps a place from another player for `WraithTimeout` (2 min of game time), and is
  no credential against the wire ([Multiplayer §5.4](multi-player.md#54-wraiths)).
- **A peer link** (`network.PeerAcceptor`, `converge.Reach.admitPeerLink`):
  `MsgConnect` names its sender's participant identity, and the acceptor admits any
  roster member but itself. Identities run 1 to 17, so whoever reaches a
  participant's port can open a link as any participant it holds no link to: a
  Wraith, the lost host during a succession, any pair not linked yet
  (`PeerManager.AddConnectionAs` refuses only a second link). The link's crossings
  then pass `crossingActors` as that participant's, and its notices can take the
  succession ([Services §10](services-and-networking.md#10-timeouts-limits-and-security)).
  In a player's session every participant that advertises a port takes them
  (`-authority migrate`, the default without `-serve`); a fleet pod runs
  `-authority host`.
- `MsgAuthRequest` (0x30) and `MsgAuthResponse` (0x31) are reserved: nothing sends
  them, and `NetworkSystem` counts one as a drop.

Confidentiality exists only on TLS, the browser route as far as the site's edge and
a player's wss doors. On vif:// and ws, a claim, a resume token and every frame can
be read and replayed by anyone on the path.

## 3. The open work this serves

From [the todo list](todo.md), each with what it needs here.

Authentication itself:

- [Add browser admission authentication](todo.md#add-browser-admission-authentication)
  (P2): a short-lived, session-scoped admission credential, issued after
  authentication and consumed in the handshake, kept out of page history and logs;
  creation taking one too; whether the router stays the credential boundary; a
  terminal's door authenticated by Argon2-SCRAM bound to its leaf certificate. Its
  prerequisite, every route carrying real play without transport faults, applies to
  all of §7. Met by §7.2, §7.4 and §7.5.
- [Publish a player's session at the router](todo.md#publish-a-players-session-at-the-router)
  (P3): a credential in the publish frame, and accounting keyed by that identity
  rather than the address. Met by §7.5 and §8.

Waiting on an identity:

- [Budget a fleet pod's joins per player](todo.md#budget-a-fleet-pods-joins-per-player)
  (P3): the router naming the player to the pod, which a ticket's subject does.
- [Name participants beyond their slot](todo.md#name-participants-beyond-their-slot)
  (P3): a roster field, a capture schema change and a trust decision. A name the
  coordinator copies from a verified ticket is attested; one a peer chose is not
  (Q15).
- [Let an authority drop a participant](todo.md#let-an-authority-drop-a-participant)
  (P2): keeping a dropped player out takes an identity, an account or a participant
  key, and its keep-out list is what refuses that player's passes (§7.3); the
  operator route on the router is an operator's (Q16).
- [Watch a session as a spectator](todo.md#watch-a-session-as-a-spectator) (P2): a
  second kind of cursorless entry, which a ticket's and a pass's kind tells from a
  player.
- [Offer lixen.com games on the start screen](todo.md#offer-lixencom-games-on-the-start-screen)
  (P1): hosting and listing at the site. It builds on the API as it stands, and
  takes a sign-in once the site has accounts.

Session membership whose trust overlaps:

- [Hear a participant again once its link reopens](todo.md#hear-a-participant-again-once-its-link-reopens)
  (P1): the fix clears a departure mark when the link reopens, a link any dial
  naming that participant opens today; with participant keys (§7.1) only the
  participant can.
- [Keep a Wraith's stats in the session](todo.md#keep-a-wraiths-stats-in-the-session)
  (P2): the session takes a returning run's word for its record. A key proves the
  run is the one that left, never that its record is true.
- [Merge two sessions](todo.md#merge-two-sessions) and
  [Split a session under a second authority](todo.md#split-a-session-under-a-second-authority)
  (P3): the redirect, "rejoin at this link", is an order participants obey, so it is
  signed by the term's authority (§7.1), and a participant's pass is good at the
  door it names only once that door's authority admits it.
- [Find games on the local network](todo.md#find-games-on-the-local-network) (P2):
  a beacon is unauthenticated by nature; a passphrase door (§7.4) authenticates the
  host a beacon names.

Adjacent, and out of scope: [Fetch a content-addressed bundle over HTTP](todo.md#fetch-a-content-addressed-bundle-over-http)
(P1, a publisher's signature from another origin),
[Cache a received scenario to the user root](todo.md#cache-a-received-scenario-to-the-user-root)
(P3, bytes from a peer), [Promote verified images to the node](todo.md#promote-verified-images-to-the-node)
(P2, a registry's pull credential) and
[Sign the Android build with a release key](todo.md#sign-the-android-build-with-a-release-key)
(P2). They sign content and builds, not people; a publisher's key can reuse
§7.1's Ed25519 code.

## 4. `lixenwraith/auth`

v0.5.1: one package over `golang-jwt/jwt/v5` and `golang.org/x/crypto`, neither of
which vif's `go.mod` names yet. What it offers:

- **Argon2id PHC** (`HashPassword`, `VerifyPassword`, `ValidatePHCHashFormat`,
  `MigrateFromPHC`): 64 MiB, 3 passes, 4 lanes and a 16-byte salt by default. Every
  entry refuses work past 256 MiB, 16 passes or 786432 KiB-passes, and a password
  past 1024 bytes. A server running it pays 64 MiB a login.
- **Argon2-SCRAM**: the package's own JSON challenge and response, not SASL's
  SCRAM-SHA-256. `NewCredential` provisions in one KDF pass and `Export` /
  `ImportCredential` persist the result. `ScramServer` holds the credentials and at
  most 4096 single-use handshakes of 30 s; `NewScramServerWithDecoyKey` keeps an
  unknown user's decoy salt stable across restarts, and `Stop` ends it.
  `ScramClient` refuses a cost below 64 MiB and 3 passes unless its own policy says
  otherwise (`WithMinArgonCost`). The client runs the KDF; the server only HMACs.
  `WithChannelBinding` (1–64 bytes at both ends) commits the proof to the TLS
  channel, such as the SHA-256 of the server's leaf certificate, and a bound and an
  unbound peer never agree. Only a verified server final authenticates, and its
  `Username` is the identity. The four messages are JSON structs, as a vif handshake
  frame's payload is.
- **JWT**: `NewJWT` (HS256, a key of 32 bytes or more), `NewJWTRSA` and
  `NewJWTVerifier` with their PEM forms (RS256, 2048 bits or more). `sub`, `exp`,
  `iat` and `nbf`, issuer and audience pinned, the application's claims nested
  under `extra`. Its defaults, a 24 h lifetime and 5 min of leeway, are a caller's
  to override. One key a manager: no `kid`, no `jti` of its own, and no EdDSA,
  though golang-jwt v5 carries it (Q7).
- **Bearer tokens**: `ParseBearerToken` (RFC 6750 syntax), and
  `SimpleTokenValidator`, an in-memory allowlist keyed by SHA-256, with no expiry.

Its README already treats vif, under its old name vi-fighter, as a JWT audience
beside logwisp, with `lixen-auth` the issuer of its example: the shape §7.5 takes.
It cites `doc/security-audit.md`, which its repository does not hold.

## 5. How `lixenwraith/chess` uses it

chess v0.16.3 requires `lixenwraith/auth` v0.5.1 for its hashing, its JWT and its
bearer parsing. It uses no SCRAM, no channel binding, no RS256 and no
`SimpleTokenValidator`: its clients send the password over HTTPS and the server
hashes it. The todo's channel-bound SCRAM "as `lixenwraith/chess` authenticates"
is ahead of chess's code: vif would be auth's first SCRAM consumer.

The calls:

- `service.New`: one `auth.NewJWT` manager, HS256, issuer `chess-server`, audience
  `chess-api`, a 7-day lifetime (`SessionTTL`), and no leeway, since one process
  mints and verifies.
- `service/user.go`: `auth.HashPassword` and `auth.VerifyPassword` behind four KDF
  slots (`MaxConcurrentKDF`, about 256 MiB), which a caller waits 5 s for before a
  503 (`ErrAuthBusy`); an unknown name is verified against a dummy hash
  (`dummyPasswordHash`), so it costs what a wrong password does.
  `GenerateUserToken` mints `{sub: user, extra: {session_id}}`, and `ValidateToken`
  checks the JWT, then that the stored sign-in binds that subject to that session
  and has not expired.
- `http/middleware.go`: `auth.ParseBearerToken`; `AuthRequired`, and
  `OptionalAuth`, which lets an absent header through but refuses a malformed or
  invalid one rather than downgrading the request to anonymous.
- `service/integrity.go` and `cmd/chess-server/cli`: `auth.ValidatePHCHashFormat`
  over stored hashes and on `db user set-hash`. The CLI's `db user` verbs (`add`,
  `delete`, `set-password`, `set-hash`, `set-email`, `set-username`, `reset-link`,
  `list`) write the database directly.

Around them:

- Sign-ins: one row an account (`sessions`, unique on `user_id`), so a login on one
  device ends the other's. Logout and account deletion delete it, and an hourly
  sweep drops expired ones. A token is a signed pointer to that row, read on every
  request: a store-backed sign-in with a signature on top.
- Limits per client address: registration 5 a minute, login 10, recovery 3, reset
  10, account deletion 5. `-trusted-proxies` is parsed strictly before a proxy's
  header is believed, and `-max-users` caps public registration.
- The key: `-jwt-secret-file`, a regular file of 32–4096 bytes readable by its
  owner alone; a fixed key in dev mode; otherwise a random key a process, so
  sign-ins end at a restart.
- Recovery: a 32-byte token mailed as a link (`?reset=<token>`), its SHA-256 stored,
  one an account, for an hour.
- Backups: a nightly `pg_dump` (`chess-backup.timer`), kept 14 days.
- Games: the account that creates a game or makes a side's first move claims that
  side (`Game.ClaimSlot`); a claimed side refuses anonymous moves, and anonymous
  play takes the rest.
- Clients: the web client keeps the token in `localStorage`, the CLI client in
  memory.

What vif takes: the KDF bound and the dummy credential wherever a server checks a
password, a sign-in record that logout revokes, an issuer and an audience on every
token, optional authentication that refuses a malformed credential, strict key-file
checks, an operator CLI over the store, a backup of it, and anonymous play beside
accounts. What it does otherwise, and why:

- several sign-ins an account, one a device, since one player plays from a
  terminal, a phone and a page;
- a page's sign-in in an `HttpOnly` cookie its script never reads, where
  `localStorage` is readable by any script on the origin;
- a public-key signature on tickets, so pods and player hosts verify one without
  the key that mints one;
- no recovery token in a URL, which history and logs keep;
- nothing read from a store on the game path: a ticket lives a minute and is spent
  once, and a session pass is checked against the session's own keys.

## 6. Requirements

- Anonymous play stays possible: an account adds to what a player can do, and its
  absence refuses nothing by default (Q1).
- A dropped link authenticates nothing again: a redial within the session proves
  its participant key, with no prompt, no KDF and no request to the site (§7.1,
  §7.3). Starting the game again on a signed-in device asks for no password (Q6).
- Least privilege: a ticket admits one kind of admission to one session for about
  a minute; a pass admits one key to one session. Pods and player hosts hold no key
  that mints a ticket; the router holds the ticket key and verifies no password.
- No credential in a URL, a command line, a log line, page history, a capture or a
  journal. The handshake is outside the journal, and the JSONL log records a
  refusal without its credential.
- One flow and one vocabulary on every host: `:login`, `:logout` and a Multiplayer
  page section on a terminal, the window, Android and a page, a host's lack reading
  `unavailable in this build` (AGENTS.md, Platforms).
- One mechanism per problem: participant keys replace claim tokens rather than sit
  beside them; the per-address budgets stay for anonymous dials.
- The standard library first (`crypto/ed25519`, `crypto/tls`, `crypto/rand`), and
  `lixenwraith/auth` for Argon2id, SCRAM and JWT. No cgo: FreeBSD, Linux, Android
  and js/wasm alike.
- A credential travels only on TLS (Q10). On plaintext, authentication stops a
  stranger off the path and nobody on it.

The threats, and the part that meets each:

1. A stranger who reaches a participant's port and names a participant identity
   (§2): §7.1.
2. A machine in the middle of a self-signed door: §7.4.
3. A stranger in a session whose link leaked, and a dropped player coming back:
   §7.5, and keep-out by key or account (§7.3).
4. Guessing and enumerating accounts, and exhausting a server's KDF memory: §7.5's
   limits.
5. A compromised pod, which runs vif over a stranger's bytes: it holds a ticket
   verifier and its own session's signer, never the router's key.
6. A pass read off a plaintext link, or out of a page's storage without its key:
   useless alone, since every use signs a fresh nonce (§7.3).

## 7. Preliminary plan

Five parts, in this order once the todo's prerequisite holds. §7.1 and §7.2 land as
one change, since a key's proof rides the exchange; each later part lands alone.
Each bumps `ProtocolVersion`; §7.1 also bumps `snapshot.Schema`, and with the
arrival's payload `event.JournalSchema`.

The tokens the plan ends with, one a scope:

- **Sign-in** (§7.5): an account on one device. Opaque, 32 random bytes whose
  SHA-256 the identity service stores; it lasts 30 days and renews past half its
  life (Q6). Only the identity service checks one, when the router asks it for an
  account's ticket. Sign-out or a disabled account ends it.
- **Admission ticket** (§7.5): one admission to one session. A JWT the router
  signs; about a minute, spent once; checked by the session's door. It expires.
- **Session pass** (§7.3): that admission, for every redial. A token the session's
  authority signs, naming the participant's key; it lasts the session, capped
  (Q9), and does nothing without the key's signature. A drop and the session's end
  revoke it.

The participant key is no token: it is what the pass names and every redial
proves.

### 7.1 Participant keys

A participant makes an Ed25519 key pair for its place, and its first admission
request (§7.2) carries the public half with a signature over the door's nonce. The
coordinator records it on the roster entry (`RosterEntry.Key`); the arrival crosses
it, so every instance and every capture holds it. Then:

- A peer link is a challenge: the acceptor answers `MsgConnect` with a nonce, the
  dialer signs the session, both identities and the nonce, and the acceptor
  verifies the signature against the roster, as the dialer does the acceptor's. A
  name alone opens nothing.
- Succession notices and handoff records (`MsgAuthorityReport`,
  `MsgAuthorityHandoff`), which are flooded and forwarded, carry their author's
  signature, so a forwarded one is still its author's. A handoff record names each
  term's authority key, which a pass is checked against (§7.3). A redirect, for a
  merge or a split, is signed by the term's authority.
- A claim becomes possession of the key: an away entry keeps the key, and a
  returning dial signs the door's nonce instead of naming a token, so the door
  seats it in the place that key holds, whatever gate the door keeps: a key on the
  roster was admitted once. The token, `ClaimDigest` and `MsgJoinClaims` go; the
  keeper holds private keys where it held tokens, and a relay that carries a dial
  can no longer take its place.

Cost: 32 bytes a roster entry, a round trip a peer link, and a signature of tens of
microseconds a link and a succession message, nothing a tick. It needs no
infrastructure and no new dependency. It neither authenticates a host to a new
joiner, which holds no key for it yet, nor protects a plaintext stream from
someone on its path.

### 7.2 The admission exchange

The reserved kinds carry admission: 0x31 what the door asks and answers, 0x30 what
the dialer sends. The door asks first, after the route frame and the address
budget, in place of today's claims step, so an open door pays no round trip it
does not pay now:

```
dialer                                door
MsgSessionRoute                 -->
                                <--   MsgAuthResponse{methods, nonce}
MsgAuthRequest{pass, proof}     -->   a returning participant: done
MsgAuthRequest{ticket, proof}   -->   a fleet join: done
MsgAuthRequest{key, proof}      -->   a new joiner at an open door: done
MsgAuthRequest{scram first}     -->   a passphrase door:
                                <--   MsgAuthResponse{scram challenge}
MsgAuthRequest{scram proof}     -->
                                <--   MsgAuthResponse{scram final, or a refusal}
                                <--   the preview, the seat and the offer as today
```

A proof is the participant key's signature over the nonce, the session's name and,
on a player's wss door, the SHA-256 of its leaf certificate; a new joiner's request
carries the public key it proves. A pass names its key and a key names its place,
so nothing is left to claim. The front door reads none of it: it places by the
route frame and splices the rest as today. A refusal is a `RefusalCode`, one new
code for authentication required and one for refused, each with its retry, and a
page shows its reason as it shows every refusal. Both frames are bounded, a few
KiB, and join the handshake's fuzz targets. A relay asks the authority for the
nonce it hands a dialer, so a proof it carries is good once.

### 7.3 Session passes

§7.1 already brings a participant back to its own place with one signature while
the roster keeps the place, a Wraith's 2 minutes. A pass carries the admission past
that and past the door that made it, so a phone the system suspended for ten
minutes, a laptop that slept or a page reopened in its tab comes back without its
gate: no passphrase prompt and no KDF, no ticket from the site.

- **Issued** at every admission: the offer carries it, signed by the authority with
  its own participant key (a fleet pod's is the session key it makes at start).
  It names the session, the first term's authority key (so a later session under
  the same name refuses it), the participant's public key, the kind, the account
  when a ticket named one, the term and an expiry (Q9). Its form is Q8.
- **Checked** at any door of the session: the authority, a successor, which holds
  every term's authority key from the handoff records, and a relay, which checks it
  before it carries the dial on. The door verifies the pass against that term's
  key, its expiry and the session's keep-out list, then the proof against the key
  it names: one round trip and two signature checks, about a hundred microseconds
  on a desktop core.
- **Placed** by its key: back in the place the key holds, or a new place once the
  Wraith was dismissed. A redial past half the pass's life takes a fresh one in its
  offer.
- **Revoked** by a drop, which puts the key, and the account when the pass names
  one, on a keep-out list that crosses as shared state, and by the session's end,
  when no door answers for it. A sign-out stops new tickets and leaves a pass to the
  session's rule.
- **Kept** where claims are kept now, the private key beside it: a page's in the
  tab's `sessionStorage`, readable by any script on the origin as its claims are;
  a terminal's for the process, which tmux and ssh drops leave running. Android
  wires a store in the app's private files, since the system ends a backgrounded
  process and its claims with it today. `ClaimsKept` bounds them as it does claims.

A pass stolen alone is nothing: every use signs a fresh nonce with the key it
names. A pass and its key stolen together, from a page's storage or a device, are
the participant until the session drops it or ends.

### 7.4 Passphrase doors

`-passphrase` and `:host` take a passphrase or make one (Q12), which `:session`
shows. The host provisions one SCRAM credential (`auth.NewCredential`, once) and
serves a `ScramServer` at every door; a joiner asks its player for the passphrase
when the door says so, and verifies the server final before it reads an offer. On
a wss door both ends bind the exchange to the SHA-256 of the leaf certificate, the
digest `network.Fingerprint` prints: a self-signed door is then authenticated both
ways, the gap the todo names, and `-ca` stays for a pin without a passphrase. On
tcp and ws the exchange authenticates the handshake alone. The pass it ends with
spares the joiner the passphrase on every redial (§7.3).

A machine in the middle that answers for the door receives a proof it can test
passphrases against offline, at Argon2's cost a guess: a chosen phrase must be
long, and a generated one of about 80 bits is past guessing. A door published
through the router is reached in plaintext past the site's edge, so its exchange
runs unbound there (§8). The KDF runs on the joiner, 64 MiB once a join, a page's
and a phone's too: measure it there before the cost is chosen.

### 7.5 Admission tickets and accounts

The router mints admission tickets: JWTs through `lixenwraith/auth`, signed by a
key the router alone holds (EdDSA or RS256, Q7), the site as issuer and
`vif-session` as audience, about a minute long, with `extra` naming the session, the
kind (play, watch, create, publish), a ticket id and, for an account, its id and
name. `POST /vif/api/sessions` returns one for the session it makes, and a new
`POST /vif/api/sessions/<id>/tickets` one for a session that exists. A page fetches
it from its own origin and hands it to the join in memory; a native client asks the
site its link came from (`linkSites`). The pod verifies it with the public key its
Job's arguments carry, holds its session to the pod's own name, and spends its id
once, keeping spent ids for a ticket's life. A player's host may verify the site's
tickets too, from the public key at `GET /vif/api/keys`, for a session open to the
site's accounts alone. A ticket admits once; the pass it is exchanged for admits
every redial after.

Anonymous tickets come first. Their subject is the dial's address, so the router
names the player to the pod (the per-player budget), and a pod run with
`-admit ticket` refuses a new admission that went around the router. Accounts
follow, held by an identity service apart from the router (Q2, Q3), with a sign-in
a device: an `HttpOnly`, `Secure`, `SameSite=Strict` cookie for a page; a file of
mode 0600 under the user's state directory for a terminal, refused when it is
wider; and the app's private storage on Android. A sign-in renews past half its
life, so a player back within a month never types a password again (Q6). From a
sign-in the router mints a ticket naming the account. Then:

- the router budgets creates, joins and doors by account, and a publish frame
  carries a publish ticket;
- the coordinator copies the account's id and name onto the roster entry and the
  pass: an attested label, and keep-out by account for a drop;
- a sign-out or a disabled account stops new tickets; a ticket in flight lives out
  its minute, and a pass the session's rule.

The identity service takes what chess learned (§5): limits per address and per
account, a KDF bound, a decoy for an unknown name (SCRAM's stable decoy salts), no
answer that tells whether an account exists, and an operator CLI over its store.

## 8. What it asks of vif-router and the deployment

By part:

- **Keys, the exchange and passes** (§7.1–§7.3): nothing. The router places a dial
  by its route frame and splices the rest unread, so the door's question and the
  dialer's pass cross it as bytes; the front door, the browser route and the
  NodePort forward stay as they are. A router update already waits for an empty
  fleet (`update.sh`), so a fleet player never redials across one. It does drop
  published doors' tunnels, and their players come back through the resumed
  publication with their passes. The publication's `Resume` token is then the last
  bearer secret on the wire: a publish can carry the run's public key instead, and a
  resume sign the router's nonce.
- **Passphrase doors** (§7.4): nothing, with one limit. A published door's dials
  cross the router and its tunnel in plaintext, the browser route's past the site's
  edge, so its SCRAM runs unbound, and the router and anyone on either leg can test
  passphrases against a proof offline. A generated code (Q12) is past that; a
  chosen phrase is not.
- **Anonymous tickets** (§7.5): the router's first secret, and the pods' first
  verifier.
  - The key is a file in `/etc/vif-router`, `root:vif-router` 0640 as `router.env`
    is, made once on the node by `update-vif-router.sh` when absent (a `keygen`
    verb of the router's) and named by path in `vif-router.env`, so the repository
    holds the path and never the key. It needs no backup: a lost key is a new one,
    and only the tickets open at the time die with it.
  - Rotation rides a router update, which waits for an empty fleet, so no pod holds
    the old public key. `GET /vif/api/keys` lists the current and the previous key
    for player hosts that verify the site's tickets.
  - The verifier: the router writes its public key into each Job's arguments
    (`manifest.go`, with `30-session.yaml` and `render-session.sh` in step) beside
    `-admit ticket`. A public key is no secret, so it takes no Secret, ConfigMap or
    mount, no change to the router's Role (`40-router-rbac.yaml`) and no egress,
    which the session's network policy keeps closed. A session made by hand
    (`session.sh`) runs open unless given the key.
  - The API: the create answers with its creator's ticket beside the three
    targets, and the tickets route mints for a session that exists, charged to the
    join budget the router keeps now. The listing stays public: a listed id admits
    only a dial holding a ticket, which the router budgets. The router serves exact
    paths today, so both routes join its mux.
  - The pod's budget: the ticket's subject names the player, where today every
    routed dial reaches the pod from the node.
- **Accounts** (§7.5): a new node service, and the node's first lasting player
  data.
  - The identity service runs as a system user of its own with its own unit and
    state directory, bound to loopback. The router forwards `/vif/api/auth/` to it
    byte for byte, as it forwards `/vif/api/logs` to LogWisp (`log_proxy.go`), and
    asks it, on a second loopback listener that forward never reaches, which
    account a sign-in names before it mints that account's ticket: one loopback
    request a ticket. No new public port and no filter change.
  - `update.sh` gains it as a component. Its restart ends no session and pauses
    only sign-ins and account tickets, so it updates without the empty-fleet gate,
    as `filter`, `objects` and `wad` do.
  - Its store needs a backup and a restore, as chess's has, and a way to delete an
    account and what it holds.
  - Its limits key on the client address the router forwards, believed from the
    router's loopback address alone, as chess believes `-trusted-proxies`.
  - A page's sign-in is a cookie scoped to `/vif/api/`, and the router mints from a
    cookie only on a request whose `Origin` is `-web-origin`, the check its browser
    route already makes. A native client sends its sign-in as a bearer header, which
    no other site's page can make a browser send.
  - The public log stream carries the sessions' logs, which then name a participant
    by slot, never by account; the identity service's logs stay off it, and hold no
    credential.
  - The operator routes, dropping a participant and draining, take a loopback
    listener of the router's own (Q16).

What the site's edge then provides, beside the requirements of
[Deploying §12](kube-docker-deploy.md#12-the-sites-edge):

- publish `POST /vif/api/sessions/<id>/tickets`, `GET /vif/api/keys` and
  `/vif/api/auth/` to 9080, and nothing else new;
- pass `Cookie` and `Authorization` through on those paths untouched, and keep both
  out of its access log;
- cache nothing under `/vif/api/auth/`.

Once tickets land, D6 in [Deploying §0](kube-docker-deploy.md#0-decisions) and
[The session fleet §4](kubernetes-fleet.md#4-security-posture) change: a fleet
admission takes a ticket, and only the native transports stay unauthenticated on
the wire. A ticket costs the router one signature a join and the pod one check
(Ed25519 tens of microseconds, RS256 signing about a millisecond: orders of
magnitude, not readings), nothing a tick.

## 9. Questions to decide

Each gives its options with what follows from them, then a leaning.

**Q1. Anonymous play on the fleet.**
- Open, as today: nothing changes for a player, and a leaked link admits anyone.
- An anonymous ticket for every new admission: a request to the site before each
  join, so a native `vif://` link needs its site; the router names every player to
  its pod, and the NodePort route no longer goes around its budget. Redials show
  passes and ask the site nothing.
- An account to create, anyone to join: the fleet's capacity is spent by players
  the site knows, and a newcomer still joins a friend's link.

Leaning: anonymous tickets first, accounts optional, and an account required only
for what needs a lasting name.

**Q2. The credential boundary.**
- The router mints and alone verifies: pods trust a spliced stream, so the public
  NodePort forward has to close, every native join goes through the front door,
  and the front door parses the exchange.
- The router mints and pods verify: a public key in every Job's arguments, the
  NodePort route stays, and the router reads nothing past the route frame.
- A separate admission service: the router stays a router, at the cost of another
  process on the node and another hop before every join.

Leaning: the router mints and pods verify; the router budgets by account where it
mints, so the front door never reads a ticket.

**Q3. Where accounts live.**
- Nowhere yet: tickets, participant keys, passes and passphrase doors cover the P1
  and P2 items; names, keep-out by account and accounting wait.
- A vif identity service: one more process and store on the node, its keys apart
  from the router's.
- The router: one process, but the narrow boundary grows a store and password
  handling, which a compromised router then exposes.
- One site-wide service shared with chess, the `lixen-auth` issuer auth's README
  sketches: one account for both games; chess takes a verifier and an audience, and
  its users move or the service reads them.

Leaning: nowhere until a feature needs a lasting name, then a service of its own,
built as the site's so chess can verify it later.

**Q4. The account store.**
- A file the identity service owns (one document, atomic rename): no database to
  run, enough for hundreds of accounts, backed up as a file.
- PostgreSQL, as chess runs it: concurrency and migrations, and a database to
  operate on the node.
- SQLite: cgo, or a large pure-Go module, which the no-cgo rule leaves out.

Leaning: the file, until a site-wide service shares chess's database.

**Q5. How a sign-in proves its password.**
- chess's way, the password over HTTPS and Argon2id on the server: any page can
  sign in, the site's lobby included; the server pays 64 MiB a login behind its KDF
  slots.
- Argon2-SCRAM: the password never leaves the device and the server only HMACs;
  registration sends the exported credential. The client pays the KDF, and the
  site's own script cannot sign in without the wasm build or an Argon2id of its
  own.
- No password, a device key as the account: nothing to guess or recover, and a
  lost device is a lost account unless a second one was enrolled.

Leaning: SCRAM, with sign-in in the game (Q14), once the KDF is measured on a page
and a phone.

**Q6. A sign-in's form and life.**
- Form: an opaque token the service stores the digest of, checked on each ticket
  (one loopback request, and a sign-out takes effect at once); or a JWT the router
  verifies alone (no request, and a sign-out waits out its life, or a revocation
  list, which is the store again). chess's is the first with a signature on top.
- Reach: one an account, as chess has (a login on the phone ends the terminal's),
  or one a device, listed and revoked one by one.
- Life: fixed, as chess's 7 days, then the password again; or sliding, renewed past
  half its life and ended after 30 idle days.

Leaning: opaque, one a device, sliding over 30 days.

**Q7. A ticket's form.**
- An RS256 JWT through auth as it is: a 256-byte signature keeps a ticket under
  1 KiB; a verifier holds a public key of about 400 bytes; signing costs the router
  about a millisecond.
- An EdDSA JWT, through a small addition to auth (golang-jwt v5 has the method): a
  64-byte signature, a 32-byte public key in a Job's arguments, and one key type
  with participant keys and passes.
- An opaque random token in the router's memory (`SimpleTokenValidator`, with an
  expiry added): nothing to sign, but only the router checks one, so Q2's first
  option.

Leaning: EdDSA, added to auth before tickets land; RS256 if auth stays as it is.

**Q8. A session pass's form.**
- An EdDSA JWT through auth, signed with the authority's participant key: one form
  with tickets, and the JWT code a pod's ticket check links anyway (Q18); about
  400 bytes.
- vif's own, an Ed25519 signature over a fixed struct in the frame's JSON: about
  200 bytes, the standard library alone, and a second token format to keep.
- None, the roster as the record: a returning key proves itself against its entry
  (§7.1), with nothing to sign or carry, and nothing past the roster's memory: a
  player back after its Wraith was dismissed passes its gate again.
- A bearer token whose digest the roster keeps, today's claim made an admission:
  read and replayed on plaintext, and gone with the entry.

Leaning: an EdDSA JWT, if auth takes EdDSA (Q7); vif's own otherwise.

**Q9. How long a session pass lasts.**
- The place's life: it ends as the Wraith is dismissed, and then it adds nothing to
  §7.1 (Q8's third option).
- The session's, capped at a day: a player away longer than a Wraith rejoins in a
  new place without the gate; a fleet session's hour caps it sooner.
- A short term renewed at each redial, an hour say: an idle pass dies on its own,
  and a session holding a player's attention keeps it fresh.

Leaning: the session's, capped at a day, renewed past half its life.

**Q10. What carries a credential natively.**
- The front door as it is, plaintext: a single-use ticket and a nonce-bound proof
  stop a stranger off the path, and anyone on it can take the stream after them.
- The wss route for any native join that carries a ticket or a passphrase: TLS to
  the site's edge, and one more hop. A pass's proof needs no secrecy and keeps the
  front door.
- TLS on the front door, terminated by the router: the router then holds the site's
  certificate, and a published door's tunnel can ride it too.

Leaning: the wss route, and TLS on the front door only if that hop shows in the
readings.

**Q11. A ticket on the browser route.**
- In band (§7.2): one path for every host, and no ambient authority on the
  upgrade.
- The sign-in cookie on the upgrade: no ticket to fetch, but the upgrade then
  carries ambient authority only the `Origin` check guards, and native clients need
  another path.

Leaning: in band.

**Q12. A passphrase door's secret.**
- A code the host generates and shows: past offline guessing, passed on beside the
  link.
- A phrase the host chooses: memorable, and guessable by a machine in the middle,
  the router included for a published door.
- The code in the link's fragment: one thing to paste, but the link becomes a
  secret, and every place a link is logged or shown has to hide it.

Leaning: generated by default, chosen when asked, never in the link.

**Q13. A participant key's life.**
- One a place, as a claim is: no identity across sessions.
- One a device, kept and reused: a lasting pseudonym, giving names and keep-out
  without accounts, and a key that follows a player from session to session.
- Deferred, signing peer links alone and keeping bearer claims.

Leaning: one a place now, a device's revisited with Q3.

**Q14. Where a player signs in.**
- In the game on every host: `:login <site>` and a Multiplayer page section.
- The site's lobby page alone.
- Both.

Leaning: in the game, the lobby reading the same cookie.

**Q15. Names on the roster.**
- An account's name alone, attested by the coordinator.
- Also a name an anonymous player chooses, shown as unverified.
- None for an anonymous player, who keeps the slot's label.

Leaning: an account's name; an anonymous player keeps the slot's label.

**Q16. Operator authority on the router (drop, drain).**
- A loopback listener the node's operator reaches over ssh: no credential, nothing
  reachable from outside.
- An operator role on accounts: reachable from anywhere, so a stolen sign-in is an
  operator.
- None: the deploy scripts alone.

Leaning: loopback.

**Q17. Recovery.**
- By email: a mail relay the deployment lacks, and addresses kept.
- By an operator's reset on the node, chess's `db user set-password`.
- None: a new account, all a device-key account (Q5) could have anyway.

Leaning: an operator's reset, no email.

**Q18. The dependency.**
- Import auth as it is: golang-jwt and x/crypto join vif's and the Android module's
  `go.mod`, and the game binary links the JWT code.
- Split auth into SCRAM, JWT and Argon2id packages first: the game links what it
  uses, at a breaking change for chess.
- The standard library alone in the game, auth in the services: no SCRAM in the
  game, so no passphrase doors, and passes in vif's own form.

Leaning: import it as it is, and read the native, wasm and Android binary sizes
before and after.

## 10. File catalog

What an implementation reads and changes, by area. Paths are from each
repository's root.

The session layer, `internal/network`:

- `protocol.go`: the kinds, the reserved `MsgAuthRequest` and `MsgAuthResponse`,
  `MsgSessionRoute`, `MsgJoinClaims`, `MsgConnect`/`MsgAck`, the tunnel's kinds.
- `session.go`: `RosterEntry`, `SessionOffer`, `JoinerReport`, `Coordinator`,
  `HostAcceptor`, `readSessionName`, `askClaims`, `NewClaimToken`, `ClaimDigest`,
  `RefusalCode` and its table, `openJoinStream`, `answerClaims`,
  `AdmissionLimiter`.
- `reach.go`: `PeerAcceptor`, `PeerDialer`, `peerHello`, `peerWelcome`,
  `SuccessionChain`.
- `authority.go`: `AuthorityReport`, `HandoffRecord`, which §7.1 signs and §7.3
  reads term keys from.
- `identity.go`: `ProtocolVersion`, `PeerIdentity`.
- `tunnel.go`: `PublishRequest`, `Published`, `TunnelHub`, `DoorTunnel`, the resume
  token.
- `config.go`: `Config.Claims`, `Config.Identity`, `RequestSession`.
- `fuzz_test.go`: the handshake fuzz targets the new frames join.

The link, `pkg/network`:

- `endpoint.go`: schemes, `LoadTLS`, `SelfSignedTLS`, `Fingerprint`, `dialTLS`,
  `TrustTLS`, `RequestSession`, `ListedLinks`, `linkSites`.
- `transport.go` and `config.go`: the accept loop, `MaxHandshakes`,
  `ConnectTimeout`.
- `connection.go`: `PeerManager.AddConnectionAs`, `ErrLinkStands`.
- `websocket_wasm.go`: a page's socket, which can set no header.

The protocol and the run:

- `internal/converge/reach.go`: `Reach.admitPeerLink`.
- `internal/converge/authority.go`: `onReport`, `takeBack`, where reports and
  handoffs are flooded and forwarded.
- `internal/system/network.go`: `crossingActors`, `admissibleFromSource`,
  `stateInRange`.
- `internal/event/payload.go`: `ParticipantJoinedPayload`, which would carry a
  participant's key.
- `internal/snapshot/capture.go`: `Schema`.

Membership, `internal/app/internal/membership`:

- `claim.go`: `ClaimKeeper`, which would keep keys and passes.
- `identity.go`: `buildIdentity`, `joinerReport`.
- `session.go`: the coordinator's `HostAcceptor` wiring, `claimedLocked`,
  `closeStaleLink`, `yieldLocked`, `redialSeat`.
- `relay.go`: a relay's acceptor, and the claims it carries to the authority.
- `converge.go`: `redial`, `redialDoor`, where a pass is shown.
- `host.go`, `join.go` and `seat.go`: hosting, dialling, and a seat's claims.
- `internal/app/internal/config/membership.go`: `Membership`, `ClaimStore`.

Parameters, paths and commands:

- `internal/parameter/network.go`: `ClaimsKept`, `ClaimStoreInterval`,
  `WraithTimeout`, `NetworkAdmitWindow`, `NetworkAdmitBurst`,
  `NetworkAdmitTracked`, `NetworkTunnelResumeWindow`,
  `NetworkSessionRequestTimeout`; `internal/parameter/player.go`: `MaxPlayers`.
- `internal/paths/paths.go`: `UserRoot` and the state directory a native sign-in
  would be kept under.
- `internal/mode/commands.go`, `config_menu.go` and `config_form.go`: `:host`,
  `:join`, `:session`, the Multiplayer page and its request form, where `:login`
  and a passphrase prompt would go.
- `cmd/vif/main.go` and `usage.go`: the `-host`, `-serve`, `-join`, `-name`,
  `-cert`, `-key`, `-selfsign`, `-ca`, `-relay` and `-authority` flags and the help
  table the manual is made from; `cmd/vif/browser.go`: a page's `ClaimStore`.

Hosts:

- `web/terminal.js` and `web/worker.js`: the tab's claims in `sessionStorage`, the
  launch arguments.
- `android/game/game.go`, and `StartScreen.java` and `MainActivity.java` under
  `android/app/src/main/java/com/lixen/vif`: the Android run, which keeps no claims
  past its process.

The router, `tool/vif-router`:

- `server.go`: the mux, `/vif/api/sessions`, the create budget, `clientAddr`.
- `ws_proxy.go`: the browser route and its `Origin` check.
- `tcp_proxy.go`: the front door's placement, `placeDoor`, the publication of a
  door.
- `log_proxy.go`: the loopback forward an identity service's routes would copy.
- `config.go`: the router's flags.
- `manifest.go`: the Job a pod runs, whose arguments would carry the ticket
  verifier.
- `kube.go`: the router's own credential, a ServiceAccount token file.
- `README.md`: the router's API.

The deployment:

- `deploy/guest/vif-router.env`, `update-vif-router.sh` and `vif-router.service`:
  the router's settings, which hold no secret, its update behind the empty-fleet
  gate, and where a key file would be made.
- `deploy/update.sh`: the components, and which of them wait for an empty fleet.
- `deploy/k3s/30-session.yaml`, `render-session.sh` and `session.sh`: the
  session's Job as a template, and a session made by hand.
- `deploy/k3s/20-networkpolicy.yaml` and `40-router-rbac.yaml`: what reaches a
  pod, that it reaches nothing, and what the router may do.
- `deploy/website/vif.nginx.example`: the reference edge §8's new requirements
  extend.
- Deploying the session fleet §0 (D6), §2, §11 and §12.

Documents that state today's posture: Multiplayer §1, §5.4, §5.6 and §8; Services
and networking §7.1, §7.2 and §10; The session fleet §4 and §9; Build profiles and
platforms, Where WebSocket is spoken, The public route and §6; Domain design §7.

`lixenwraith/auth`: `scram.go`, `argon2.go`, `jwt.go`, `token.go`, `error.go`,
`example_test.go` (a whole SCRAM exchange) and `README.md` (limits and
deployment).

`lixenwraith/chess`, the reference consumer:

- `internal/server/service/service.go` and `user.go`: the JWT manager, the KDF
  slots, sign-in and validation.
- `internal/server/service/reset.go`: recovery tokens.
- `internal/server/http/middleware.go`, `auth.go` and `handler.go`: bearer
  parsing, the account routes and their limits.
- `internal/server/storage/session.go` and `schema.go`: the sign-in table and its
  binding to a subject.
- `cmd/chess-server/secret.go`, `proxy.go` and `cli/cli.go`: the key file, trusted
  proxies, the operator CLI.
- `deploy/linux/chess-backup.timer` and `chess-backup.service`: the store's backup.
- `internal/server/game/game.go` and `internal/server/processor/processor.go`: a
  game's slot claims.
- `doc/architecture.md` and `doc/api.md`: its design and its account API.
