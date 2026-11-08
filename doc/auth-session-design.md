# Authentication and session design

Who a vif player is, what admits a dial to a session, how a participant comes back
after a dropped link, a crash or a reboot without authenticating again, and how a
host and its participants prove themselves to each other. The decisions are settled
(§0); what they ask of the code is a plan in phases, each a PR (§7); what is built is
the admission primitives in `internal/network/admission.go` (§7.0), which nothing
sends yet. §2 states what authenticates today, §3 the open work this serves, §4 and
§5 what `lixenwraith/auth`, `lixenwraith/logwisp` and `lixenwraith/chess` give and
what vif takes, §6 the requirements and threats, §8 what the plan asks of the router
and the deployment, §9 the router's budget for ten thousand routed players, §10 the
questions still to decide and §11 the files an implementation reads and changes. As
each phase lands its part moves into the reference documents ([Multiplayer](multi-player.md),
[Services and networking](services-and-networking.md),
[The session fleet](kubernetes-fleet.md),
[Deploying the session fleet](kube-docker-deploy.md)) and leaves this one.

## 0. Decisions

- **D1. An identity made at start, a place kept past the run.** A run makes an
  Ed25519 key pair for each place it takes, as it takes it, the way a logwisp
  listener gets a certificate on a key made at start: ephemeral, and shown to the
  session alone. The key is the participant's identity on the wire, anonymous and
  identifiable at once: the roster carries its public half, every redial proves it,
  and nothing links it to another session or to a person. Its pass (D13) and its
  private half are kept past the process by default, a page's in the tab's storage,
  a terminal's in a 0600 file under the user's state directory, Android's in the
  app's private files, so a crash, a restart or a reboot brings the player back to
  its Wraith's place, at the same host or a successor's, with one signature and no
  prompt. `[play] keep_places = false` keeps nothing past the process.
- **D2. Keys and passes replace claims.** One mechanism: the claim token, its
  digest, `MsgJoinClaims` and `ClaimKeeper`'s tokens go; the keeper holds keys and
  passes where it held tokens, and the per-address budgets stay for anonymous dials.
- **D3. The direct NodePort route goes.** Every fleet dial crosses the router, by
  the front door or the browser route, so one placement, one budget and one
  admission apply to all of them. `direct_target`, `port`, the NodePort Service, the
  31700–31709 forward and `-port-first`/`-port-last` go; the fleet's size is
  `-sessions-max` beside the quota, and the pod's game port is reachable from the
  node alone. The pod's own per-address limiter then budgets a player's doors alone;
  a fleet pod budgets by a ticket's subject.
- **D4. TLS on the front door.** The native fleet link is `vifs://<site>:7777/<id>`.
  TLS ends at the site's edge, which hands the router each connection under PROXY
  protocol v2 so the dialer's address still keys its budgets; where no edge stands
  the router terminates it with `-route-cert`/`-route-key`. Plaintext `vif://` stays
  for a player's own tcp doors, where a passphrase door (D14) authenticates them.
  Asked of the edge in §10, Q1.
- **D5. The router admits in band.** After the route frame and the address budget
  the router dials the pod, or opens the door, relays the door's question to the
  dialer and reads one answer. A pass is forwarded unread. A sign-in token, or
  nothing, has the router ask the identity service which account the token names,
  mint a ticket naming that account or the dialer's address and the key the dialer
  proved, and forward the key, its proof and the ticket; the token never reaches the
  pod and crosses TLS alone. On the browser route the page's sign-in cookie on the
  upgrade is the token, same-origin, `SameSite=Strict` and `Origin`-checked. One
  connection and one exchange on every host, native and page alike.
- **D6. Pods verify.** A fleet pod runs `-admit ticket` with the router's public key
  in its Job's arguments and refuses a new admission that shows none; a pass is its
  own and needs no router. The router stays stateless, and a pod trusts no splice.
- **D7. vif's own tokens, no JWT.** A ticket and a pass are one form, a JSON body
  under an Ed25519 signature with a domain tag (`network.Signed`), two hundred bytes
  and the standard library alone. Their only verifiers are vif's own doors, so
  `lixenwraith/auth` is taken for Argon2id and SCRAM and nothing in vif reads a JWT.
- **D8. Accounts live in an identity service of their own.** Registration, sign-in
  by Argon2-SCRAM, sign-ins per device, their revocation, and one loopback question
  for the router, which account does this token name. Generic, in a repository of
  its own, so chess can take it as its account service later and the game binary
  never names a database driver. The router forwards `/vif/api/auth/` to it byte for
  byte, as it forwards the log stream.
- **D9. The store is PostgreSQL 18 in a FreeBSD jail**, reached from the bhyve guest
  the router runs in over the host's internal network, by a role that may reach the
  service's database alone, with SCRAM-SHA-256 and TLS on the hop. Backed up by
  `pg_dump` as chess's is. The driver is Q2.
- **D10. The KDF runs on the player.** Argon2-SCRAM wherever a password is proven: a
  sign-in and a passphrase door. Registration sends the derived credential, so the
  service runs no Argon2 at any rate of sign-ins or registrations, and the deployment
  pins one KDF profile for every account (64 MiB, 3 passes, 4 lanes until measured on
  a phone and a page). The one server-side Argon2 is the operator's CLI, resetting a
  password.
- **D11. A sign-in is opaque**, 32 random bytes whose SHA-256 the service stores, one
  per device, named and revocable one by one, sliding over 30 days and renewed past
  half its life. A page holds it in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie
  scoped to `/vif/api/`; a terminal in a 0600 file, refused wider; Android in private
  storage.
- **D12. Anonymous play stays.** An account adds to what a player can do and its
  absence refuses nothing by default; an anonymous ticket names the dialer's address.
  An account is required only for what needs a lasting name: a roster name, keep-out
  by account, publishing a door, creating past the anonymous budget.
- **D13. A pass lasts the session**, capped at a day, renewed past half its life in
  a redial's offer; a drop puts its key, and its account if any, on the keep-out list,
  and the session's end leaves no door to answer for it. Revoking a sign-in stops new
  tickets and leaves a pass to the session's rule.
- **D14. A passphrase door's secret is generated**, about 80 bits shown once, chosen
  when the host asks, never in the link.
- **D15. Names.** An account's name, copied onto the roster entry by the coordinator
  from a verified ticket, is attested; an anonymous player keeps the slot's label.
- **D16. Operator authority is loopback**: dropping a participant and draining take
  a loopback listener of the router's, reached over ssh; no operator accounts.
- **D17. Recovery is an operator's reset**, no email.
- **D18. The game imports `lixenwraith/auth` as it is** for SCRAM, and the native,
  wasm and Android sizes are read before and after; splitting the module waits on
  those readings.

## 1. Words

*Session* keeps its vif senses ([term index](term-index.md#s)), above all one world
timeline under one authority. The words of signing in are kept apart from it:

- An **account** is a lasting player identity the identity service holds: an id, a
  name and a SCRAM credential.
- A **sign-in** is an account's proof on one device, an opaque token the service
  can revoke. chess calls it a session.
- A **participant key** is the Ed25519 key pair a participant makes for its place
  (`network.Key`), whose public half the roster carries; a **proof** is its
  signature over a door's nonce, the session's name and the door's channel binding.
- An **admission ticket** is a short-lived capability for one session, signed by
  the router: the session, the kind of admission (play, watch, create, publish), the
  subject it admits, an account or an address, and the key it admits.
- A **session pass** is a session authority's signed statement that a participant
  key was admitted, and as what. A redial shows it, with a fresh proof, in place of
  authenticating again.
- A **signed token** (`network.Signed`) is the form both share: a JSON body and an
  Ed25519 signature over it under a domain tag.
- A **passphrase door** admits a dial only after an Argon2-SCRAM exchange over a
  passphrase its host set.
- The **identity service** is the node service that holds accounts and sign-ins,
  answers `/vif/api/auth/` and tells the router which account a sign-in names.

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
- **A session's own NodePort** (31700–31709), which D3 removes: the pod's
  `AdmissionLimiter`, 6 joins a minute per address (an IPv6 /64), 1024 addresses
  tracked, and `MaxHandshakes` (8) in flight.
- **A player's doors**, `-host` and `-serve` at tcp, ws and wss. A wss door serves
  `-cert`/`-key` or a `-selfsign` certificate; a dialer trusts the system's roots, a
  self-signed certificate as it is, or what `-ca` names: an issuer's PEM, or
  `sha256:<hex>` pinning the leaf, which the host prints and `:session` reads
  (`network.TrustTLS`). A ws door checks no `Origin`.
- **A relay door published through the router** (`-relay vif://<site>:7777`):
  `PublishRequest` names the session, and its `Resume` token, a bearer secret, keeps
  the door's name for `NetworkTunnelResumeWindow` (10 min) after a lost control
  connection; `-doors-per-address`, and the join budget per publication. Plaintext.
- **The join handshake** (`network.HostAcceptor`): route, budget
  (`Coordinator.Admit`), claims (`MsgJoinClaims`), preview and seat, `Assign`, the
  offer, then the joiner's `PeerIdentity`, a build and session match
  ([Services §7.2](services-and-networking.md#72-join-identity-and-who-decides-it)).
  It identifies a binary and a world, never a person.
- **A claim** (`network.NewClaimToken`): 32 random bytes whose SHA-256 the roster
  carries; a dial names the token in clear to the coordinator, through its relay if
  it has one. A page keeps its claims in the tab's `sessionStorage`; a terminal and
  Android keep them for the process's life. A claim keeps a place from another player
  for `WraithTimeout` (2 min of game time), and is no credential against the wire
  ([Multiplayer §5.4](multi-player.md#54-wraiths)).
- **A peer link** (`network.PeerAcceptor`, `converge.Reach.admitPeerLink`):
  `MsgConnect` names its sender's participant identity, and the acceptor admits any
  roster member but itself, so whoever reaches a participant's port can open a link
  as any participant it holds no link to, and act as that participant's cursor
  through `crossingActors` ([Services §10](services-and-networking.md#10-timeouts-limits-and-security)).
  A fleet pod runs `-authority host`, one port; a player's session every participant
  that advertises a port.
- `MsgAuthRequest` (0x30) and `MsgAuthResponse` (0x31) are defined
  (`AuthRequest`, `AuthResponse`) and sent by nothing; `NetworkSystem` counts one as
  a drop.

Confidentiality exists only on TLS, the browser route as far as the site's edge and
a player's wss doors. On vif:// and ws, a claim, a resume token and every frame can
be read and replayed by anyone on the path.

## 3. The open work this serves

From [the todo list](todo.md), each with what it needs here.

Authentication itself:

- [Add browser admission authentication](todo.md#add-browser-admission-authentication)
  (P2): met by phases 2, 4 and 5 (§7.6). Its prerequisite, every route carrying real
  play without transport faults, applies to all of §7.
- [Publish a player's session at the router](todo.md#publish-a-players-session-at-the-router)
  (P3): a publish ticket (D12) and accounting keyed by the account: phase 6.
- [Drop the direct NodePort route](todo.md#drop-the-direct-nodeport-route) (P1,
  D3): phase 0, which every later phase assumes.

Waiting on an identity:

- [Budget a fleet pod's joins per player](todo.md#budget-a-fleet-pods-joins-per-player)
  (P3): the ticket's subject names the player to the pod (phase 4).
- [Name participants beyond their slot](todo.md#name-participants-beyond-their-slot)
  (P3): a roster field, a capture schema change and D15 (phase 5).
- [Let an authority drop a participant](todo.md#let-an-authority-drop-a-participant)
  (P2): keeping a dropped player out takes an identity, and its keep-out list is
  what refuses that player's passes (phase 2); the operator route is D16.
- [Watch a session as a spectator](todo.md#watch-a-session-as-a-spectator) (P2): a
  second kind of admission, which a ticket's and a pass's kind tells from a player.
- [Offer lixen.com games on the start screen](todo.md#offer-lixencom-games-on-the-start-screen)
  (P1): builds on the API as it stands, and takes `:login` once accounts exist.

Session membership whose trust overlaps:

- [Hear a participant again once its link reopens](todo.md#hear-a-participant-again-once-its-link-reopens)
  (P1): with participant keys only the participant can reopen its link.
- [Keep a Wraith's stats in the session](todo.md#keep-a-wraiths-stats-in-the-session)
  (P2): a key proves the run is the one that left, never that its record is true.
- [Merge two sessions](todo.md#merge-two-sessions) and
  [Split a session under a second authority](todo.md#split-a-session-under-a-second-authority)
  (P3): the redirect is signed by the term's authority, and a pass is good at the
  door it names only once that door's authority admits it.
- [Find games on the local network](todo.md#find-games-on-the-local-network) (P2):
  a passphrase door authenticates the host a beacon names.

Adjacent, and out of scope: [Fetch a content-addressed bundle over HTTP](todo.md#fetch-a-content-addressed-bundle-over-http)
(P1), [Cache a received scenario to the user root](todo.md#cache-a-received-scenario-to-the-user-root)
(P3), [Promote verified images to the node](todo.md#promote-verified-images-to-the-node)
(P2) and [Sign the Android build with a release key](todo.md#sign-the-android-build-with-a-release-key)
(P2) sign content and builds, not people; a publisher's key can reuse the Ed25519
code here.

## 4. `lixenwraith/auth`, and logwisp as its consumer

v0.5.1: one package over `golang-jwt/jwt/v5` and `golang.org/x/crypto`. What vif
takes:

- **Argon2-SCRAM**: the package's own JSON challenge and response over Argon2id, not
  SASL's SCRAM-SHA-256. `NewCredential` provisions in one KDF pass on the client;
  `Export`/`ImportCredential` carry the result as a map, which the identity service
  stores and a passphrase door keeps in memory. `ScramServer` holds credentials of
  one shared KDF profile and at most 4096 single-use handshakes of 30 s, and the
  server side only HMACs; `NewScramServerWithDecoyKey` keeps an unknown user's decoy
  salt stable across restarts, so a probe cannot tell an account from none.
  `ScramClient` refuses a cost below 64 MiB and 3 passes unless `WithMinArgonCost`
  says otherwise: the client's floor, never the challenge's. `WithChannelBinding`
  (1–64 bytes at both ends) commits the proof to the TLS channel, and a bound and an
  unbound peer never agree. Only a verified server final authenticates, and its
  `Username` is the identity. Every bound is per operation; the caller rate-limits by
  address, bounds frames before decoding and bounds concurrent KDF calls.
- **Argon2id PHC** (`HashPassword`, `VerifyPassword`): 64 MiB a verification.
  Nothing in vif stores a password hash (D10); the operator's CLI derives a SCRAM
  credential instead.

What vif does not take: the JWT managers (D7) and `SimpleTokenValidator` (a sign-in
has an expiry and a store). What auth gains for vif, in the PR beside this one:
`ScramServer.Abandon(fullNonce)`, which frees a handshake whose dialer dropped after
the challenge without running the proof path, the call logwisp made through
`ProcessClientFinalMessage(nonce, "")`; and its README and package doc no longer
cite an audit document the repository does not hold.

logwisp v0.39 is the consumer to copy, not chess: `internal/authz/scram.go` runs the
whole exchange on TCP (a hello carrying the client first, one JSON line a step) and
on HTTP (`POST /auth`, a challenge then a token), bound to the listener's leaf
certificate (`sha256.Sum256(cert.Certificate[0])`) and unbound only behind a
trusted TLS-terminating proxy, where a browser's token becomes a cookie and logout
a revocation set; a per-address table of attempts (`netacl.Table`: a burst, a rate
and the unfinished exchanges one address may hold), refunded on success and drained
on failure; a dialer that pins the certificate it bound to and renews its token
before expiry; an abandoned exchange released at once. `internal/tlsx/pki.go` is the
pattern D1 takes: a key made once per process (`processKey`), certificates reissued
on it, so a pin holds across reloads and the identity costs no file. vif's SCRAM
frames are `AuthRequest.Scram` and `AuthResponse.Scram` carrying the same four auth
structs.

## 5. What `lixenwraith/chess` gives

chess v0.16.3 requires auth for Argon2id hashing, an HS256 JWT bound to a stored
sign-in row and bearer parsing; it uses no SCRAM. What vif takes from it is the
store and its operations, since its deployment is the one D9 mirrors:

- PostgreSQL 18 in a FreeBSD jail (`deploy/freebsd/setup-jail.sh`,
  `deploy/postgresql/setup.sql`): a database and a schema of the service's name, a
  login role with `CONNECT` and `search_path` alone, `public` revoked, statement,
  lock and idle-in-transaction timeouts set on the role, an optional split between
  an owner role that migrates and a runtime role that reads and writes rows. chess
  connects over the jail's Unix socket with peer authentication; the identity
  service connects over TCP from another guest, so its role takes a SCRAM-SHA-256
  password and `pg_hba.conf` admits it from the guest's address with `hostssl`.
- Migrations as an ordered list applied in one transaction under an advisory lock,
  the version in a one-row table; a nightly `pg_dump --format=custom` kept 14 days
  (`deploy/postgresql/chess-backup.sh`), and `pg_restore` into a fresh database.
- An operator CLI over the store (`db user add|delete|set-password|list`), the
  password prompted rather than passed; a dummy credential for an unknown name so a
  refusal costs what a wrong password does, which SCRAM's decoy salts give for free;
  a `-max-users` cap on public registration; limits per address on registration (5
  a minute), sign-in (10), reset (10) and deletion (5); `-trusted-proxies` parsed
  strictly before a forwarded address is believed.

What vif does otherwise, and why: several sign-ins an account, one a device (D11);
a page's sign-in in an `HttpOnly` cookie, where chess's web client keeps a JWT in
`localStorage`; no recovery token in a URL (D17); nothing read from a store on the
game path, since a pass is checked against the session's own keys; and SCRAM, so
no password crosses to the server and no server runs the KDF (D10). chess's own
authentication needs no fix for vif's sake; adopting the identity service (D8) is
its later choice, which the service's generic API keeps open.

## 6. Requirements

- Anonymous play stays possible (D12).
- A dropped link, a crash, a restart or a reboot authenticates nothing again: a
  redial within the pass's life proves its key, with no prompt, no KDF and no request
  to the site (D1, D13). Starting the game again on a signed-in device asks for no
  password within 30 days (D11).
- Least privilege: a ticket admits one kind of admission to one session, once,
  for one key; a pass admits one key to one session. Pods and player hosts hold no
  key that mints a ticket; the router holds the ticket key and verifies no password;
  the identity service verifies SCRAM proofs and holds no plaintext.
- No credential in a URL, a command line, a log line, page history, a capture or a
  journal. The handshake is outside the journal, and the JSONL log records a
  refusal without its credential. A sign-in token crosses TLS alone (D4, D5).
- One flow and one vocabulary on every host: `:login`, `:logout`, a passphrase
  prompt and a Multiplayer page section on a terminal, the window, Android and a
  page; a host's lack reads `unavailable in this build` (AGENTS.md, Platforms).
- One mechanism per problem (D2, D7).
- The standard library first (`crypto/ed25519`, `crypto/tls`, `crypto/rand`), and
  `lixenwraith/auth` for Argon2id and SCRAM. No cgo: FreeBSD, Linux, Android and
  js/wasm alike. The game binary names no database driver (D8).
- Hardening on every new surface: a bounded frame before decoding (`MaxAuthFrame`),
  a deadline on every step, a budget per address before any signature is checked,
  a spent set that is bounded (`SpentTickets`), tokens refused from the future as
  from the past, and a fuzz target on every frame a stranger's bytes reach.

The threats, and the part that meets each:

1. A stranger who reaches a participant's port and names a participant identity
   (§2): keys on peer links (§7.1).
2. A machine in the middle of a self-signed door: a bound passphrase door (§7.4).
3. A stranger in a session whose link leaked, and a dropped player coming back:
   tickets (§7.5), and keep-out by key or account (§7.3).
4. Guessing and enumerating accounts, and exhausting a server's KDF memory: SCRAM
   with decoy salts, no server KDF, and the identity service's limits (§7.5, §9).
5. A compromised pod, which runs vif over a stranger's bytes: it holds the router's
   public key and its own session's signer, never the router's key or a sign-in.
6. A pass read off a plaintext link, or out of a page's storage without its key:
   useless alone, since every use signs a fresh nonce (§7.3).
7. A sign-in token read off the wire: it crosses TLS alone, and the router strips it
   before the pod (D4, D5); a stolen one is revoked from the device list (D11).

## 7. The plan

### 7.0 What is built

`internal/network/admission.go`, sent by nothing yet:

- `Key`: an Ed25519 key pair, `NewKey`, `Public`, kept as its 32-byte seed in JSON;
  `SignProof`/`VerifyProof` over a door's nonce, the session's name and a channel
  binding (`ChannelBinding`, the SHA-256 of a leaf certificate); `NewNonce`,
  `parameter.AuthNonceBytes` of randomness.
- `Signed`: the body as signed and the signature, under a domain tag (`signable`:
  `vif <domain>\0`, then each field behind its length), so a proof, a pass and a
  ticket under one key never stand in for each other.
- `PassBody`, `SignPass`, `VerifyPass` (the term's key looked up by the term the
  body names, `PassLifetime` the cap, `Renewable` past half-life) and `TicketBody`,
  `SignTicket`, `VerifyTicket` (`TicketLifetime`, a fresh `jti`), both with
  `AuthClockLeeway` of skew either way; `SpentTickets`, bounded by
  `SpentTicketsKept`; `AdmissionKind` (play, watch, create, publish), by name on the
  wire; `SubjectOf` an address as the budgets key it.
- `AuthResponse` and `AuthRequest`, the 0x31 and 0x30 bodies, `MaxAuthFrame` their
  bound, the methods `key`, `pass`, `ticket` and `scram`; the refusal kinds
  `RefusalSignIn` (retry after signing in), `RefusalCredential` and
  `RefusalDropped` (no retry).
- Tests, one a rule, and `FuzzASignedTokenIsOpenedWholeOrRefused`.

### 7.1 Participant keys

A participant makes a `Key` for its place and its first admission request carries
the public half with a proof over the door's nonce. The coordinator records it on
the roster entry (`RosterEntry.Key`, replacing `Claim`); the arrival crosses it, so
every instance and every capture holds it. Then:

- A peer link is a challenge: the acceptor answers `MsgConnect` with a nonce, the
  dialer signs the session, both identities and the nonce, and the acceptor verifies
  against the roster, as the dialer does the acceptor's. A name alone opens nothing.
- Succession notices and handoff records (`MsgAuthorityReport`,
  `MsgAuthorityHandoff`) carry their author's signature, so a forwarded one is still
  its author's. A handoff record names each term's authority key, which a pass is
  checked against (§7.3). A redirect, for a merge or a split, is signed by the
  term's authority.
- A claim becomes possession of the key: an away entry keeps the key, and a
  returning dial signs the door's nonce instead of naming a token, so the door seats
  it in the place that key holds, whatever gate the door keeps. The token,
  `ClaimDigest` and `MsgJoinClaims` go; the keeper holds private keys where it held
  tokens, and a relay that carries a dial can no longer take its place.
- The keeper's store becomes every host's (D1): `ClaimStore` stays the interface,
  a page's `sessionStorage` document as today, a terminal's a 0600 file
  `places.json` under `paths.stateDir`, written as claims are written now
  (`ClaimStoreInterval`), Android's in the app's files. `keep_places` names it.

Cost: 32 bytes a roster entry, a round trip a peer link, and a signature of tens of
microseconds a link and a succession message, nothing a tick. It needs no
infrastructure and no new dependency.

### 7.2 The admission exchange

The door asks after the route frame and the address budget, in place of today's
claims step, so an open door pays no round trip it does not pay now:

```
dialer                                   door
MsgSessionRoute                          -->
                                         <--  MsgAuthResponse{methods, nonce}
MsgAuthRequest{key, proof, pass}         -->  a returning participant: done
MsgAuthRequest{key, proof, ticket}       -->  a fleet join: done
MsgAuthRequest{key, proof}               -->  a new joiner at an open door: done
MsgAuthRequest{key, proof, scram first}  -->  a passphrase door:
                                         <--  MsgAuthResponse{scram challenge}
MsgAuthRequest{scram proof}              -->
                                         <--  MsgAuthResponse{scram final}, or a refusal
                                         <--  the preview, the seat and the offer as today
```

Every answer carries the key and its proof; a pass or a ticket says what admits it.
A pass names its key and a key names its place, so nothing is left to claim. A
refusal is `RefusalSignIn` when the door takes no bare key, `RefusalCredential` for
what was shown, `RefusalDropped` for a key or account on the keep-out list; a page
shows its reason as it shows every refusal. Both frames are bounded
(`MaxAuthFrame`) and join the handshake's fuzz target. A relay asks the authority
for the nonce it hands a dialer, so a proof it carries is good once.

Through the router (D5) the same frames run end to end, the router placing itself
between the question and the answer for one frame each way: it reads the dialer's
answer, forwards a pass whole, and turns a sign-in or nothing into a ticket. Its
own refusals (`RefusalSignIn` when a session is for accounts alone,
`RefusalCredential` for a token the identity service does not know) are the join
reply the dialer reads today.

### 7.3 Session passes

§7.1 already brings a participant back to its own place with one signature while
the roster keeps the place, a Wraith's 2 minutes. A pass carries the admission past
that, past the door that made it, and past the process that held it (D1): a phone
the system suspended, a laptop that slept, a game that crashed, a machine rebooted
or a page reopened comes back without its gate, no passphrase prompt, no KDF, no
ticket from the site.

- **Issued** at every admission: the offer carries it, signed by the authority with
  its own participant key (a fleet pod's is the session key it makes at start). It
  names the session, the first term's authority key, so a later session under the
  same name refuses it, the participant's key, the kind, the account and name when a
  ticket named one, the term and an expiry (`PassBody`).
- **Checked** at any door of the session: the authority, a successor, which holds
  every term's authority key from the handoff records, and a relay, which checks it
  before it carries the dial on. The door verifies the pass against that term's key
  and the clock (`VerifyPass`), then the session's keep-out list, then the proof
  against the key it names: one round trip and two signature checks, about a hundred
  microseconds on a desktop core.
- **Placed** by its key: back in the place the key holds, or a new place once the
  Wraith was dismissed. A redial past half the pass's life takes a fresh one in its
  offer (`Renewable`).
- **Revoked** by a drop, which puts the key, and the account when the pass names one,
  on a keep-out list that crosses as shared state, and by the session's end.
- **Kept** beside its key, where claims are kept now and on every host (§7.1).

A pass stolen alone is nothing: every use signs a fresh nonce with the key it
names. A pass and its key stolen together, from a page's storage or a device, are
the participant until the session drops it or ends.

### 7.4 Passphrase doors

`-passphrase` and `:host` take a passphrase or make one (D14), which `:session`
shows. The host provisions one SCRAM credential (`auth.NewCredential`, once) and
serves a `ScramServer` at every door; a joiner asks its player for the passphrase
when the door's methods name `scram` alone, and verifies the server final before it
reads an offer. On a wss door both ends bind the exchange to the SHA-256 of the leaf
certificate, the digest `network.Fingerprint` prints: a self-signed door is then
authenticated both ways, and `-ca` stays for a pin without a passphrase. On tcp and
ws the exchange authenticates the handshake alone. The pass it ends with spares the
joiner the passphrase on every redial.

A machine in the middle that answers for the door receives a proof it can test
passphrases against offline, at Argon2's cost a guess: a generated code of about 80
bits is past guessing, a chosen phrase must be long. A door published through the
router is reached over the router's TLS to the site's edge and plaintext on the
tunnel, so its exchange runs unbound there (§8). The KDF runs on the joiner, 64 MiB
once a join, a page's and a phone's too: measure it there before the cost is chosen.

### 7.5 Admission tickets and accounts

The router mints tickets in band (D5), signed by a key it alone holds, with
`SignTicket`: the session, the kind, the subject, the dialer's key and a minute.
A pod verifies it with the public key its Job's arguments carry (`-admit ticket
<key>`), holds its session to the pod's own name, spends its id once
(`SpentTickets`) and checks the dialer's proof over the key the ticket names. A
player's host may verify the site's tickets too, from `GET /vif/api/keys`, for a
session open to the site's accounts alone. A ticket admits once; the pass it is
exchanged for admits every redial after.

Anonymous tickets come first: their subject is the dial's address (`SubjectOf`),
so the router names the player to the pod, and a pod run with `-admit ticket`
refuses a new admission that went around the router, which D3 leaves no route for.
Accounts follow, in the identity service (D8):

- `POST /vif/api/auth/register`: a name and a derived credential
  (`Credential.Export`), checked against the deployment's one KDF profile and a name
  policy; the service runs no KDF. 201 with the account's id.
- `POST /vif/api/auth/login`, two steps: the client first, answered with the
  challenge; the client final, answered with the server final and the sign-in: a
  bearer token for a native client, a cookie for a page that asked for one
  (logwisp's `session: cookie`). The device's name travels with the final.
- `POST /vif/api/auth/logout`, `GET /vif/api/auth/me` (id, name, the sign-ins by
  device), `DELETE /vif/api/auth/sign-ins/<id>`.
- On a second loopback listener the forward never reaches: `POST /introspect` with
  a token, answered with the account's id and name, or nothing; a renewal past the
  sign-in's half-life rides the same request.
- An operator CLI over the store: add, disable, reset a password (the one
  server-side Argon2), list; a backup and a restore.
- Limits per client address, believed from the router's loopback address alone:
  registration, sign-in and deletion as chess bounds them; a `-max-accounts` cap.

Then the router budgets creates, joins and doors by account where it mints; the
coordinator copies the account's id and name onto the roster entry and the pass
(D15); a revoked sign-in stops new tickets; `:login <site>` and `:logout` run the
exchange from the game on every host (`auth.ScramClient`), the lobby page reading
the same cookie.

### 7.6 Phases

Each a PR, in this order; each bumps `ProtocolVersion` where it changes a frame,
and phase 1 bumps `snapshot.Schema` and `event.JournalSchema` with the roster.

- **Phase 0, the direct route goes** (D3): `tool/vif-router` drops the NodePort
  Service and pool for `-sessions-max`; `direct_target` and `port` leave the API;
  `deploy/k3s` (the policy admits the game port from the node alone, `session.sh`
  and `render-session.sh` stop allocating ports), `nftables.conf`, the fleet,
  deploy and router docs and the term index follow. The host's 31700–31709 forward
  is asked of the user (§10, Q1).
- **Phase 1, keys and the exchange** (§7.1, §7.2 on player hosts): keys replace
  claims, the exchange replaces `MsgJoinClaims`, peer links and succession messages
  are signed, the keeper persists keys on every host behind `keep_places`. The
  router splices the new frames unread; the fleet runs open doors still.
- **Phase 2, passes** (§7.3) and keep-out, with [Let an authority drop a participant](todo.md#let-an-authority-drop-a-participant).
- **Phase 3, passphrase doors** (§7.4): auth enters the game binary; native, wasm
  and Android sizes read before and after (D18); the Argon2 cost measured on a page
  and a phone.
- **Phase 4, the router's TLS front door, in-band admission and anonymous tickets**
  (D4, D5, D6, §7.5 first half): `vifs://`, PROXY protocol v2 on the front door, the
  ticket key and `keygen`, the pod's `-admit ticket`, `GET /vif/api/keys`, the edge's
  stream block.
- **Phase 5, accounts**: the identity service in its own repository (§10, Q2, Q3),
  PostgreSQL in the jail, the router's forward and introspection, the cookie on the
  upgrade, `:login`/`:logout`, names on the roster (D15).
- **Phase 6, the account's reach**: publish and create tickets, budgets by account,
  the operator's loopback routes (D16), spectators.

## 8. What it asks of vif-router and the deployment

By phase:

- **Phase 0**: the Service becomes `ClusterIP`, kept for the EndpointSlice readiness
  the allocator reads; the pool and `isNodePortConflict` go, `-sessions-max` (10)
  stands in beside the quota's `count/jobs`; `fleetLimits.SessionsMax` reads it.
  `20-networkpolicy.yaml`'s game ingress takes an `ipBlock` of the node's address on
  the pod network, a placeholder the deploy renders, so a pod is reachable from the
  router alone; the host forwards 7777 and nothing else.
- **Phases 1–3**: nothing. The router places by the route frame and splices the
  rest unread, so the door's question and the dialer's answer cross it as bytes. A
  router update waits for an empty fleet, so a fleet player never redials across one;
  it does drop published doors' tunnels, and their players come back through the
  resumed publication with their passes. The publication's `Resume` token is then
  the last bearer secret on the wire: phase 6 has a publish carry the run's key and a
  resume sign the router's nonce.
- **Phase 4**:
  - The edge terminates TLS for 7777 in an nginx `stream` block and forwards to the
    node's 7777 with `proxy_protocol on`; the router reads one PROXY v2 header before
    the route frame (`-route-proxy-protocol`, refusing a connection without one) and
    keys its budgets on the address it names. Without an edge, `-route-cert` and
    `-route-key` make the router the TLS end. The game's `vifs://` dials TLS with
    the system's roots, as a wss door is dialled.
  - The ticket key is a file in `/etc/vif-router`, `root:vif-router` 0640 as
    `router.env` is, made once on the node by `update-vif-router.sh` when absent
    (`vif-router keygen`) and named by `-ticket-key` in `vif-router.env`, so the
    repository holds the path and never the key. It needs no backup: a lost key is
    a new one, and only the tickets open at the time die with it.
  - Rotation rides a router update, which waits for an empty fleet, so no pod holds
    the old public key. `GET /vif/api/keys` lists the current and the previous key.
  - The verifier: the router writes its public key into each Job's arguments
    (`manifest.go`, with `30-session.yaml` and `render-session.sh` in step) beside
    `-admit ticket`. A public key is no secret: no Secret, ConfigMap or mount, no
    change to the Role, no egress. A session made by hand runs open unless given
    the key.
  - The front door's placement grows one frame each way (§7.2), bounded by
    `MaxAuthFrame` and `routeTimeout`, and the browser route shares it; the
    `routePlacing` ceiling of 32 bounds the exchanges in flight as it bounds
    lookups. A ticket costs the router one signature a join and the pod one check.
- **Phase 5**:
  - The identity service runs on the node as a system user of its own with its own
    unit and state directory (the decoy key, 0600), bound to loopback twice: the
    `/vif/api/auth/` listener the router forwards to, and the introspection listener
    the forward never reaches. No new public port and no filter change; `update.sh`
    gains it as a component, updated without the empty-fleet gate, since its restart
    pauses only sign-ins and account tickets.
  - Its store is the jail's PostgreSQL (D9): a database and schema of the service's
    name, a role for it with a SCRAM-SHA-256 password read from a 0600 file, reached
    over the host's internal network with `hostssl` in `pg_hba.conf` and the jail's
    certificate pinned or signed by an issuer the service is given; `statement_timeout`
    and its siblings on the role as chess sets them; `pg_dump` nightly on the jail.
    The database is the host's to provision; the service ships the SQL, the schema
    migrations and the backup script.
  - The router forwards `/vif/api/auth/` byte for byte (`log_proxy.go`'s shape) and
    asks introspection once per account ticket. A page's sign-in is a cookie scoped
    to `/vif/api/`, and the router mints from a cookie only on a request whose
    `Origin` is `-web-origin`.
  - The public log stream carries the sessions' logs, which name a participant by
    slot, never by account; the service's logs stay off it and hold no credential.
- **Phase 6**: the operator routes, dropping a participant and draining, on a
  loopback listener of the router's own (D16).

What the site's edge then provides, beside [Deploying §12](kube-docker-deploy.md#12-the-sites-edge):
a `stream` server on 7777 ending TLS and forwarding with PROXY protocol v2 to the
node (phase 4); `/vif/api/auth/` and `GET /vif/api/keys` published to 9080, with
`Cookie` and `Authorization` passed through untouched and kept out of its access
log, nothing cached under `/vif/api/auth/` (phase 5); and the 31700–31709 forward
removed (phase 0).

Once phase 4 lands, D6 in [Deploying §0](kube-docker-deploy.md#0-decisions) and
[The session fleet §4](kubernetes-fleet.md#4-security-posture) change: a fleet
admission takes a ticket over TLS, and only a player's own tcp and ws doors stay
unauthenticated on the wire.

## 9. Ten thousand routed players on one machine

The target: `vif-router` carries 10,000 concurrent routed players on a node of
about 8 GiB that also runs the fleet's pods, "routed" being spliced connections,
most of them to doors players publish, since a pod holds a few players and a few
hundred megabytes each. What each player costs the router, and what bounds it:

- **No KDF.** D10 moves Argon2 to the player: the router verifies signatures and the
  identity service HMACs. chess's shape, a password over HTTPS and Argon2id on the
  server behind four 64 MiB slots, serves about two sign-ins a second at 256 MiB
  and would take an hour over a launch day's registrations; SCRAM with
  client-derived credentials serves any rate at no memory. The one Argon2 on the
  node is the operator's reset.
- **Signatures.** Ed25519 signs in about 25 µs and verifies in about 60 µs on a
  desktop core; 10,000 admissions in a minute are 170 a second, a sign and a verify
  each, under 2% of one core. An introspection per account admission is one
  loopback request, 170 a second at that rate and nothing at steady state, since a
  sign-in lasts 30 days.
- **Splices.** Today each placed connection costs two goroutines and two 32 KiB
  `io.Copy` buffers, about 80 KiB, 800 MiB at 10,000; the `stallBounded` wrapper
  also keeps Go from its kernel `splice(2)` path between two TCP sockets. Phase 4
  moves the stall deadline off the writer so TCP-to-TCP copies run in the kernel
  with no user buffer, and gives the WebSocket leg an 8 KiB pooled buffer: about 24
  KiB a connection, 240 MiB at 10,000, plus the kernel's socket buffers under load.
  The pod side pays nothing new: the router already terminates both routes.
- **TLS.** At the edge, the edge pays it (about 30 KiB a connection in nginx);
  terminated by the router, `crypto/tls` adds about 40 KiB a connection, 400 MiB at
  10,000, which is why D4 prefers the edge.
- **Tables.** `SpentTickets` holds 4096 ids a minute; the join and create budgets
  track 1024 addresses; the identity service's `ScramServer` holds 4096 handshakes
  of 30 s and every credential (10,000 at about 150 bytes is 1.5 MiB); its
  PostgreSQL pool is ten connections. All in the tens of megabytes together.
- **The budget**: the router under 1 GiB at 10,000 connections with the edge
  terminating TLS, the identity service under 256 MiB, PostgreSQL in its jail, and
  the pods what remains, about 6 GiB, twelve sessions at 480 MiB each; past them
  the site offers a hosted session through a player's door.

The readings to take, in phase 4's PR: 10,000 connections held through a stand-in
door on a second process, RSS and CPU of the router before and after the splice
change, and the admission exchange's cost per connection; `-route-max` and
`routePlacing` tuned from them. A projection is not a result.

## 10. Questions still to decide

Each needs the user, since it names the host, the site or a repository.

- **Q1. The edge and the host** (D3, D4). Remove the 31700–31709 forward; add an
  nginx `stream` server ending TLS on 7777 with `proxy_protocol on` to the node's
  7777, and the `/vif/api/auth/` and `/vif/api/keys` locations with `Cookie` and
  `Authorization` passed through. Both are outside this repository. Without the
  stream block the router terminates TLS itself from a certificate it is given, or
  phase 4 falls back to minting tickets over `POST /vif/api/sessions/<id>/tickets`
  and the front door stays plaintext, carrying only the one-minute ticket.
- **Q2. The PostgreSQL driver** for the identity service. `jackc/pgx/v5` as `pgconn`
  alone, the protocol, SCRAM-SHA-256 and TLS without `database/sql` (the driver
  chess uses, a dozen modules); or a client of our own on the standard library
  (`crypto/pbkdf2`, `crypto/hmac`, `crypto/tls`), the simple and extended query
  protocol, SCRAM-SHA-256 and text results, about a thousand lines in a repository
  of its own, for a service whose whole schema is two tables. The leaning is the
  client of our own, behind the service's store interface so either fits.
- **Q3. The identity service's repository and name**, and whether chess adopts it
  (D8): `lixenwraith/identity`, one binary, a loopback API and a CLI, with vif the
  first consumer and chess's accounts importable by a `MigrateFromPHC` run the
  user types a password into; or vif-specific under `tool/`, which puts a database
  driver in vif's module.
- **Q4. The settings name** for D1, `[play] keep_places`, and the Android app's
  storage for it.
- **Q5. The KDF profile** pinned for accounts (D10), after the wasm and phone
  readings of phase 3: 64 MiB and 3 passes, or less on the phone's evidence.

## 11. File catalog

What an implementation reads and changes, by area. Paths are from each
repository's root.

The session layer, `internal/network`:

- `admission.go`: the keys, proofs, tokens, frames and spent set (§7.0).
- `protocol.go`: the kinds, `MsgAuthRequest`, `MsgAuthResponse`, `MsgSessionRoute`,
  `MsgJoinClaims` (to go), `MsgConnect`/`MsgAck`, the tunnel's kinds.
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
- `fuzz_test.go` and `admission_test.go`: the handshake and token fuzz targets.

The link, `pkg/network`:

- `endpoint.go`: schemes (`vifs://` joins them), `LoadTLS`, `SelfSignedTLS`,
  `Fingerprint`, `dialTLS`, `TrustTLS`, `RequestSession`, `ListedLinks`, `linkSites`.
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
- `internal/event/payload.go`: `ParticipantJoinedPayload`, which carries a
  participant's key.
- `internal/snapshot/capture.go`: `Schema`.

Membership, `internal/app/internal/membership`:

- `claim.go`: `ClaimKeeper`, which keeps keys and passes.
- `identity.go`: `buildIdentity`, `joinerReport`.
- `session.go`: the coordinator's `HostAcceptor` wiring, `claimedLocked`,
  `closeStaleLink`, `yieldLocked`, `redialSeat`.
- `relay.go`: a relay's acceptor, and the claims it carries to the authority.
- `converge.go`: `redial`, `redialDoor`, where a pass is shown.
- `host.go`, `join.go` and `seat.go`: hosting, dialling, and a seat's claims.
- `internal/app/internal/config/membership.go`: `Membership`, `ClaimStore`.

Parameters, paths, settings and commands:

- `internal/parameter/network.go`: `ClaimsKept`, `ClaimStoreInterval`,
  `WraithTimeout`, `NetworkAdmitWindow`, `NetworkAdmitBurst`,
  `NetworkAdmitTracked`, `NetworkTunnelResumeWindow`,
  `NetworkSessionRequestTimeout`, `AuthNonceBytes`, `TicketLifetime`,
  `SpentTicketsKept`, `PassLifetime`, `AuthClockLeeway`.
- `internal/paths/paths.go` (`stateDir`, where a terminal keeps its places and its
  sign-in) and `settings.go` (`Play`, where `keep_places` goes).
- `internal/mode/commands.go`, `config_menu.go` and `config_form.go`: `:host`,
  `:join`, `:session`, the Multiplayer page and its request form, where `:login`
  and a passphrase prompt go.
- `cmd/vif/main.go` and `usage.go`: the `-host`, `-serve`, `-join`, `-name`,
  `-cert`, `-key`, `-selfsign`, `-ca`, `-relay`, `-authority`, `-passphrase` and
  `-admit` flags and the help table the manual is made from; `cmd/vif/browser.go`:
  a page's `ClaimStore`.

Hosts:

- `web/terminal.js` and `web/worker.js`: the tab's claims in `sessionStorage`, the
  launch arguments.
- `android/game/game.go`, and `StartScreen.java` and `MainActivity.java` under
  `android/app/src/main/java/com/lixen/vif`: the Android run, which keeps no claims
  past its process yet.

The router, `tool/vif-router`:

- `server.go`: the mux, `/vif/api/sessions`, the create budget, `clientAddr`.
- `ws_proxy.go`: the browser route and its `Origin` check.
- `tcp_proxy.go`: the front door's placement, `placeDoor`, `splice`,
  `stallBounded`, the publication of a door.
- `log_proxy.go`: the loopback forward the identity service's routes copy.
- `config.go`: the router's flags.
- `allocator.go`: the NodePort pool, `availablePorts`, `sessionRecord`,
  `fleetLimits`.
- `manifest.go`: the Job a pod runs, whose arguments carry the ticket verifier;
  `buildService`, the NodePort Service.
- `kube.go`: the router's own credential, a ServiceAccount token file.
- `README.md`: the router's API.

The deployment:

- `deploy/guest/vif-router.env`, `update-vif-router.sh` and `vif-router.service`:
  the router's settings, which hold no secret, its update behind the empty-fleet
  gate, and where the ticket key file is made.
- `deploy/update.sh`: the components, and which of them wait for an empty fleet.
- `deploy/k3s/30-session.yaml`, `render-session.sh` and `session.sh`: the
  session's Job and Service as a template, and a session made by hand.
- `deploy/k3s/20-networkpolicy.yaml` and `40-router-rbac.yaml`: what reaches a
  pod, that it reaches nothing, and what the router may do.
- `deploy/guest/nftables.conf`: the node's filter, its NodePort comments.
- `deploy/website/vif.nginx.example`: the reference edge §8 extends.
- Deploying the session fleet §0 (D6), §2, §11 and §12.

Documents that state today's posture: Multiplayer §1, §5.4, §5.6 and §8; Services
and networking §7.1, §7.2 and §10; The session fleet §1, §4, §8 and §9; Build
profiles and platforms, Where WebSocket is spoken, The public route and §6; Domain
design §7.

`lixenwraith/auth`: `scram.go`, `argon2.go`, `error.go`, `example_test.go` (a whole
SCRAM exchange) and `README.md` (limits and deployment).

`lixenwraith/logwisp`, the SCRAM consumer to copy: `internal/authz/scram.go` and
`proxy.go`, `internal/netacl/limit.go`, `internal/tlsx/pki.go`, `doc/security.md`.

`lixenwraith/chess`, the store to mirror: `deploy/freebsd/setup-jail.sh`,
`deploy/postgresql/setup.sql` and `chess-backup.sh`, `internal/server/storage`
(`storage.go`, `schema.go`, `session.go`, `user.go`), `cmd/chess-server/cli/cli.go`,
`cmd/chess-server/secret.go` and `proxy.go`, `doc/database.md` and
`doc/deployment.md`.
