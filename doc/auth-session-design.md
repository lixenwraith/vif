# Authentication and session design

Who a vif player is, what admits a dial to a session, how a participant comes back
after a dropped link, a crash or a reboot without authenticating again, and how a
host and its participants prove themselves to each other. The decisions are settled
(§0); what they ask of the code is a plan in phases, each a PR (§7), of which the
direct route's removal and the participant keys with their admission exchange have
landed (§7.0). §2 states what authenticates today, §3 the open work this serves, §4
and §5 what `lixenwraith/auth`, `lixenwraith/logwisp` and `lixenwraith/chess` give
and what vif takes, §6 the requirements and threats, §8 what the plan asks of the
router and the deployment, §9 the router's budget for ten thousand routed players
and §10 the files an implementation reads and changes. As each phase lands its part
moves into the reference documents ([Multiplayer](multi-player.md),
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
  prompt. `[session] resume_after_restart = false` keeps nothing past the process.
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
  On this deployment TLS ends at the site's edge, which hands the router each
  connection under PROXY protocol so the dialer's address still keys its budgets;
  the router can end it itself for a deployment with no such edge
  (`-route-cert`/`-route-key`, off unless given). Plaintext `vif://` stays for a
  player's own tcp doors, where a passphrase door (D14) authenticates them.
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
- **D8. Accounts live in an identity service of their own**, `lixenwraith/identity`,
  a public repository: registration, sign-in by Argon2-SCRAM, sign-ins per device,
  their revocation, and one loopback question for the router, which account does
  this token name. Generic, so chess adopts it as its account service in a session
  of its own once vif runs on it, and the game binary never names a database driver.
  The router forwards `/vif/api/auth/` to it byte for byte, as it forwards the log
  stream.
- **D9. The store is PostgreSQL 18 in a FreeBSD jail**, reached from the bhyve guest
  the router runs in over the host's internal network, by a role that may reach the
  service's database alone, with SCRAM-SHA-256 and TLS on the hop. Backed up by
  `pg_dump` as chess's is. The client is the service's own, on the standard library
  (`crypto/pbkdf2`, `crypto/hmac`, `crypto/tls`): the simple and extended query
  protocol, SCRAM-SHA-256 and text results, for a schema of two tables; pgx's dozen
  modules are what chess carries and this service does not need.
- **D10. The KDF runs on the player.** Argon2-SCRAM wherever a password is proven: a
  sign-in and a passphrase door. Registration sends the derived credential, so the
  service runs no Argon2 at any rate of sign-ins or registrations, and the deployment
  pins one KDF profile for every account: 64 MiB, 3 passes and 4 lanes, read again
  on a phone and a page once vif's implementation is deployed. The one server-side
  Argon2 is the operator's CLI, resetting a password.
- **D11. A sign-in is opaque**, 32 random bytes whose SHA-256 the service stores, one
  per device, named and revocable one by one, sliding over 30 days and renewed past
  half its life. A page holds it in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie
  scoped to `/vif/`, so the browser route's upgrade at `/vif/ws/` carries it; a
  terminal in a 0600 file, refused wider; Android in private storage.
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
  connections a session. Plaintext. A pod admits its game port from the node alone,
  so every dial crosses this or the browser route, and the pod's own
  `AdmissionLimiter` sees one address, the node's.
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
  (`Coordinator.Admit`), the admission exchange (§7.2: the door's nonce, the dial's
  proven keys), preview and seat, `Assign`, the offer, then the joiner's
  `PeerIdentity`, a build and session match
  ([Services §7.2](services-and-networking.md#72-join-identity-and-who-decides-it)).
  It identifies a binary, a world and a key, never a person.
- **A participant key** (`network.Key`): the Ed25519 pair a run holds each place
  under, whose public half the roster, the cursor and every capture carry; a dial
  proves it over the door's nonce, through its relay if it has one, which carries
  the proofs to the authority with the challenge it asked for them. A page keeps
  its places in the site's `localStorage`, held by one tab at a time under a Web
  Lock, a terminal in `places.json` under the user's state directory, Android in
  the app's files, each as `[session] resume_after_restart` says. A key keeps a place from another player for
  `WraithTimeout` (2 min of game time); on a plaintext link anyone on the path can
  take the stream after the proof ([Multiplayer §5.4](multi-player.md#54-wraiths)).
- **A peer link** (`network.PeerAcceptor`, `converge.Reach.admitPeerLink`): a
  challenge both ways, each end proving the roster's key for itself over the
  other's nonce and the session, so a name alone opens nothing; the acceptor admits
  any roster member but itself. A fleet pod runs `-authority host`, one port; a
  player's session every participant that advertises a port.
- **Succession messages** (`MsgAuthorityReport`, `MsgAuthorityHandoff`) carry
  their author's signature, which a receiver checks against the roster's key for
  the participant they name before it acts; `MsgPeerList` is unsigned, and a forged
  chain costs a dial that no key answers.
- `MsgAuthRequest` (0x30) and `MsgAuthResponse` (0x31) carry the exchange; a pass,
  a ticket and a passphrase step are defined in them and sent by nothing yet.

Confidentiality exists only on TLS, the browser route as far as the site's edge and
a player's wss doors. On vif:// and ws, a claim, a resume token and every frame can
be read and replayed by anyone on the path.

## 3. The open work this serves

From [the todo list](todo.md), each with what it needs here.

Authentication itself:

- Browser admission authentication, the item phases 2, 4 and 5 met (§7.6) and
  closed with phase 5. Its prerequisite, every route carrying real play without
  transport faults, applies to all of §7.
- [Publish a player's session at the router](todo.md#publish-a-players-session-at-the-router)
  (P3): a publish ticket (D12) and accounting keyed by the account: phase 6.

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
has an expiry and a store). v0.5.2 added for vif `ScramServer.Abandon(fullNonce)`,
which frees a handshake whose dialer dropped after the challenge without running
the proof path, the call logwisp made through `ProcessClientFinalMessage(nonce, "")`
until v0.39.2; its README and package doc no longer cite an audit document the
repository does not hold.

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

Phases 0 to 5 (§7.6). The primitives in `internal/network/admission.go`: `Key`, an
Ed25519 key pair kept as its 32-byte seed in JSON, with `Prove`/`VerifyProof` over a
door's nonce, the session's name and a channel binding (`ChannelBinding`, the SHA-256
of a leaf certificate), `VerifyProofs` over a dial's list and `EncodeKey`/`DecodeKey`,
the roster form; `Signed`, the body as signed and the signature under a domain tag
(`signable`: `vif <domain>\0`, then each field behind its length), so a proof, a
pass, a ticket, a peer link and a succession message under one key never stand in
for each other; `PassBody`, `SignPass`, `VerifyPass` (the term's key looked up by the
term the body names, `PassLifetime` the cap, `Renewable` past half-life) and
`TicketBody`, `SignTicket`, `VerifyTicket` (`TicketLifetime`, a fresh `jti`), both
with `AuthClockLeeway` of skew either way; `SpentTickets`, bounded by
`SpentTicketsKept`; `AdmissionKind` (play, watch, create, publish), by name on the
wire; `SubjectOf` an address as the budgets key it; `AuthResponse` and `AuthRequest`,
the 0x31 and 0x30 bodies, `MaxAuthFrame` their bound, the methods `key`, `pass`,
`ticket` and `scram`; the refusal kinds `RefusalSignIn` (retry after signing in),
`RefusalCredential` and `RefusalDropped` (no retry). And what §7.1 and §7.2 describe,
live: the handshake asks and the dial proves, the roster carries keys, the keeper
keeps them on every host, peer links challenge both ways, reports and handoffs are
signed, a relay carries proofs over its authority's challenge. And §7.3, live: the
seat's offer carries a pass, the keeper keeps it beside the key, a dial shows the one
for the first authority key the door names and every door checks it under the key of
the term it names, the term keys and the keep-out list riding the offer and the
handoff record whole; a drop keeps the dropped key out at every door through its
departure crossing, and the dropped person plays on alone. And §7.4, live: a host
with a passphrase serves its credential's exchange at every door of its own, a dial
given the phrase runs it and verifies the door, bound to the leaf over TLS, a pass
spares it, and a door with no credential in a gated session admits a pass alone.
And §7.5's first half, live: the router reads the edge's PROXY header and ends TLS
where no edge does, runs the door's exchange through itself and mints an anonymous
ticket for the key the dial proved under `-ticket-key`, lists its public half at
`GET /vif/api/keys`, and hands out `vifs://` links where TLS fronts the door; a pod
run with `-admit ticket` takes the router's tickets and no bare key (`TicketGate`).
And §7.5's second half, live: the router forwards the identity service's routes and
asks it which account a sign-in names (`identityClient`), from a native dial's
answer over TLS (`AuthRequest.SignIn`, `Config.SignIn`) or a page's cookie, and the
ticket names the account; the game registers, signs in and out (`network.Register`,
`SignIn`, `SignOut`; `:register`, `:login`, `:logout`), keeps the sign-in per site
(`SignInKeeper`, `signins.json`) and shows it to a TLS door alone; the account and
its name ride the roster entry, the cursor and the pass (D15), and a drop keeps the
account out. Tests, one a rule, and fuzz targets over the token forms, the join
handshake and the peer link.

### 7.1 Participant keys

Landed; [Multiplayer §5.4](multi-player.md#54-wraiths) states it. A run makes a `Key`
for each place as it takes it (`PlaceKeeper`, `place.go`) and its dial proves a fresh
one first, then every kept place of its kind no run holds; the coordinator records
the key on the roster entry (`RosterEntry.Key`), the arrival crosses it, so every
instance, cursor and capture holds it. A peer link is a challenge both ways, signed
over the nonce, both identities and the session; a report and a handoff record carry
their author's signature (`Sign`, `Verify`), checked against the roster's key for
the participant they name. A returning dial proves its key and is seated in the
place that key holds, whatever gate the door keeps; a relay asks the authority for
the challenge its door asks (`RelayNonce`) and carries the proofs on, so it can take
nobody's place. The keeper's store is every host's: a page's `localStorage`, held
by the first tab on the site, a tab beside it keeping its own in `sessionStorage`, a
terminal's `places.json` under the user's state directory (`paths.PlacesFile`, a
file for the user alone), Android's in the app's files, each behind `[session]
resume_after_restart`, on by default.

Cost, as designed: 32 bytes a roster entry, two round trips a peer link, and a
signature of tens of microseconds a link, a dial and a succession message, nothing
a tick. No new dependency.

Not yet: a signed redirect for a merge or a split, and a binding on a wss door's
proof, which a page cannot compute and which §7.4 brings with the SCRAM exchange.

### 7.2 The admission exchange

The door asks after the route frame and the address budget, in place of the claims
step it replaced, so an open door pays no round trip it did not pay before:

```
dialer                                   door
MsgSessionRoute                          -->
                                         <--  MsgAuthResponse{methods, nonce}
MsgAuthRequest{keys}                     -->  an open door: done        (live)
MsgAuthRequest{keys, pass}               -->  a returning participant: done
MsgAuthRequest{keys, ticket}             -->  a fleet join: done
MsgAuthRequest{keys, scram first}        -->  a passphrase door:
                                         <--  MsgAuthResponse{scram challenge}
MsgAuthRequest{scram proof}              -->
                                         <--  MsgAuthResponse{scram final}, or a refusal
                                         <--  the preview, the seat and the offer as today
```

Every answer carries the keys the dial proves (`Proof`), the first its own for a new
place and the rest the places it may come back to, at most `PlacesKept`; a pass or a
ticket will say what admits one of them. A key on the roster names its place, so
nothing is left to claim. A refusal is `RefusalSignIn` when the door takes no bare
key, `RefusalCredential` for what was shown, `RefusalDropped` for a key or account
on the keep-out list; a page shows its reason as it shows every refusal. Both frames
are bounded (`MaxAuthFrame`) and the join handshake's fuzz target scripts them.

Through the router (D5) the same frames will run end to end, the router placing
itself between the question and the answer for one frame each way: it reads the
dialer's answer, forwards a pass whole, and turns a sign-in or nothing into a
ticket. Today it splices them unread.

### 7.3 Session passes

Landed; [Multiplayer §5.4](multi-player.md#54-wraiths) states it. §7.1 brings a
participant back to its own place with one signature while the roster keeps the
place, a Wraith's 2 minutes. A pass carries the admission past that, past the door
that made it, and past the process that held it (D1): a phone the system suspended,
a laptop that slept, a game that crashed, a machine rebooted or a page reopened
comes back without its gate, no passphrase prompt, no KDF, no ticket from the site.

- **Issued** at every seat: the offer carries it (`SessionOffer.Pass`), signed by the
  authority with its own participant key (a fleet pod's is the key it makes at
  start). It names the session, the first term's authority key, so a later session
  under the same name refuses it, the participant's key, the kind, the account and
  name when a ticket named one, the term and an expiry (`PassBody`, `PassLifetime`).
  A redial showing a pass still under half its life takes none in its offer
  (`Renewable`); any other seat takes a fresh one.
- **Shown** by the dial the door asks: the door's question names the session's
  first authority key (`AuthResponse.Authority`), not its name, which the router
  rewrites along a published route, and the dial shows the pass it keeps for that
  key over a key it proves (`ChoosePass`), or none.
- **Checked** at every door of the session (`Coordinator.Pass`): the authority, a
  successor and a relay, each holding the authority key of every term from the
  offer or the handoff record (`SessionOffer.TermKeys`, `HandoffRecord.TermKeys`,
  `network.TermKey`; a record naming another key for a term the receiver lived
  through is refused). The door verifies the pass against that term's key and the
  clock (`VerifyPass`), then the first authority key, then that the key it names is
  among those the dial proved, then the keep-out list: one round trip and two
  signature checks, about a hundred microseconds on a desktop core. A relay checks
  before it carries the dial on, and the authority checks again (`RelayJoin.Pass`).
- **Placed** by its key: back in the place the key holds, or a new place once the
  Wraith was dismissed. At an open door it buys nothing yet; §7.4 and §7.5 put the
  gate it skips in front of it.
- **Revoked** by a drop (`:player drop`), which puts the key, and the account when
  the pass names one, on the keep-out list, and by the session's end. The list is
  session state as the roster and the chain are: the offer and the handoff record
  carry it whole (`KeepOut`, `KeepOutKept`), and the drop's departure crossing adds
  to it on every instance alike (`ParticipantDepartedPayload.Dropped`,
  `NetworkResource.OnDropped`), so a successor's and a relay's doors refuse what the
  authority's does, `RefusalDropped`. A dropped person plays on alone, as after
  `:leave`, told why; a dropped bot's run ends. A fresh key is admitted still: the
  session name is a routing key, and the identity a drop would hold is §7.5's.
- **Kept** beside its key (`place.Pass`), where the keys are kept, on every host.

A pass stolen alone is nothing: every use signs a fresh nonce with the key it
names. A pass and its key stolen together, from a page's storage or a device, are
the participant until the session drops it or ends. Cost: one signature a seat and
two checks a dial that shows one, about 400 bytes a pass in the offer and in the
keeper's file, and the term keys and the keep-out list in each offer and record, 44
bytes an entry.

### 7.4 Passphrase doors

Landed; [Multiplayer §5.4](multi-player.md#54-wraiths) states it. `-passphrase` and
`:host ... passphrase` take a passphrase or make one (D14: `GeneratePassphrase`, 16
characters of `crypto/rand`'s text alphabet, 80 bits, printed once and shown by
`:session`), eight bytes at least (`CheckPassphrase`). The host derives one SCRAM
credential from it when its first door opens (`PassphraseDoor`, `auth.NewCredential`
under the fixed name `door`, Argon2id at `parameter.DoorArgon*`: 64 MiB, three
passes, one lane, which a page and a phone run as a desktop does) and serves its
`ScramServer` at every door of its own; the question names `scram` alone, with
`pass`. A dial given the passphrase (`-passphrase`, `:join <target> <passphrase>`)
opens the exchange in its answer beside its proofs, the challenge comes back in the
door's second question, the proof in the dial's second answer and the door's final in
a third question, which the dial verifies before it reads the preview; the dial's
KDF runs once a join, at the door's cost and no lower than its own (`WithMinArgonCost`).
One with none is refused `RefusalSignIn`, whose advice names the passphrase: the
refusal is the prompt, a dial retries with the phrase. A wrong one is refused
`RefusalCredential`, saying no more. A pass spares the exchange on every redial.

On a TLS door both ends bind the exchange to the leaf certificate the door serves
(`LeafBinding` on the door, the handshake's peer certificate on the dial, under a
websocket too), so a self-signed door is authenticated both ways; on tcp and ws it
authenticates the handshake alone. The offer carries the credential's verifier
(`SessionOffer.Door`, `DoorVerifier`: the salt, the cost and the two keys, never
the phrase), which every participant proved, so a relay's door and a successor's
serve the exchange from it (`OpenDoor`) and a newcomer joins there as at the host's;
the KDF's memory is returned to the system as each derivation ends. A gated door
with no verifier, a fixture's, admits a pass alone (`Coordinator.Gated`). What a
participant is trusted with, the verifier among it, is [the todo's](todo.md#tighten-what-a-relay-and-a-successor-are-trusted-with)
to tighten once the phases are done.

A machine in the middle that answers for the door receives a proof it can test
passphrases against offline, at Argon2's cost a guess: a generated code is past
guessing, a chosen phrase must be long. A door published through the router is
reached over the router's TLS to the site's edge and plaintext on the tunnel, so its
exchange runs unbound there (§8). The cost on a page and a phone is still to be
measured (D18); the sizes were: `auth` adds 166 KiB to the native binary (21.73 to
21.89 MB, stripped) and 220 KiB to the wasm (37.23 to 37.45 MB), under one percent,
so the module stays whole.

### 7.5 Admission tickets and accounts

The router mints tickets in band (D5), signed by a key it alone holds, with
`SignTicket`: the session, the kind, the subject, the dialer's key and a minute.
A pod verifies it with the public key its Job's arguments carry (`-admit ticket
<key>`), holds its session to the pod's own name, spends its id once
(`SpentTickets`) and checks the dialer's proof over the key the ticket names. A
player's host may verify the site's tickets too, from `GET /vif/api/keys`, for a
session open to the site's accounts alone. A ticket admits once; the pass it is
exchanged for admits every redial after.

Anonymous tickets come first, and are landed: their subject is the dial's address
(`SubjectOf`), so the router names the player to the pod, and a pod run with
`-admit ticket` refuses a new admission that went around the router, which D3 leaves
no route for. The router intercepts one frame each way (`tcpRouter.exchange`): the
door's question goes to the dialer, the dialer's answer comes back, its proofs are
checked over the door's nonce, a dial showing no pass is given a ticket, and every
frame after is spliced, a passphrase door's steps among them. The front door reads
the edge's PROXY header, version 1 or 2, when `-route-proxy-protocol` says an edge
fronts it, and ends TLS itself under `-route-cert`; `-route-secure`, implied by the
latter, has every link read `vifs://`, which the game dials with the system's roots
or `-ca`. `vif-router keygen` writes the seed `-ticket-key` names; `GET /vif/api/keys`
lists the public key. Accounts are landed too, in the identity service (D8), whose
routes and introspection are these:

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
phase 1 bumps `snapshot.Schema` and `event.JournalSchema` with the roster, and
phase 2 `event.JournalSchema` with the departure.

- **Phase 0, the direct route goes** (D3): landed. The Service is `ClusterIP`, the
  fleet's size `-sessions-max`, the game port admitted from the node alone.
- **Phase 1, keys and the exchange** (§7.1, §7.2 on player hosts): landed. Keys
  replaced claims, the exchange replaced `MsgJoinClaims`, peer links and succession
  messages are signed, the keeper persists keys on every host behind `[session]
  resume_after_restart`. The router splices the new frames unread; the fleet runs
  open doors still.
- **Phase 2, passes and keep-out** (§7.3): landed. The seat's offer carries a pass,
  every door checks one shown, the term keys and the keep-out list ride the offer
  and the handoff record, `:player drop` keeps the dropped key out and the dropped
  person plays on alone; the router's route for a drop waits for phase 6.
- **Phase 3, passphrase doors** (§7.4): landed. `-passphrase`, `:host ...
  passphrase` and `:join <target> <passphrase>`; the exchange in the admission
  frames, bound over TLS; a pass spares it; relays' and successors' doors gated. The
  binary grew under one percent (D18); the Argon2 cost on a page and a phone is
  measured at deployment.
- **Phase 4, the router's TLS front door, in-band admission and anonymous tickets**
  (D4, D5, D6, §7.5 first half): landed. `vifs://`, the PROXY header on the front
  door, TLS ended there where no edge does, the ticket key and `keygen`, the pod's
  `-admit ticket`, `GET /vif/api/keys`, the edge's stream block in the reference
  edge; the deployment turns the edge on with `VIF_ROUTER_ROUTE_PROXY_PROTOCOL` and
  `VIF_ROUTER_ROUTE_SECURE` once the stream block fronts 7777.
- **Phase 5, accounts** (D8, D9, D15): landed. The identity service in its own
  repository on PostgreSQL in the jail; the router forwards its routes and asks it
  which account a sign-in names, from a native dial's answer over TLS or a page's
  cookie on the upgrade, and the ticket names the account; `:register`, `:login` and
  `:logout` on every host, the sign-in kept per site in `signins.json` or the
  browser's cookie; the account's name on the roster, the cursor and the pass; a
  drop keeps the account out. Budgets by account are phase 6's.
- **Phase 6, the account's reach**: publish and create tickets, budgets by account,
  the operator's loopback routes (D16), spectators.

## 8. What it asks of vif-router and the deployment

By phase:

- **Phases 1–3**: nothing. The router places by the route frame and splices the
  rest unread, so the door's question and the dialer's answer cross it as bytes. A
  router update waits for an empty fleet, so a fleet player never redials across one;
  it does drop published doors' tunnels, and their players come back through the
  resumed publication with their passes. The publication's `Resume` token is then
  the last bearer secret on the wire: phase 6 has a publish carry the run's key and a
  resume sign the router's nonce.
- **Phase 4**:
  - The edge terminates TLS for 7777 in an nginx `stream` block and forwards to the
    node's 7777 with `proxy_protocol on`; the router reads one PROXY header before
    the route frame (`-route-proxy-protocol`, refusing a connection without one), the
    v1 text line nginx writes or the v2 binary header HAProxy does, and
    keys its budgets on the address it names. A deployment with no such edge gives
    the router `-route-cert` and `-route-key`, off by default, and it ends TLS itself.
    The game's `vifs://` dials TLS with the system's roots, as a wss door is dialled.
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
    the forward never reaches. No new public port and no filter change; the
    service's own deploy steps (its `deploy/README.md`) install and update it
    without the empty-fleet gate, since its restart pauses only sign-ins and
    account tickets.
  - Its store is the jail's PostgreSQL (D9): a database and schema of the service's
    name, a role for it with a SCRAM-SHA-256 password read from a 0600 file, reached
    over the host's internal network with `hostssl` in `pg_hba.conf` and the jail's
    certificate pinned or signed by an issuer the service is given; `statement_timeout`
    and its siblings on the role as chess sets them; `pg_dump` nightly on the jail.
    A deployment too small for a jail, or a test node, gives it `-store-file`
    instead: one JSON file under its state directory, the same operator commands,
    the same routes to the router.
    The database is the host's to provision; the service ships the SQL, the schema
    migrations and the backup script.
  - The router forwards `/vif/api/auth/` byte for byte (`log_proxy.go`'s shape) and
    asks introspection once per account ticket. A page's sign-in is a cookie scoped
    to `/vif/`, which the `/vif/ws/` upgrade carries, and the router mints from a
    cookie only on a request whose `Origin` is `-web-origin`.
  - The public log stream carries the sessions' logs, which name a participant by
    slot, never by account; the service's logs stay off it and hold no credential.
- **Phase 6**: the operator routes, dropping a participant and draining, on a
  loopback listener of the router's own (D16).

What the site's edge then provides, beside [Deploying §12](kube-docker-deploy.md#12-the-sites-edge):
a `stream` server on 7777 ending TLS and forwarding with PROXY protocol to the
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

## 10. File catalog

What an implementation reads and changes, by area. Paths are from each
repository's root.

The session layer, `internal/network`:

- `admission.go`: the keys, proofs, tokens, frames and spent set (§7.0).
- `protocol.go`: the kinds, `MsgAuthRequest`, `MsgAuthResponse`, `MsgSessionRoute`,
  `MsgConnect`/`MsgAck`, the tunnel's kinds.
- `session.go`: `RosterEntry`, `SessionOffer`, `JoinerReport`, `Coordinator`,
  `Admission`, `HostAcceptor`, `readSessionName`, `askAdmission`, `answerAdmission`,
  `RefusalCode` and its table, `openJoinStream`, `RelayJoin` and its steps,
  `AdmissionLimiter`.
- `reach.go`: `PeerKeys`, `PeerAcceptor`, `PeerDialer`, `peerHello`, `peerWelcome`,
  `peerBind`, `SuccessionChain`.
- `authority.go`: `AuthorityReport` and `HandoffRecord` with their `Sign` and
  `Verify`; §7.3 reads term keys from the record.
- `identity.go`: `ProtocolVersion`, `PeerIdentity`.
- `tunnel.go`: `PublishRequest`, `Published`, `TunnelHub`, `DoorTunnel`, the resume
  token.
- `config.go`: `Config.Keys`, `Config.Identity`, `RequestSession`.
- `fuzz_test.go` and `admission_test.go`: the handshake, peer link and token fuzz
  targets.

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
- `internal/event/payload.go`: `ParticipantJoinedPayload` and
  `CursorSpawnRequestPayload`, which carry a participant's key; `component.Cursor.Key`.
- `internal/snapshot/capture.go`: `Schema`.

Membership, `internal/app/internal/membership`:

- `place.go`: `PlaceKeeper`, which keeps keys and will keep passes.
- `identity.go`: `buildIdentity`, `joinerReport`.
- `session.go`: the coordinator's `HostAcceptor` wiring, `keyedLocked`, `ownKey`,
  `LocalKey`, `closeStaleLink`, `yieldLocked`, `redialSeat`.
- `relay.go`: a relay's acceptor, `relayNonce`, and the proofs it carries to the
  authority, which `issueRelayNonce` and `relayedKeys` check.
- `converge.go`: `redial`, `redialDoor`, where a pass is shown.
- `host.go`, `join.go` and `seat.go`: hosting, dialling, and a seat's places.
- `internal/app/internal/config/membership.go`: `Membership`, `PlaceStore`.

Parameters, paths, settings and commands:

- `internal/parameter/network.go`: `PlacesKept`, `PlaceStoreInterval`,
  `WraithTimeout`, `NetworkAdmitWindow`, `NetworkAdmitBurst`,
  `NetworkAdmitTracked`, `NetworkTunnelResumeWindow`,
  `NetworkSessionRequestTimeout`, `AuthNonceBytes`, `TicketLifetime`,
  `SpentTicketsKept`, `PassLifetime`, `AuthClockLeeway`.
- `internal/paths/paths.go` (`PlacesFile`, `PlaceFile`, and `stateDir`, where a
  terminal's sign-in would go) and `settings.go` (`Session`, `resume_after_restart`).
- `internal/mode/commands.go`, `config_menu.go` and `config_form.go`: `:host`,
  `:join`, `:session`, the Multiplayer page and its request form, where `:login`
  and a passphrase prompt go.
- `cmd/vif/main.go` and `usage.go`: the `-host`, `-serve`, `-join`, `-name`,
  `-cert`, `-key`, `-selfsign`, `-ca`, `-relay`, `-authority`, `-passphrase` and
  `-admit` flags and the help table the manual is made from; `cmd/vif/browser.go`:
  a page's `PlaceStore`.

Hosts:

- `web/terminal.js` and `web/worker.js`: the site's places in `localStorage` under
  a Web Lock, a second tab's in `sessionStorage`, the launch arguments.
- `android/game/game.go`, and `StartScreen.java` and `MainActivity.java` under
  `android/app/src/main/java/com/lixen/vif`: the Android run, whose places are in
  the app's files.

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
