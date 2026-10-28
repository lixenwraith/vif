# Telemetry reference

This reference covers the status-registry telemetry of every system `manifest.BuildSystems` constructs, `MetaSystem` and `BehaviorSystem` (`manifest.ContextSystems`), the session layer (`internal/snapshot`, `internal/converge` and the content service), and the telemetry `internal/engine` and `internal/event` own. For each owner it records the keys, where they are registered, written and reset, and what reads them by name.

## The registry

`status.Registry` holds four maps of atomic cells: `Bools`, `Ints`, `Floats` and `Strings`. An owner registers a key by calling `Get` before `Scheduler.Prepare` calls `Registry.Freeze`. After the freeze, a `Get` of a new key returns a detached cell that no snapshot sees and counts it in `stat.late`. Constructors register; `Init` and the reset paths only store.

**Consumers.**

- **Generic.** The status snapshot, the telemetry overlay and pinned cards, the flight recorder, `vif-log`'s registry traversal, the probe's `/metrics`, and `:d break stat <key>`, which tests whatever cell an operator names. The probe exports ints, floats and bools as `vif_<key>` gauges and omits strings. "Generic only" in the tables below means nothing outside the producer reads the key by name.
- **Named.**
  - The status bar (`NewStatusBarRenderer`).
  - The `:session` line (`membership.sessionSummaryLocked`) and the `:content` command.
  - The journal anchor and capture header, which carry `content.*`.
  - Scenario FSM guards and actions (`StatusIntCompare`, `StatusBoolEquals`, `StatusToVar`).
  - Bot graph conditions (`StatusInt`, `StatusBool`), which read the bot's own registry.
  - The config menu, the seat and script hosts (`forkCell`), `RunBot`'s `bot stopped` record, the `:d` report, and `:d break region`. The tables below name each one's keys.
- **Comparison surfaces** (`internal/snapshot/surface.go`).
  - `SharedKey` selects the keys two instances must agree on. It denies everything `SimDeniedKey` denies, the player-domain, transport, aggregate and per-instance prefixes and keys, every `.buf_*_hwm` and every `.protected_player_rejects`. It re-admits `spatial.indexed_shared`. A shared capture carries exactly these cells and installs them last (`corrector.captureStatusLocked`, `installStatusLocked`), and the runtime parity digest hashes the same surface (`snapshot.SharedSurface`).
  - `SimDeniedKey` selects the keys a replay does not compare: the pacing, render and telemetry-about-telemetry keys in `deniedSimKey`, plus the `snapshot.`, `prof.` and `proc.` prefixes.

**Slots.** A per-slot metric (`status.NewPlayerInt` or `NewPlayerBool`) registers `player.<slot>.<suffix>` for slots 0–15 (`parameter.MaxPlayers` is 16). It can also register a bare key that mirrors the slot this instance drives, set by `Registry.SetLocalSlot`. With no local slot, the bare key mirrors nothing and reads zero.

- Bare keys are instance-local and never a shared FSM guard input (D-20).
- The mirrored bare keys are `energy.current`, `heat.{current,overheat,at_max,ember}`, `shield.active`, `boost.{active,remaining}`, `typing.max_streak`, `weapon.<kind>`, `weapon.orbs` and `player.{x,y,entity}`.
- Energy, shield, heat, boost and weapon (its held kinds) publish every roster slot on each update through `eachRosterSlot`, a peer's cursor included, and zero a slot whose cursor is gone. `MetaSystem` writes -1 to `player.<slot>.{x,y}` for an empty slot.

**Cards.** `status.SplitKey` projects keys into cards of at most `OverlayCardMaxEntries` (15) fields, and `TestTelemetryGroupsFitOverlayCards` enforces that limit. Visibility gates apply to views, snapshots and recorder windows:

- A slot's cards (`player.<slot>`, `player.<slot>.weapon`) are kept only while `player.<slot>.entity` is non-zero.
- The `combat.absorbed.*`, `combat.damage.*`, `prof*` and `proc` cards are kept only while a member is non-zero.

## Registration and reset rules

- **Freeze timing.** Every key registers before the freeze:
  - Schema-derived `fsm.<region>.*` keys bind in `Scheduler.initLoadedScenario`, because region names exist only once a scenario loads. That is still before `Prepare`.
  - Behavior program keys (`behavior.<program>.*`, `kills.<program>`) bind in `NewBehaviorSystem` and `NewMetaSystem` from the programs `App.readScenario` compiled before the world was built.
  - The position store binds right after `newGameContext` creates the registry, because `World` builds the store before the registry exists.
  - `NewRegistry` reserves `stat.late` and the recorder's `rec.*`, so a recorder enabled after the freeze is still visible.
  - `Freeze` reserves `stat.groups` and `stat.metrics`.
  - `TestTelemetryRegistryStaysFrozenAcrossTicksAndReset` asserts that nothing registers late.
- **Shared helpers.** `internal/system/telemetry.go` holds the helpers that produce the repeated key families listed under [Key families](#key-families): `newLifecycleTelemetry`, `newBounceTelemetry`, `newRejectionTelemetry` and `newBufferTelemetry`.
- **Fresh allocations.** `hitComposite.members` in the explosion path is a fresh slice inside a queued payload, not a reusable buffer. Reusing it would corrupt queued events, so it has no high-water metric.
- **Throttled publication.** Expensive formatting runs on a cadence rather than every tick:
  - The event-type strings and spatial gauges publish every `parameter.StatSnapshotTicks` (200).
  - `eye.ga.typefit` publishes on the same cadence.
  - The `adapt.{graphs,populations}` gauges and `adapt.g*` strings publish every `parameter.AdaptTelemetryInterval` (10) ticks.
- **Reset test.** `TestTelemetrySessionCountersReset` seeds every int, bool and string, runs a game reset, and requires zero, the reset baseline, or a listed bootstrap value. Floats are not checked.
- **Keys that survive a reset.**
  - Prefixes: `content.`, `rec.`, `stat.`, `snapshot.` (except `predictions_pending`), `prof.`, `proc.`, `network.term*` and `network.handoff*`.
  - Session keys: `network.{authority,migrations,fork,migrating,evicted,listening,chain,host_lost}`.
  - Operator and context keys: `context.{frame,screen_w,screen_h,mode}` and `engine.{fps,speed_pct,speed}`. The test also exempts `engine.breakpoint`, because its seeded value has no armed request behind it; at runtime a reset cancels the armed run-until request and the key reads `-`.
- **Metric-free systems.** Systems with no counters and no reusable allocation-shaped buffers register nothing.

## System telemetry

Rows follow `manifest.BuildSystems` order, then `MetaSystem` and `BehaviorSystem`. `Init` runs from the engine's pass (`World.InitSystems`) once the set is built, and again on a game reset, in dependency order.

| Owner | Metric keys | Registered | Written | Reset | Named consumer |
|---|---|---|---|---|---|
| Network | `network.*` connection, barrier, relay, parity, link and pacing keys; `context.map_locked` | `NewNetworkSystem`; `context.map_locked` by `newGameContext` | See [Session telemetry](#session-telemetry) | `Init` | See [Session telemetry](#session-telemetry) |
| Cursor | `player.{count,local,entity,cursor_rejects,spawn_failures}`, `player.<slot>.{entity,control}` | `NewCursorSystem` | Spawn failures, move rejections, and `publishRoster` on every roster change | `Init` | Slot cards gate on `player.<slot>.entity` |
| Ping | `ping.{cursor_rejects,disabled_rejects}` | `NewPingSystem` | Request rejection paths | `Init` | Generic only |
| Transient | `effects.{grayout_active,strobe_active}`, `transient.{explosion_merged,explosion_evicted}` | `NewTransientSystem` | Effect handlers and update; the explosion table's merge and eviction paths | `Init` | Generic only |
| Camera | None | — | — | — | — |
| Energy | `energy.{current,damage_multiplier,penalty_count,reward_count,spend_count,passive_count,passive_drained,crossed_zero_count,penalty_rejects,cursor_rejects,missing_energy_rejects,disabled_rejects}`, `player.<slot>.energy.current` | `NewEnergySystem` | Resolved energy deltas and passive drain. `damage_multiplier` is written only by `MetaSystem` | `Init`; a despawned cursor's slot is zeroed | `damage_multiplier` scales penalties in `EnergySystem` and is shown by the status bar |
| Shield | `shield.{active,shield_hit,cursor_rejects,disabled_rejects}`, `player.<slot>.shield.active` | `NewShieldSystem` | Resolved shield handlers and update | `Init`; a despawned cursor's slot is zeroed | Generic only |
| Heat | `heat.{current,overheat,at_max,ember,cursor_rejects,disabled_rejects}`, `player.<slot>.heat.{current,overheat,at_max,ember}` | `NewHeatSystem` | Resolved heat handlers and update | `Init`; a despawned cursor's slot is zeroed | Generic only |
| Boost | `boost.{active,remaining,truncated,cursor_rejects,disabled_rejects}`, `player.<slot>.boost.{active,remaining}` | `NewBoostSystem` | Resolved boost handlers and update | `Init`; a despawned cursor's slot is zeroed | Generic only |
| Weapon | `weapon.{<kind>,orbs,orbs_reaped,tracers,tracer_{wall_collisions,boundary_steps,grid_steps},buf_{tracers,glyphs}_hwm}`; `weapon.{main,<kind>}_fired` (card `weapon.fired`); `weapon.{kind,cursor,disabled}_rejects` (card `weapon.rejects`); `player.<slot>.weapon.{<kind>,orbs}`. Kinds are the `WeaponSpecs` names: `disruptor`, `emitter`, `gun`, `launcher`, `rod`, `turret` | `NewWeaponSystem` | Resolved fire and grant handlers, the orb reap, and the tracer sweep | `Init`; a despawned cursor's slot is zeroed | Generic only |
| Typing | `typing.{correct,errors,max_streak,buf_delete_hwm,cursor_rejects,disabled_rejects}`, `player.<slot>.typing.max_streak` | `NewTypingSystem` | Resolved typing and delete paths | `Init`; a despawned cursor's slot is zeroed | `RunBot`'s `bot stopped` record logs `typing.{correct,errors}` |
| Composite | None | — | — | — | — |
| Wall | `wall.{enabled,count,push_events,buf_pending_push_checks_hwm}` | `NewWallSystem` | Wall handlers, update, cave steps and buffer observation; `push_events` counts cursor and Shared displacements alone, as instances compare it | `Init` | Generic only |
| Tower | `tower.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `NewTowerSystem` | Spawn, cancel and death handlers; update | `Init` | Generic only |
| Gateway | `gateway.{active,count}` | `NewGatewaySystem` | `Update` | `Init` | Generic only |
| Loot | `loot.{drops,active,collects,unreachable,wall_collisions,boundary_reflections,physics_steps,buf_pity_hwm}` | `NewLootSystem` | Drop and collect handlers; per-tick `active` and `unreachable`; owner-route maintenance; bounce integration | `Init` | Generic only |
| Glyph | `glyph.{enabled,next_spawn_ms,orphan_glyph,buf_placement_hwm}`; floats `glyph.{density,rate_mult}` | `NewGlyphSystem` | Spawn pacing in update and the enable command | `Init` | Generic only; `density` counts the kind being placed, scenery or typeable. The spawner reads `environment.drift_*` |
| Nugget | `nugget.{active,spawned,collected,jumps,spawn_failures,cursor_rejects,disabled_rejects}` | `NewNuggetSystem` | Resolved nugget handlers and update | `Init` | Generic only |
| Particle | `{dust,decay,blossom}.{count,applied,wall_collisions,boundary_hits,grid_steps,protected_rejects,buf_hit_entities_hwm,buf_processed_cells_hwm}`; `dust.{created,destroyed,centers_dropped,buf_{centers,flash,impulses,transform}_hwm}`; `particle.{cursor_rejects,disabled_rejects}` | `NewParticleSystem` | Behavior-selected spawn, apply, update and detonation paths | `Init` | Generic only |
| Gold | `gold.{active,header_entity,timer,spawn_failures,cursor_rejects,disabled_rejects}` | `NewGoldSystem` | Resolved sequence handlers and update | `Init` | Generic only |
| Materialize | None | — | — | — | — |
| Fuse | `fuse.{spawn_failures,disabled_rejects}` | `NewFuseSystem` | Fuse request failure paths | `Init` | Generic only |
| Spirit | None | — | — | — | — |
| Lightning | None | — | — | — | — |
| Missile | `missile.{count,spawned,impacts,expired,wall_collisions,boundary_hits,grid_steps,disabled_rejects}` | `NewMissileSystem` | Resolved spawn, impact and expiry; swept update | `Init` | Generic only |
| Navigation | `nav.{entities,recomputes,roi_cells,buf_groups_hwm}` | `NewNavigationSystem` | Wall-change ROI accumulation, per-tick flow-field rebuild count, and group observation | `Init` | Generic only |
| Soft collision | `soft_collision.{collisions,immune_rejects,buf_{drains,swarms,quasars,storms,pylons}_hwm}` | `NewSoftCollisionSystem` | Resolved collision pass; buffer observation | `Init` | Generic only |
| Combat | `combat.{active,count,hits_direct,hits_area,knockbacks,stuns,burn_ticks,damage_dealt,immune_rejects,unprofiled}`; `combat.{attacker,container,cursor,disabled,kinetic_immune,relation,stun_immune,target}_rejects`; `combat.effect_{kinetic,stun,burn,energy_drain}`; `combat.chain_{followups,depth_max,depth_total}`; `combat.live_<type>`; `combat.{damage,absorbed}_{attacker,defender}_<type>`; `combat.damage_family_<family>` | `NewCombatSystem` | Resolved direct, area and over-time attacks; status ticks | `Init` | Generic only |
| Drain | `drain.{count,pending,paused,collisions,suicides,spawned,fusions,despawned,spawn_failures,killed_by_player,killed_by_lifecycle,wall_collisions,boundary_reflections,grid_steps,protected_rejects,buf_{drain_cache,pending_spawns}_hwm}` | `NewDrainSystem` | Spawn, lifecycle, collision and movement paths | `Init` | Generic only |
| Quasar | `quasar.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewQuasarSystem` | Spawn, lifecycle and bounce paths | `Init` | `quasar.active` is a shipped FSM guard (the `LeftMap` and `Slain` macros) |
| Swarm | `swarm.{active,count,transition_stalls,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewSwarmSystem` | Spawn, lifecycle, state-transition and bounce paths | `Init` | Generic only |
| Storm | `storm.{active,circle_count,red_active_frames,green_active_frames,blue_active_frames,nudge_count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects,buf_{ellipse_offsets,member_excludes}_hwm}` | `NewStormSystem` | Spawn, lifecycle, 3-D physics and update paths | `Init` | `storm.active` is a shipped FSM guard (`storm.toml`, the `LeftMap` and `Slain` macros) |
| Kraken | None | — | — | — | — |
| Pylon | `pylon.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `NewPylonSystem` | Spawn, cancel and death handlers; update | `Init` | Generic only |
| Snake | `snake.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewSnakeSystem` | Spawn, lifecycle and bounce paths | `Init` | Generic only |
| Eye | `eye.{count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewEyeSystem` | Spawn, lifecycle and bounce paths | `Init` | Generic only |
| Mount | `mount.{count,fired,host_rejects,disabled_rejects}` | `NewMountSystem` | Attach requests and the per-mount fire pass | `Init` | Generic only |
| Bullet | `bullet.{count,spawned,hits,wall_collisions,boundary_hits,grid_steps,disabled_rejects}` | `NewBulletSystem` | Spawn, swept update, and cursor, shield or target hits | `Init` | Generic only |
| Flash | None | — | — | — | — |
| Fadeout | None | — | — | — | — |
| Marker | None | — | — | — | — |
| Button | None | — | — | — | — |
| Explosion | `explosion.{triggered,cursor_rejects,disabled_rejects,buf_composites_hwm}` | `NewExplosionSystem` | Explosion request handler and the composite pass | `Init` | Generic only |
| Motion marker | `motion_marker.buf_{base_markers,base_positions,colored_markers}_hwm` | `NewMotionMarkerSystem` | Marker-buffer observation | `Init` | Generic only |
| Splash | None | — | — | — | — |
| Environment | `environment.{wind_active,drift_active,drift_scenery}`; floats `environment.{drift_direction,drift_speed}` | `NewEnvironmentSystem` | Update gauges | `Init` | `drift_*` steer the glyph spawner's entry edge, its pace and the kind it places |
| Death | `death.{killed,tagged,batch_{count,entities_total,size_max,silent,flash,blossom,decay,fadeout,dust,other},protected_rejects,zero_rejects,missing_entities,missing_effect_data,payload_rejects,disabled_rejects,buf_destroy_hwm}` | `NewDeathSystem` | The `EventDeathBatch` handler and the `DeathComponent` sweep in `Update` | `Init` | Generic only |
| Timer | None | — | — | — | — |
| Adaptation | `adapt.{graphs,populations,buf_*_hwm}`; strings `adapt.{g1,g2,g3,g4}` | `NewAdaptationSystem` | Graphs, populations and the `g` strings every `AdaptTelemetryInterval` ticks; buffer observation as it runs | `Init` | Generic only |
| Genetic | `eye.ga.{generation,best,avg,pending,outcomes,tracked}`; string `eye.ga.typefit`; `eye.buf_ga_{pending_deaths,track_keys,tracking,typefit}_hwm` | `NewGeneticSystem` | GA processing; `typefit` every `StatSnapshotTicks` | `Init` | Generic only |
| Audio | `audio.{backend,silent,played,dropped,mask,sfx_muted,music_muted}`, `audio.rej_{bad_id,muted,not_running,paused,queue_full,silent}` | `NewAudioSystem` | `Update`, from backend totals; `publishAudioMask` (audio and music systems) for the mask keys | `Init` takes backend baselines; counters publish deltas from them | The status bar and config menu read `audio.mask`; the config menu also reads `audio.silent` |
| Music | Strings `music.{group,tier,rhythm,melody,fill,bass}`; `music.bpm` | `NewMusicSystem` | Update, stop and mute, from the sequencer readout | `Init` clears them | Generic only |
| Meta | `context.{map_w,map_h,camera_x,camera_y}`, `player.<slot>.{x,y}`, `kills.{drain,eye,kraken,pylon,quasar,snake,storm,swarm,tower,total,uncredited}`, `kills.<program>` for each behavior program, `session.{any_defeated,all_defeated}`, `energy.damage_multiplier` | `NewMetaSystem` | `Update` (map, camera, placement), kill and defeat handlers, the cycle damage-multiplier events, and `LoadShared` | `Init`; kill tallies, the defeat latches and the multiplier are carried by its D-19 record | `session.any_defeated` and `kills.{drain,quasar,storm,kraken}` are shipped FSM guards, and those kill keys are also `StatusToVar` sources; the status bar reads `energy.damage_multiplier` |
| Behavior | `behavior.{active,count,unknown_program_rejects,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}`; per program `behavior.<program>.{count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `NewBehaviorSystem`, from the scenario's compiled programs | Spawn, cancel, lifecycle and bounce paths; the counts every `Update`, and a spawn's or an entrance's at once | `Init` | `behavior.count` is a shipped FSM guard (`wad/scenario/vanguard`) |

Audio-free builds (`vif_headless`, `vif_noaudio`) use `audio_none.go`. There, Audio registers only `audio.mask`, held at -1, and Music registers nothing.

## Session telemetry

These owners describe this instance's place in a session. The `NetworkSystem` rows reset with its `Init`; the authority, reachability and `snapshot.` rows survive a game reset.

| Owner | Metric keys | Registered | Written | Reset | Named consumer |
|---|---|---|---|---|---|
| Connection | `network.{peers,connected,state,map_latched}`, `context.map_locked` | `NewNetworkSystem` (`map_locked` by the game context) | `publishConnectionTelemetry` every poll | `Init`; `state` rests at `off`. `MetaSystem.handleGameReset` republishes `map_locked` | Status bar: `state`. `:session`: `peers`, `map_latched` |
| Barrier and crossings | `network.{crossings_sent,crossings_received,state_applied,frames_dropped,corrections_received,join_lag_ticks,lag_ticks,stale}`, `network.barrier_{deferred,applied_local,applied_peer,late,delay_ticks,ran_without_peer,peer_lag_ticks,peer_artifacts,peer_applied}`, `network.artifacts_{refused,refused_tick,schedule_full,pre_install,authority_superseded,restored}`, `network.commit_{late,void,refused_raw}` | `NewNetworkSystem` | Transport polling, admission, playout, commit and the per-tick lag measurement; `join_lag_ticks` once a join catches up | `Init` | Status bar: `stale`, `lag_ticks` |
| Relay and transport | `network.{relay_forwarded,relay_duplicates,relay_refused,transport_lost_in,transport_lost_out}` | `NewNetworkSystem` | Flood relay of artifacts, corrections and departures; raw artifacts a run does not carry; `publishTransportLoss` | `Init` | Generic only |
| Link measurement | `network.link_{rtt_ms,jitter_ms,bps,loss_pct,peer,saturated}` | `NewNetworkSystem` | `publishLinkMeasurement` every tick, copied from the transport's per-peer estimate | `Init` | Status bar: `link_rtt_ms`, `link_loss_pct`. `:session`: all but `link_saturated` |
| Pacing | `network.pace_{late_ticks,trim_permille,steps}` | `NewNetworkSystem` | Guest tick pacing against authority epochs | `Init` | Generic only |
| Link bytes | `network.sent_{epoch,sync,digest,correction,repair,control}_bytes`, `network.wire_bytes_{out,in}` | `NewNetworkSystem` | `publishLinkBytes` every tick, from the socket port's totals over every link it has had; an in-process mesh writes none | `Init` | The `network.wire` card; [Netcode design §3](netcode-design.md#3-readings) |
| Authority | `network.{term,authority,migrations,term_refused,fork,migrating,host_lost,evicted}`; `network.{term_stale,handoff_bytes}` | `newAuthority` through `converge.New` during App construction; `snapshot.NewTelemetry` registers `term_stale` and `handoff_bytes`. `NewNetworkSystem` also binds `host_lost` | Offers, handoffs, refusals, succession and eviction. `NetworkSystem.reportDisconnect` raises `host_lost` | Not reset: a game reset does not undo a handoff, a fork or a lost authority | Status bar: `host_lost`, `migrating`. `:session`: `host_lost` and `Authority.Summary`. `network.fork` is read by the seat and script hosts (`forkCell`) |
| Reachability | `network.{chain,rejoin_attempts,listening}` | `newReach` through `converge.New` | `Reach.drive` between ticks (`chain`, `rejoin_attempts`); `AdoptListener` (`listening`) | `chain` and `listening` are not reset; `drive` zeroes `rejoin_attempts` whenever no succession is contested | `:session`: `chain`, `listening`. Status bar: `rejoin_attempts` in the `Migrating` badge |
| Capture and correction | `snapshot.{bytes,capture_us,encode_us,stage_us,commit_us,install_tick,catch_up_ticks,keyframes,projected_ticks}`, `snapshot.corrections_{sent,applied,refused,superseded,held,jumped}`, `snapshot.correction_{bytes_sent,entries,entities,cells,tick}` | `snapshot.NewTelemetry` during App construction | Capture reads, publications and installs | Not reset: a game reset does not undo a join | `NetworkSystem` reads `correction_entities` into its link report |
| Selective correction | `snapshot.{manifests_sent,manifests_received,manifests_off_tick,manifest_bytes_sent,manifest_bytes_received,corrections_hash_only,sections_compared,pages_compared,hash_us,selective_bytes,request_bytes}`, `snapshot.shards_{requested,sent,received,refused,applied}`, `snapshot.shard_bytes_{sent,received}`, `snapshot.{pages,entities,cells}_repaired`, `snapshot.{proof_failures,baseline_refusals,keyframe_fallbacks}` | `snapshot.NewTelemetry` | Every index published or answered and every repair served or applied | Not reset | Generic only; split across the `snapshot.index` and `snapshot.repair` cards |
| Relay retention | `snapshot.relay_{retained,served,unserved,bytes_sent,bytes_received}` | `snapshot.NewTelemetry` | Repairs a relaying participant serves or refuses from its own retention | Not reset | Generic only |
| Replay suffix | `snapshot.replay_{suffix_records,records,overflow,skipped,suffix_unavailable}` | `snapshot.NewTelemetry` | Every correction that replays, or declines to replay, the local suffix | Not reset | Generic only |
| Correction cadence | `snapshot.cadence_{ticks,keyframe_interval,keyframe_period_ticks,keyframe_age_ticks,uplink_bps,budget_bps,floor_bps,links,constrained_links,constrained,floor_breached}` | `snapshot.NewTelemetry` | Every publication decision, and every correction a guest applies | Not reset | Status bar: `cadence_ticks`, `cadence_constrained`, `cadence_floor_breached`. `:session`: the operating point |

The whole `snapshot.` prefix is outside both comparison surfaces. A host publishes what a read cost, and a guest publishes what an install cost and how far its prediction drifted. Under weakened D-11, two instances do not agree on those values. The `network.` prefix is outside the shared surface because a peer's counters are the complement of this instance's. Link timing is copied from the transport and never derived by a system, so a round trip stays out of the simulation (D-24). `TestAHealthyLinkKeepsTheNominalPointAndHidesItsTiming` asserts that no cadence or link key is compared.

## Engine, event and process telemetry

| Owner | Metric keys | Registered | Written | Reset | Named consumer |
|---|---|---|---|---|---|
| Game context | `engine.fps`, `context.{frame,screen_w,screen_h,map_locked,mode}` | `newGameContext` | The render loop (`fps`, `frame`), resize, mode change, and the map lock (game context and `NetworkSystem`) | A game reset does not clear them; the owners rewrite them | Status bar: `engine.fps` |
| Time control | `engine.{speed_pct,speed,step,breakpoint,paused}` | `NewTimeControl` | Pause, rate, step and breakpoint changes under the time-control lock | `speed_pct` and `speed` survive both reset forms, except that cancelling an armed run-until request reinstates the rate it was armed from. `breakpoint` returns to `-` and `step` to 0, because the reset cancels both requests | Status bar: `speed`, `step`, `breakpoint` |
| Scheduler / world | `engine.{ticks,input_horizon,apm,music_apm,tick_slips}`, `entity.{count,created_total,destroyed_total}`, `time.game_elapsed_ms`, `event.{backoffs,dispatches,dead,dropped,invalid,queue_len,queue_max,settle_exhausted}`, `event.settle_{pre,post,loop,reset,input,settle,wire,close}`, `fsm.{state,elapsed,max_duration,state_index,state_count}` | `NewScheduler` | Dispatch and the tick tail, under the world lock; `engine.input_horizon` when a tick opens and before the machine boots or resets; the `fsm.` summary after the FSM update and on an FSM import. A capture install stores `engine.ticks` and `time.game_elapsed_ms` (`corrector.adoptClockLocked`) | `Scheduler.resetTelemetry` from `executeReset` | Status bar: `engine.ticks`, `engine.music_apm` and the five `fsm.` summary keys |
| FSM regions | `fsm.<region>.{state,index,elapsed,max_duration,paused}` for each region of the loaded scenario | `bindFSMTelemetry` from `initLoadedScenario` | After the FSM update each tick, and on an FSM import (`ImportFSM`) | `resetTelemetry` | `:d break region` resolves its region name against the `fsm.<region>.state` keys |
| Event queue | Per-type `[EventTypeCount]atomic.Int64` dispatch and dead-letter arrays, surfaced as strings `event.{dispatch_by_type,dead_by_type}` | Arrays in `NewEventQueue`; strings in `NewScheduler` | The scheduler records after routing; the strings publish every `StatSnapshotTicks` | `Queue.ResetTelemetry` after the stale-event drain | Generic only |
| Prediction ledger | `snapshot.predictions_{pending,confirmed,dropped}` | `World.BindPredictionTelemetry` in `newGameContext` | `recordPrediction`, `settle` and `confirm` | `ResetPredictedDeaths` (from `NetworkSystem.Init`) zeroes `pending`; the other two are not reset | Generic only |
| Position / spatial grid | `spatial.{cell_saturations,cell_overflows,player_budget_rejects,occupied_cells,indexed_entities,indexed_shared,unindexed,max_cell_occupancy,cell_occupancy_hwm,positions_hwm,position_batch_hwm}` | `Positions.BindTelemetry` in `newGameContext` | Position mutations under the world lock. Gauges publish every `StatSnapshotTicks` and after a capture install | `World.Clear` | `spatial.indexed_shared` is the one compared `spatial.` key |
| Profiler | `prof.{<phase>_us,tick_max_us,frame_max_us}`, strings `prof.top.<NN>`, `proc.{cpu_user_pct,cpu_sys_pct,rss_peak_mb,mem_mb,heap_live_mb,alloc_mb_s,gc_s,allocs_s,goroutines,io_read_s,io_write_s,csw_s,preempt_s}` | `prof.New` in `newGameContext` | Once per `ProfWindow` of running game while `:d prof` is on; zeroed when it stops | Not reset | `:d prof` pins the `prof` and `prof.top` cards; the `:d` report shows the `prof` and `proc` groups |
| Corpus | `content.{source,files,blocks,lines,rejected,served,file}` | `ContentService.PublishStatus` during App construction | The fingerprint once at publication; `served` and `file` on every `NextBlock` | Not reset | The journal anchor and replay verification, the capture header, and `:content` |
| Registry | `stat.{late,groups,metrics}` | `NewRegistry` (`late`) and `Freeze` (`groups`, `metrics`) | `Registry.Tick` after every completed tick (`late`, once non-zero) and `Freeze` | Not reset | Excluded from replay comparison |
| Flight recorder | `rec.{depth,flushes,records,skipped}` | `NewRegistry` | The recorder | Not reset | Excluded from replay comparison |

## Key families

These patterns account for most keys. The [key notes](#key-notes) cover individual keys whose meaning a pattern does not convey.

| Pattern | Producer | Meaning |
|---|---|---|
| `<d>.{spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `newLifecycleTelemetry`: drain, eye, pylon, quasar, snake, storm, swarm, tower, and `behavior.<program>` | Instances created, removed by cancellation or integrity cleanup, and killed. A kill counts as `killed_by_player` when its killer resolves to a roster cursor (`ResolveCursor`), otherwise as `killed_by_lifecycle`. `spawn_failures` counts requests that produced no entity |
| `<d>.{wall_collisions,boundary_reflections,physics_steps}` | `newBounceTelemetry`: behavior, eye, loot, quasar, snake, storm, swarm | Resolved wall contacts, reflections at the simulation bounds, and physics integration substeps |
| `<d>.{wall_collisions,boundary_hits,grid_steps}` | Swept movers: blossom, bullet, decay, dust, missile (drain uses `boundary_reflections`) | Wall contacts, swept paths ended at the bounds, and grid-traversal steps |
| `<d>.cursor_rejects`, `<d>.disabled_rejects` | `newRejectionTelemetry` and per-system cells | Requests dropped because no roster cursor resolved, or because the system was disabled. `combat.cursor_rejects` is the exception: see [key notes](#key-notes) |
| `<d>.protected_rejects`, `<d>.protected_player_rejects` | Death, drain, the particle behaviors, and the shared species (behavior, eye, quasar, snake, storm, swarm) | Interactions refused by a protection mask. The shared sweep systems count player-domain victims in `protected_player_rejects` so that `protected_rejects` stays comparable |
| `<d>.buf_<name>_hwm` | `newBufferTelemetry` | High-water live length of one reusable buffer or state collection. Outside the shared surface |
| `combat.{damage,absorbed}_{attacker,defender}_<type>` | Combat | Damage points dealt, or rejected by immunity, by attacker or defender type. Types are `component.CombatEntityNames`: cursor, drain, quasar, swarm, storm, pylon, snake_head, snake_body, eye, tower, kraken |
| `combat.damage_family_<family>` | Combat | Damage points per attack family (`combatAttackNames`): tracer, shield, lightning, explosion, missile, pulse, self_destruct, bullet, ray, burn |
| `combat.live_<type>` | Combat | Live Combat-store population per type. It decomposes `combat.count`, so a type still counted while its species reports inactive is a lifecycle leak |
| `weapon.<kind>`, `weapon.<kind>_fired`, `player.<slot>.weapon.<kind>` | Weapon | Whether a cursor holds the kind, and discharges per kind. `weapon.main_fired` counts main-fire actions past the cooldown, each of which fires the loadout |
| `storm.<color>_active_frames` | Storm | Attack ticks of a circle of that colour (red, green, blue) |
| `event.settle_<label>` | Scheduler | Non-empty settle passes, attributed by source label |
| `player.<slot>.<suffix>` | Per-slot metrics | See [The registry](#the-registry) |

## Key notes

### Player, session and progression

| Key | Meaning |
|---|---|
| `player.<slot>.entity` (int) | The cursor entity in the slot, or zero. Gates the slot's cards |
| `player.<slot>.control` (int) | That cursor's control mode, or -1 for an empty slot |
| `player.local` (int) | The slot this instance drives |
| `player.entity` (int) | The cursor entity this instance drives, the number an `:emit` payload names it by, or zero |
| `session.any_defeated` (bool) | At least one rostered cursor's owner-authored defeat latch (`EventCursorDefeatState`) is set. A shipped game-over guard |
| `session.all_defeated` (bool) | Every rostered cursor's defeat latch is set |
| `kills.<species>` (int) | Species deaths. `kills.drain` counts `EventDrainDefeated`, not `EventSpeciesKilled`. A tower's death is never counted: the structure is the players' own, and `kills.tower` stays zero |
| `kills.total`, `kills.uncredited` (int) | All counted deaths, and those whose killer resolves to no roster cursor |
| `energy.damage_multiplier` (int) | The cycle damage factor. `MetaSystem.setDamageMultiplier` is its only writer: it doubles on `EventCycleDamageMultiplierIncrease` and returns to 1 on `EventCycleDamageMultiplierReset` and in `Init`. `EnergySystem` multiplies penalties by it |
| `energy.penalty_rejects` (int) | Penalties absorbed by an active boost or ember |
| `energy.missing_energy_rejects` (int) | Resolved cursor requests whose cursor had no energy component |

### Species, combat and effects

| Key | Meaning |
|---|---|
| `combat.cursor_rejects` (int) | Direct and area attacks whose owner resolves to no roster cursor. The attack still resolves, so the count measures species-owned attacks rather than rejections |
| `combat.attacker_rejects` (int) | Attacks with no attributable attacker type: a direct hit whose origin and owner both lack combat state, or an area attack whose origin is neither a cursor nor a combat entity |
| `combat.container_rejects` (int) | Attacks refused because the resolved composite was a non-damageable container |
| `combat.relation_rejects` (int) | Direct hits refused because the hit entity was not a member of the target composite |
| `combat.target_rejects` (int) | Attacks refused because the target, or a required member, lacked combat state |
| `combat.kinetic_immune_rejects` (int) | Kinetic effects refused on a living target by an open kinetic-immunity window or enrage |
| `combat.stun_immune_rejects` (int) | Stun effects a living target's species or state refused |
| `combat.effect_burn`, `combat.burn_ticks` (int) | Burns left or refreshed by a charged pulse's chain, and burn ticks that dealt damage |
| `combat.effect_energy_drain` (int) | Energy-drain effects that emitted an energy reward |
| `combat.chain_{followups,depth_max,depth_total}` (int) | Follow-up attacks emitted by chain profiles, the deepest chain, and the sum of emitted depths |
| `swarm.killed_by_lifecycle` (int) | Also counts swarms retired after `SwarmMaxCharges` charges or at their lifetime deadline, which emit no kill event |
| `swarm.transition_stalls` (int) | Locks that found no target, the chase re-arming, and locks that could neither charge nor teleport and fell back to deceleration |
| `behavior.unknown_program_rejects` (int) | Spawn and cancel requests naming no program of the scenario, dropped |
| `behavior.<program>.count` (int) | Live instances of the program and those still materializing in; `behavior.count` sums the programs |
| `drain.count` (int) | Live drains not already queued for death |
| `drain.paused` (bool) | A spawning hold applies to this instance's own cursor: a session-wide hold, or a quasar fused from that cursor |
| `nav.recomputes` (int) | Flow fields rebuilt on the last tick (a gauge) |
| `nav.roi_cells` (int) | Cumulative composite-passability cells recomputed after wall changes: each change's bounds widened by the composite footprint and clamped to the map |
| `nav.entities` (int) | Entities holding a Navigation component on the last tick. Outside the shared surface |
| `loot.unreachable` (int) | Drops with no route home this tick, because their owner is gone or walls seal them off. They bleed to rest |
| `weapon.orbs_reaped` (int) | Orbs the store held that no loadout justified: a duplicate, an orb of a kind that fires from the cursor, one whose owner is not a rostered cursor this instance simulates, or one whose weapon has no charges |
| `weapon.kind_rejects` (int) | Grants or removals naming a kind outside `WeaponSpecs` |
| `mount.fired` (int) | Mounted discharges; a ray counts once per warning start |
| `mount.host_rejects` (int) | Mount requests naming an unplaced or non-Shared host, a cursor, or an unknown weapon or lane |
| `bullet.spawned`, `bullet.hits` (int) | Bullets created by a mount or a cursor's turret, and bullets stopped by a cursor, a shield or a combat target |
| `dust.created`, `dust.destroyed` (int) | Dust actually created, and dust consumed by a detonation. Expired dust dies through the timer |
| `dust.centers_dropped` (int) | Burst centres (one per occupied cell) dropped from a detonation that already held `ExplosionCenterCap` |
| `death.batch_*` (int) | Resolved `EventDeathBatch` payloads (`count`), entries presented (`entities_total`), the largest batch (`size_max`), and requests per effect route (`silent`, `flash`, `blossom`, `decay`, `fadeout`, `dust`, `other`) |
| `death.tagged` (int) | Entities processed from the `DeathComponent` sweep |
| `death.{protected,zero,payload}_rejects`, `death.missing_{entities,effect_data}` (int) | Entries refused by `ProtectFromDeath`, zero identifiers, bad payload types, entities already gone, and deaths whose effect lacked position, glyph or wall data |
| `environment.drift_active`, `environment.drift_scenery` (bool), `environment.drift_direction`, `environment.drift_speed` (float) | The drift gauge, whether text placed under it is scenery, its heading, and the pace in cells a second it drifts or eases to, 0 while none runs. The glyph spawner reads them to choose its entry edge, to spawn sooner past `DriftFillSpeed` and to stamp and count scenery |

### Network

| Key | Meaning |
|---|---|
| `network.state` (string) | `off`, `waiting`, `connected` or `down`. The status bar shows `Net: down` for `StatusNetworkDownDuration` (10 s) and `Net: wait`, and hides the badge when `off` |
| `network.peers`, `network.connected` | Connected session peers, and whether there is at least one |
| `network.map_latched` (bool) | The host-authored D-14 map dimensions are latched |
| `network.state_applied` (int) | Owner-authored cursor snapshots applied to remote cursors |
| `network.frames_dropped` (int) | Frames rejected by encoding, framing, ordering or identity checks |
| `network.barrier_deferred` (int) | Local crossing artifacts the barrier took at production, to apply at the agreed tick |
| `network.barrier_applied_local`, `network.barrier_applied_peer` (int) | Local and peer artifacts admitted at their playout boundary |
| `network.barrier_late` (int) | Artifacts applied after their apply tick. Committed frames reach a paced guest before their tick, so a rise is a guest running ahead of its pacing band, which its next correction repairs |
| `network.barrier_delay_ticks` (int) | The playout lead in force, in ticks |
| `network.barrier_ran_without_peer` (int) | Tick boundaries reached before every required peer epoch marker. The authority requires none |
| `network.barrier_peer_lag_ticks`, `network.barrier_peer_artifacts`, `network.barrier_peer_applied` | Closed-epoch lag beyond this instance's lead (zero on the authority), and the peer artifacts admitted at the latest boundary |
| `network.artifacts_refused` (int) | Peer artifacts refused: an arrival or departure from a participant that is not the coordinator, a crossing or cursor sync acting as another participant's cursor, or a raw epoch under an identity whose link it did not arrive on |
| `network.artifacts_refused_tick` (int) | Peer artifacts naming an apply tick more than `NetworkApplyWindowTicks` ahead, or a run this instance is not in: a reset's last epochs, sent a round trip before it applied |
| `network.artifacts_schedule_full` (int) | Peer artifacts turned away because their source held its `NetworkScheduledPerSource` share, the schedule held `NetworkScheduledMax` entries, or the artifact would take it past `NetworkScheduledBytes` |
| `network.artifacts_pre_install` (int) | Scheduled or arriving artifacts the installed world already contains: a barrier-bound one due at or before the capture's tick (D-22), any other at or below its source's fence. Non-zero on a joiner is the ordering working |
| `network.artifacts_authority_superseded` (int) | The part of `artifacts_pre_install` that a source fence, rather than the capture's tick, proved contained |
| `network.artifacts_restored` (int) | Applied barrier-bound artifacts the installed capture did not contain, returned to the schedule to apply again |
| `network.commit_late`, `network.commit_void` (int) | On the authority: guest crossings committed at its next tick because they arrived after their own, and the subset too late to apply, which only closed their source's fence |
| `network.commit_refused_raw` (int) | On a guest: another participant's raw epochs, passed toward the authority and not applied |
| `network.join_lag_ticks` (int) | Ticks this participant still stood behind after its join caught up, reported by `membership.finishCatchUp`. Above `NetworkJoinLagTicks`, the join is refused |
| `network.lag_ticks` (int) | How far behind the newest tick any peer closed this instance stands, measured every tick: on a paced guest the margin its pacing holds, not a fault. Zero on the authority |
| `network.stale` (bool) | Pacing's verdict that this instance is out of step: its last decision stepped, or the latest authority epoch of the window landed late beyond `NetworkPaceBandTicks`. Ahead, committed crossings reach it late; behind by a step, its lead grows. The badge reads it as `desync` with `pace_late_ticks`. Always false on the authority |
| `network.corrections_received` (int) | Authoritative corrections reassembled from the wire |
| `network.relay_forwarded` (int) | Artifact batches and correction chunks this instance passed down to the participants behind it, and departure notices it flooded |
| `network.relay_duplicates` (int) | Epochs, syncs and chunks already admitted arriving again, and crossing sequences already taken, whatever epoch carried them |
| `network.relay_refused` (int) | On a guest: raw epochs and syncs dropped from a participant it does not carry, one that neither joined through it nor is behind it while its player does not relay for others (`:relay`) |
| `network.transport_lost_in`, `network.transport_lost_out` (int) | Inbound notifications a full poll buffer discarded, and outbound frames a peer's send queue refused. A new loss is also logged |
| `network.sent_epoch_bytes`, `network.sent_sync_bytes`, `network.sent_correction_bytes`, `network.sent_repair_bytes`, `network.sent_control_bytes` (int) | Framed bytes queued on this instance's socket links, before each link's deflate, by what the frames carried: epochs and bundles, owner syncs, whole corrections and join captures, the manifest exchange (manifests, requests, shards, unserved), and everything else (probes, echoes, roster and succession). Cumulative over every link the port has had; an in-process mesh reports none |
| `network.wire_bytes_out`, `network.wire_bytes_in` (int) | Bytes the socket links wrote and read, after deflate and before inflate: what the wire carried, against `sent_*_bytes` for the stream's ratio |
| `network.link_rtt_ms` (int) | The slowest measured link's smoothed round trip. A measured one is at least 1, so zero means no link is measured. Every `link_*` cell clears when the last link goes |
| `network.link_jitter_ms`, `network.link_bps`, `network.link_peer` (int) | That link's smoothed variation, its measured delivery rate in bytes per second, and the participant at its far end |
| `network.link_saturated` (bool) | The link, not the sender, was the limit while `link_bps` was measured: a backlog stood across two echoes, or this instance's frames waited on the link longer than two probe intervals while its round trip stood inflated. Without it, `link_bps` is a lower bound on capacity |
| `network.link_loss_pct` (int) | The highest probe-loss share over all links, which need not be the slowest link's. The badge shows `loss n%` at `StatusNetLossWarnPct` or more. `:session` prints it in the link line, which appears only while a link is measured |
| `network.pace_late_ticks` (int) | The latest arrival in the last full pacing window of authority epochs: the largest of this guest's tick minus an epoch's produced tick. Negative is early |
| `network.pace_trim_permille`, `network.pace_steps` (int) | The trim pacing applies to the tick interval (positive runs slower), and whole-tick steps taken for an error too large to trim |
| `network.term` (int) | The authority term this instance acts under. An artifact from an older term is ignored; one from a term nobody handed this instance is refused |
| `network.authority` (int) | The participant authoring the shared world |
| `network.migrations` (int) | Handoffs this instance took part in, as the candidate that took the term or a follower |
| `network.term_refused` (int) | Artifacts naming a term nobody handed this instance: a fork, or an instance that skipped a succession |
| `network.term_stale` (int) | Artifacts from a term the session has left. Non-zero across every handoff and not an error |
| `network.handoff_bytes` (int) | Bytes of succession frames this instance received and drained: loss reports, handoff records and peer lists |
| `network.fork` (bool) | A succession ended with no handoff, because no candidate answered, the run lost its path to its authority, or the session pinned its authority, and this instance continues as an explicit local fork. An adopted handoff clears it, as does a session the run opens, which a person's run does at once (`membership.forked`) |
| `network.migrating` (bool) | A succession is open, or a handoff was adopted less than `NetworkMigrationBadgeTicks` ago. The status bar shows `Migrating`, with `rejoin_attempts` when non-zero, over `Host lost`. Dials are refused while the succession is open (`Authority.Migrating`), not for the tail |
| `network.host_lost` (bool) | The authority went and this instance continues from the last authoritative state: a guest lost its link to the authority, or a succession ended as a fork. An adopted handoff clears it, as does a session the run opens. It outranks every badge but `Migrating` (`Host lost`), and `:session` names it |
| `network.evicted` (int) | Participants this authority dropped, under the slow policy or for exceeding the ingress byte budget |
| `network.chain` (int) | Candidates in the succession chain, the participants with an address. `:session` reports it as "confirmed reachable" and names the doors beside it. A participant that declared no port plays, and is never a candidate, listed by its door alone if it relays ([Multiplayer](multi-player.md) §5.3, §5.6) |
| `network.rejoin_attempts` (int) | Passes down the succession list, one a second, while this instance walks it or has no path to its authority |
| `network.listening` (bool) | This instance bound a port peers can dial back. False on a leaf: a `-no-advertise` run, or one whose bind failed |

### Snapshot and correction

| Key | Meaning |
|---|---|
| `snapshot.bytes`, `snapshot.capture_us`, `snapshot.encode_us` (int) | The last shared-world capture this instance read: its encoded length, its cost under the world lock (the one stall a join is allowed), and its encoding cost outside the lock |
| `snapshot.pages_repaired`, `snapshot.cells_repaired`, `snapshot.entities_repaired` (int), `snapshot.repair_sections` (string) | Repairs this instance applied: pages and rows written, entities touched, and the sections the last one moved, which the debug record `repair rows` names row by row |
| `snapshot.stage_us`, `snapshot.commit_us`, `snapshot.project_us`, `snapshot.write_us`, `snapshot.install_tick`, `snapshot.catch_up_ticks` (int) | The last install: staging cost, commit cost and its two parts under the live lock (the ticks the staging world simulated, then the comparison of its stores against the live ones in place and the write), the tick it described, and the ticks simulated afterwards to close the gap |
| `snapshot.keyframes` (int) | Whole captures this host published, as a join gate or a cadence keyframe |
| `snapshot.corrections_sent`, `snapshot.correction_bytes_sent` (int) | Corrections broadcast, keyframes and deltas together, and their total size |
| `snapshot.corrections_applied` (int) | Corrections this instance installed |
| `snapshot.corrections_refused` (int) | Corrections that could not be resolved: a delta against a keyframe not held, or a body its header does not describe. The next keyframe is self-sufficient |
| `snapshot.corrections_superseded` (int) | Corrections a fresher one overtook before they applied, plus any the queue dropped |
| `snapshot.corrections_held` (int) | Corrections the playout buffer deferred until this instance reached their tick. A steady rise with no applications beside it is the buffer stalling |
| `snapshot.corrections_jumped` (int) | Corrections more than `parameter.NetworkHoldTicks` ahead of the clock, adopted at their own tick: a join, or a clock that had fallen behind |
| `snapshot.projected_ticks` (int) | How far behind the clock the last correction was, which is how far staging simulated it forward |
| `snapshot.correction_entries`, `snapshot.correction_entities`, `snapshot.correction_cells`, `snapshot.correction_tick` (int) | The last applied correction: component cells moved, distinct shared entities behind them, the largest placement move, and its tick. `NetworkSystem` reports `correction_entities` to peers as the correction magnitude |
| `snapshot.predictions_pending` (int) | Predicted species kills held for authoritative confirmation |
| `snapshot.predictions_confirmed` (int) | Held kills released as `EventSpeciesKillConfirmed`: proved by an authoritative capture, paid out past `PredictionLedgerMax`, or released once nothing corrects this instance |
| `snapshot.predictions_dropped` (int) | Held kills an authoritative capture refuted: the entity is still alive a convergence floor (`SnapshotFloorKeyframeTicks`) later, or the authority's allocator never issued it |
| `snapshot.manifests_sent`, `snapshot.manifests_received` (int) | Correction indexes published, and indexes received, including superseded ones |
| `snapshot.manifests_off_tick` (int) | Indexes compared against this receiver's present because no reading at their tick was held |
| `snapshot.manifest_bytes_sent`, `snapshot.manifest_bytes_received` (int) | What the indexes cost. On a converged link, this is the whole of a correction |
| `snapshot.corrections_hash_only` (int) | Corrections that carried no state because the roots agreed |
| `snapshot.sections_compared`, `snapshot.pages_compared` (int) | Section hashes examined, and page hashes computed while descending into disagreeing sections |
| `snapshot.hash_us` (int) | What indexing and comparing a capture cost outside the world lock. Read it beside `capture_us` |
| `snapshot.selective_bytes` (int) | The measured wire cost of a non-keyframe correction, which the cadence controller and admission are priced from |
| `snapshot.shards_{requested,sent,received,refused,applied}` (int) | Pages a receiver asked about, pages returned because they differed, pages arrived, pages in a repair that was not applied, and pages spliced into a capture whose root then verified |
| `snapshot.shard_bytes_sent`, `snapshot.shard_bytes_received`, `snapshot.request_bytes` (int) | Repair cost per peer from each end, and what this instance's own answers to indexes cost; a holder's sent figures include the in-process links of its own seats |
| `snapshot.{pages,entities,cells}_repaired` (int) | Pages a verified repair replaced, the distinct entities behind them, and the indexed cells carried |
| `snapshot.proof_failures` (int) | Repairs whose rows or result did not reproduce their declared hash. Nothing was written |
| `snapshot.baseline_refusals` (int) | Indexes or repairs describing a run, session or baseline this instance does not hold |
| `snapshot.keyframe_fallbacks` (int) | Times the exchange gave up and a whole world was sent or asked for |
| `snapshot.relay_retained` (int) | Authoritative records held for a neighbour to ask about. An index enters the ring only once its capture is provably the authority's |
| `snapshot.relay_served`, `snapshot.relay_unserved` (int) | Repairs answered from that retention, and requests refused because the tick is not held |
| `snapshot.relay_bytes_sent`, `snapshot.relay_bytes_received` (int) | Relayed answer cost, priced against the relaying participant's own link |
| `snapshot.replay_suffix_records` (int) | Crossings retained for replay |
| `snapshot.replay_records` (int) | Crossings re-applied in a projection, cumulative |
| `snapshot.replay_overflow` (int) | Retained crossings dropped by the tick, count or byte bound |
| `snapshot.replay_skipped` (int), `snapshot.replay_suffix_unavailable` (bool) | Corrections installed without replay because the suffix was incomplete, and whether that was so at the last correction |
| `snapshot.cadence_ticks` (int) | The correction cadence in force: the publication timeline's base, the fastest peer's plan |
| `snapshot.cadence_keyframe_interval`, `snapshot.cadence_keyframe_period_ticks` (int) | Corrections between whole captures, and the product of the two: ticks between whole authoritative worlds. A period above `SnapshotFloorKeyframeTicks` is a controller defect |
| `snapshot.cadence_keyframe_age_ticks` (int) | How long this instance has actually gone without a whole authoritative world |
| `snapshot.cadence_uplink_bps`, `snapshot.cadence_floor_bps` (int) | What the schedule costs, and what the convergence floor costs, summed over the planned links |
| `snapshot.cadence_links`, `snapshot.cadence_constrained_links` (int) | Links the schedule plans for, and how many of them are worse than nominal |
| `snapshot.cadence_budget_bps` (int) | What the tightest link allows after the utilisation share, priced on the rate it delivered while it was the limit (a join's capture, or frames held behind a backlog) and doubled every `SnapshotLinkCapacityTicks` a constrained plan goes without the link holding them again. Zero means no link has been read as the limit |
| `snapshot.cadence_constrained` (bool) | A link moved the operating point worse than nominal. While a cadence is in force and no worse qualifier applies, the status bar shows `slow` |
| `snapshot.cadence_floor_breached` (bool) | No schedule delivers a whole world inside the guaranteed window, or none arrived. The status bar shows `slow!` while a cadence is in force, and `:session` prints `BELOW THE CONVERGENCE FLOOR` |

### Engine, event and process

| Key | Meaning |
|---|---|
| `engine.music_apm` (int) | The action rate music follows. The status bar shows it as `APM` |
| `engine.tick_slips` (int) | Missed tick deadlines. Process pacing, so outside the shared surface |
| `event.invalid` (int) | Dispatched events whose type was outside the declared range |
| `event.settle_exhausted` (int) | Settles that reached the pass cap with events still queued |
| `event.dispatch_by_type`, `event.dead_by_type` (string) | Sparse `EventType=count` summaries, refreshed every `StatSnapshotTicks` |
| `spatial.cell_overflows`, `spatial.cell_saturations` (int) | Insertions dropped because a cell held `MaxEntitiesPerCell` (31) entities, and transitions where a cell first reached that capacity |
| `spatial.player_budget_rejects` (int) | Player-domain insertions refused because the cell's `ReservedPlayerPerCell` budget was spent. A shared entity is never refused for a player one |
| `spatial.indexed_entities`, `spatial.indexed_shared` (int) | Entities in spatial cells, and the shared half, which D-11 requires two instances to agree on |
| `spatial.unindexed` (int) | Positioned entities the grid holds no cell for: off-map after a crop, or dropped by a full cell. Nothing renders them and no grid query reaches them |
| `spatial.{occupied_cells,max_cell_occupancy}` (int) | Non-empty cells and the fullest cell, on the snapshot cadence |
| `spatial.{cell_occupancy_hwm,positions_hwm,position_batch_hwm}` (int) | Highest cell occupancy, dense position-store length, and committed position batch this session |
| `content.source`, `content.files`, `content.blocks`, `content.lines` | The corpus fingerprint the journal anchor and capture header carry. A replay verifies it |
| `content.rejected` (int) | Corpus sources rejected at load |
| `content.served` (int), `content.file` (string) | Blocks drawn, and the source of the latest one |
| `stat.late` (int) | Registrations after `Freeze`. Non-zero is a regression |
| `stat.groups`, `stat.metrics` (int) | Cards and keys in the frozen index |
