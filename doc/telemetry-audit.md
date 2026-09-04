# Telemetry audit

This audit covers every system constructed by `manifest.BuildSystems`, `MetaSystem`, and telemetry owned by `internal/engine` and `internal/event`. The table describes the final wiring; the defect table preserves the Phase 1 findings from the pre-fix tree.

Every metric is consumed generically by the status snapshot, debug overlay, pinning UI, and `vif-log` registry traversal. “Generic only” means no code or configuration reads the key by name outside its producer. Player patterns expand across roster slots 0–15 and retain the legacy slot-zero mirror; inactive slots keep their frozen schema but are omitted from live views and output until active. Stable keys project into semantic groups of at most 15 fields for overlay and log presentation.

## Phase 1 system audit

| Owner | Metric keys | Registered | Written | Reset | Named consumer |
|---|---|---|---|---|---|
| Cursor | `player.{count,local,cursor_rejects,spawn_failures}`, `player.<slot>.{entity,control}` | `NewCursorSystem` | Spawn/despawn handlers and roster publication | `Init` | Generic only |
| Ping | `ping.{cursor_rejects,disabled_rejects}` | `NewPingSystem` | Request rejection paths | `Init` | Generic only |
| Transient | `effects.{grayout_active,strobe_active}` | `NewTransientSystem` | Effect handlers/update | `Init` | Generic only |
| Camera | None | — | — | — | — |
| Energy | `energy.{current,damage_multiplier,penalty_count,reward_count,spend_count,crossed_zero_count,penalty_rejects,cursor_rejects,missing_energy_rejects,disabled_rejects}`, `player.<slot>.energy.current` | `NewEnergySystem` | Resolved energy handlers/update | `Init` / player reset | `energy.damage_multiplier` is read by the status bar; remainder generic |
| Shield | `shield.{active,shield_hit,cursor_rejects,disabled_rejects}`, `player.<slot>.shield.active` | `NewShieldSystem` | Resolved shield handlers/update | `Init` / player reset | Generic only |
| Heat | `heat.{current,overheat,at_max,ember,cursor_rejects,disabled_rejects}`, `player.<slot>.heat.*` | `NewHeatSystem` | Resolved heat handlers/update | `Init` / player reset | Generic only |
| Boost | `boost.{active,remaining,truncated,cursor_rejects,disabled_rejects}`, `player.<slot>.boost.*` | `NewBoostSystem` | Resolved boost handlers/update | `Init` / player reset | Generic only |
| Weapon | `weapon.{rod,launcher,disruptor,orbs,*_fired,orbs_reaped,cursor_rejects,disabled_rejects}`, `player.<slot>.weapon.*` | `NewWeaponSystem` | Resolved fire/weapon handlers | `Init` / player reset | Generic only |
| Typing | `typing.{correct,errors,max_streak,buf_delete_hwm,cursor_rejects,disabled_rejects}`, `player.<slot>.typing.max_streak` | `NewTypingSystem` | Resolved typing/delete paths | `Init` / player reset | Generic only |
| Composite | None | — | — | — | — |
| Wall | `wall.{enabled,count,push_events,buf_pending_push_checks_hwm}` | `NewWallSystem` | Wall handlers/update and buffer observation | `Init` | Generic only |
| Tower | `tower.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `NewTowerSystem` | Spawn/cancel/death handlers and update | `Init` | Generic only |
| Gateway | `gateway.{active,count}` | `NewGatewaySystem` | Spawn/despawn handlers/update | `Init` | Generic only |
| Loot | `loot.{drops,active,collects,routes,route_recomputes,unreachable,wall_collisions,boundary_reflections,physics_steps,buf_pity_hwm,buf_routes_hwm}` | `NewLootSystem` | Drop/collect handlers, owner-route maintenance, bounce integration, buffer observation | `Init` | Generic only |
| Glyph | `glyph.{enabled,next_spawn_ms,orphan_glyph,density,rate_mult,buf_placement_hwm}` | `NewGlyphSystem` | Spawn/update and snapshot publication | `Init` | Generic only |
| Nugget | `nugget.{active,spawned,collected,jumps,spawn_failures,cursor_rejects,disabled_rejects}` | `NewNuggetSystem` | Resolved nugget handlers/update | `Init` | Generic only |
| Decay | `decay.{count,applied,wall_collisions,boundary_hits,grid_steps,protected_rejects,buf_hit_entities_hwm,buf_processed_cells_hwm}` | `NewDecaySystem` | Spawn/apply/update paths | `Init` | Generic only |
| Blossom | `blossom.{count,applied,wall_collisions,boundary_hits,grid_steps,protected_rejects,buf_hit_entities_hwm,buf_processed_cells_hwm}` | `NewBlossomSystem` | Spawn/apply/update paths | `Init` | Generic only |
| Gold | `gold.{active,header_entity,timer,spawn_failures,cursor_rejects,disabled_rejects}` | `NewGoldSystem` | Resolved sequence handlers/update | `Init` | Generic only |
| Materialize | None | — | — | — | — |
| Cleaner | `cleaner.{active,spawned,wall_collisions,boundary_steps,grid_steps,cursor_rejects,disabled_rejects,buf_entities_hwm}` | `NewCleanerSystem` | Resolved requests and swept update | `Init` | Generic only |
| Fuse | `fuse.{spawn_failures,disabled_rejects,buf_pending_hwm}` | `NewFuseSystem` | Fuse request/update failure paths and buffer observation | `Init` | Generic only |
| Spirit | `spirit.buf_destroy_next_tick_hwm` | `NewSpiritSystem` | Deferred-destroy buffer observation | `Init` | Generic only |
| Lightning | None | — | — | — | — |
| Missile | `missile.{count,spawned,impacts,expired,wall_collisions,boundary_hits,grid_steps,disabled_rejects}` | `NewMissileSystem` | Resolved spawn/impact/expiry and swept update | `Init` | Generic only |
| Navigation | `nav.{entities,recomputes,roi_cells,buf_groups_hwm}` | `NewNavigationSystem` | Recompute/update paths and group observation | `Init` | Generic only |
| Soft collision | `soft_collision.{collisions,immune_rejects,buf_{drains,swarms,quasars,storms,pylons}_hwm}` | `NewSoftCollisionSystem` | Resolved collision pass and buffer observation | `Init` | Generic only |
| Combat | `combat.{active,count,hits_direct,hits_area,knockbacks,stuns,damage_dealt,immune_rejects,unprofiled,*_rejects,effect_*,chain_*,damage_{attacker,defender}_*,absorbed_{attacker,defender}_*}` | `NewCombatSystem` | Resolved direct/area attacks | `Init` | Generic only |
| Drain | `drain.{count,pending,paused,collisions,suicides,spawned,fusions,despawned,spawn_failures,killed_by_*,wall_collisions,boundary_reflections,grid_steps,protected_rejects,buf_*_hwm}` | `NewDrainSystem` | Spawn/lifecycle/collision/movement paths | `Init` | Generic only |
| Quasar | `quasar.{active,count,spawned,despawned,killed_by_*,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewQuasarSystem` | Spawn/lifecycle/bounce paths | `Init` | Generic only |
| Swarm | `swarm.{active,count,player_kills,spawned,despawned,killed_by_*,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewSwarmSystem` | Spawn/lifecycle/bounce paths | `Init` | Generic only |
| Storm | `storm.{active,circle_count,*_active_frames,nudge_count,spawned,despawned,killed_by_*,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects,buf_*_hwm}` | `NewStormSystem` | Spawn/lifecycle/3-D physics/update paths | `Init` | Generic only |
| Pylon | `pylon.{active,count,spawned,despawned,killed_by_player,killed_by_lifecycle,spawn_failures}` | `NewPylonSystem` | Spawn/cancel/death handlers and update | `Init` | Generic only |
| Snake | `snake.{active,count,spawned,despawned,killed_by_*,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewSnakeSystem` | Spawn/lifecycle/bounce paths | `Init` | Generic only |
| Eye | `eye.{count,spawned,despawned,killed_by_*,spawn_failures,wall_collisions,boundary_reflections,physics_steps,protected_rejects,protected_player_rejects}` | `NewEyeSystem` | Spawn/lifecycle/bounce paths | `Init` | Generic only |
| Bullet | `bullet.{wall_collisions,boundary_hits,grid_steps,disabled_rejects}` | `NewBulletSystem` | Spawn rejection and swept update | `Init` | Generic only |
| Dust | `dust.{created,active,destroyed,wall_collisions,boundary_reflections,grid_steps,buf_*_hwm}` | `NewDustSystem` | Resolved spawn/destruction/collision paths | `Init` | Generic only |
| Flash | None | — | — | — | — |
| Fadeout | None | — | — | — | — |
| Marker | None | — | — | — | — |
| Explosion | `explosion.{triggered,converted,merged,cursor_rejects,disabled_rejects,buf_*_hwm}` | `NewExplosionSystem` | Resolved explosion paths and reusable collections | `Init` | Generic only |
| Motion marker | `motion_marker.buf_{base_markers,base_positions,colored_markers}_hwm` | `NewMotionMarkerSystem` | Reusable marker-buffer observation | `Init` | Generic only |
| Splash | None | — | — | — | — |
| Environment | `environment.wind_active` | `NewEnvironmentSystem` | Update gauge | `Init` | Generic only |
| Death | `death.{killed,one_*,tagged,batch_*,protected_rejects,zero_rejects,missing_*,payload_rejects,disabled_rejects,buf_destroy_hwm}` | `NewDeathSystem` | Resolved single/tagged/batch death paths | `Init` | Generic only |
| Timer | None | — | — | — | — |
| Adaptation | `adapt.{graphs,populations,g1,g2,g3,g4,buf_*_hwm}` | `NewAdaptationSystem` | Adaptation processing; expensive strings on snapshot cadence | `Init` | Generic only |
| Genetic | `eye.ga.{generation,best,avg,pending,outcomes,tracked,typefit}`, `eye.buf_ga_*_hwm` | `NewGeneticSystem` | GA processing; formatted type fitness on snapshot cadence | `Init` | Generic only |
| Audio | `audio.{backend,silent,played,dropped,mask,effect_muted,music_muted,rej_*}` | `NewAudioSystem` | Session deltas from backend/update | `Init` with backend baselines | `audio.mask` is read by the status bar; remainder generic |
| Music | None | — | — | — | — |
| Meta | `context.{map_w,map_h,camera_x,camera_y}`, `player.<slot>.{x,y}`, `kills.{<species>,total,uncredited}`, `session.all_defeated` | `NewMetaSystem` | Debug/map publication, lifecycle fold and resolved species-kill handler | `Init` | Kill keys and `session.all_defeated` are FSM guards; remainder generic |
| Network | `network.{crossings_sent,crossings_received,crossings_local,state_applied,frames_dropped,barrier_deferred,barrier_applied_local,barrier_applied_peer,barrier_late,barrier_ran_without_peer,barrier_peer_lag_ticks,barrier_peer_artifacts,barrier_peer_applied,peers,connected,state,map_latched,artifacts_pre_install,artifacts_refused,corrections_received,join_lag_ticks,lag_ticks,stale,digest_mismatches,drift_part,drift_tick}` | `NewNetworkSystem` | Transport polling, playout admission, cursor sync, connection state and the running staleness measurement | `Init` | Status bar reads peer/state/latch and `stale`/`lag_ticks`; remainder generic |
| Snapshot transfer | `snapshot.{bytes,capture_us,encode_us,stage_us,commit_us,install_tick,catch_up_ticks,keyframes,corrections_sent,correction_bytes_sent,corrections_applied,corrections_refused,corrections_superseded,correction_entries,correction_entities,correction_cells,correction_tick}` | `newSnapshotTelemetry` during App construction | A capture read, a correction published, or a correction installed | **Not reset** — a join is not undone by `:new`, and these describe a transfer rather than a game | Generic only; the whole prefix is excluded from the compared shared surface *and* from a replay comparison: a host publishes what a read cost, a guest what an install cost and how far its own prediction had drifted, and under weakened D-11 the last of those is deliberately not something two instances agree on |
| Selective correction | `snapshot.{manifests_sent,manifests_received,manifest_bytes_sent,manifest_bytes_received,corrections_hash_only,sections_compared,pages_compared,hash_us,selective_bytes,shards_requested,shards_sent,shards_received,shards_refused,shards_applied,shard_bytes_sent,shard_bytes_received,request_bytes,pages_repaired,entities_repaired,cells_repaired,proof_failures,baseline_refusals,keyframe_fallbacks,replay_suffix_records,replay_records,replay_overflow,replay_skipped,replay_suffix_unavailable}` | `newSnapshotTelemetry` during App construction | Every index published or answered, every repair served or applied, every correction that replayed a local suffix | **Not reset** — like the rest of the prefix, these describe transfers rather than a game | Generic only; excluded from the compared surface with the whole `snapshot.` prefix. They are split across the `snapshot.index`, `snapshot.repair` and `snapshot.replay` cards so each stays inside `OverlayCardMaxEntries` |
| Correction cadence | `snapshot.cadence_{ticks,keyframe_interval,keyframe_period_ticks,keyframe_age_ticks,uplink_bps,budget_bps,floor_bps,constrained,floor_breached}` | `newSnapshotTelemetry` during App construction | Every publication decision, and every correction a guest applies | **Not reset** — the operating point describes this instance's link, which a `:new` does not change | Generic only; excluded from the shared surface with the rest of the prefix. Network timing may pace a transport and may not enter shared simulation state (D-24), and `TestLinkMeasurementNeverEntersTheComparedSurface` is the assertion from the other direction |
| Link measurement | `network.link_{rtt_ms,rtt_us,jitter_ms,bps,loss_pct,saturated}` | `NewNetworkSystem` | Every tick, copied from the transport's own per-peer estimate for the worst link | Cleared with the rest of `NetworkSystem`'s counters on a session reset | Generic only; the `network.` prefix is already the exact complement of a peer's counters. Nothing here is derived by a system — the estimate is the transport's and this is a copy, which is what keeps a round trip out of the simulation |

## Engine and event audit

| Owner | Metric keys | Registered | Written | Reset | Named consumer |
|---|---|---|---|---|---|
| Game context | `engine.fps`, `context.{frame,screen_w,screen_h,mode}` | `NewGameContextWithClock` | Presentation/context owners | Context lifecycle | Status bar reads `engine.fps`; snapshot allow-list reads context keys |
| Time control | `engine.{speed_pct,speed,step,breakpoint,paused}` | `NewTimeControl` | Time-control mutation under its owner lock | Time-control reset/persistent operator state | Status bar and app snapshot read these keys |
| Clock scheduler / world | `engine.{ticks,apm,music_apm,tick_slips}`, `entity.{count,created_total,destroyed_total}`, `time.game_elapsed_ms`, `event.{backoffs,dispatches,dead,dropped,queue_len,queue_max,invalid,settle_*}`, `fsm.*` | `NewClockScheduler`; schema-derived region keys bind before `Prepare` | Dispatch/tick tail while the world mutex is held | `resetTelemetry` during reset | Status bar reads tick/APM/FSM; region/debug metrics are generic |
| Event queue | Per-type `[EventTypeCount]atomic.Int64` dispatch/dead arrays; surfaced as `event.{dispatch_by_type,dead_by_type}` | Fixed arrays in `NewEventQueue`; strings in scheduler constructor | Scheduler records after routing; strings publish every `StatSnapshotTicks` | `ResetTelemetry` after stale-event drain | Generic only |
| Position/spatial grid | `spatial.{cell_saturations,cell_overflows,occupied_cells,indexed_entities,max_cell_occupancy,cell_occupancy_hwm,positions_hwm,position_batch_hwm}` | `BindTelemetry` immediately after registry construction | Position mutations under the world mutex; expensive gauges on snapshot cadence | `World.Clear` | Generic only |

## Phase 1 defects and dispositions

| Finding | Keys | Pre-fix evidence | Disposition |
|---|---|---|---|
| Late registration | `context.{map_w,map_h,camera_x,camera_y}`, `player.{x,y}`, `kills.*` | `MetaSystem.Init` called `Get` / `NewPlayerInt` and was re-entered on game reset | Moved every registration to `NewMetaSystem`; `Init` now only stores/resets |
| Dead metrics | `energy.{penalty_count,reward_count}`, `environment.wind_active`, `nav.roi_cells` | Registered but never written after construction/reset | Wired to resolved deltas, the live wind gauge, and clamped recompute ROI area |
| Suspected but not dead | `combat.{hits_direct,hits_area,knockbacks,stuns}` | All four had writes, but direct/area counted accepted profiles before state resolution | Retained keys and moved hit increments to resolved damage/effect/chain paths |
| Unreset metrics | `drain.{spawned,fusions,despawned,spawn_failures}`, all `eye.ga.*`, `nav.{entities,recomputes,roi_cells}`, `storm.nudge_count`, audio backend totals, scheduler session totals | Written values survived `EventGameResetRequest` | All session counters/strings now reset in `Init` or the scheduler reset path; audio publishes deltas from reset baselines |
| Misleading gauge | `drain.count` | Used `Drain.CountEntities()`, including entities with death already queued; pause forced a false zero | Publishes only live, non-dying drains and preserves the live gauge while paused |
| Misleading counters | `dust.created`, `swarm.player_kills`, `combat.hits_*`, audio totals | Dust counted dark entries it skipped; swarm counted every HP death as a player kill; combat counted pre-resolution; audio exposed backend-lifetime totals | Counts now follow actual creation, resolved cursor credit, state-changing attacks, and session deltas |
| Consumerless by key | Most diagnostic counters, including all newly added coverage | No exact-key lookup outside the producer | Deliberately retained: the debug overlay, pinned cards, snapshots, recorder, and `vif-log` enumerate the registry generically |
| Removed/renamed keys | `death.one_packed`, `death.one_fallback` | Baseline registry had 488 keys; the Phase 1 result had 752 | Death API unification retired the two obsolete one-entity path counters; the current registry has 750 keys, a net 262 additions |

## Deliberately unchanged or excluded

- Schema-derived `fsm.<region>.*` keys still bind during FSM loading because region names do not exist when the scheduler constructor runs. Binding completes before `ClockScheduler.Prepare` freezes the registry, and the freeze regression test proves that no key is added afterward.
- The position store binds its metrics immediately after `NewGameContextWithClock` creates the registry; `World` necessarily constructs the store before that registry exists. It is frozen before the first tick and never registers during reset.
- Legacy slot-zero mirrors such as `energy.current`, `heat.current`, and `player.x` remain intact for existing status-bar/config consumers.
- Registry-owned `content.*`, `rec.*`, and `stat.*` metrics and persistent operator/context settings are excluded from session-zero assertions. Reset tests seed and verify every session-owned int, bool, and string; live gauges are allowed to rebuild to their deterministic reset values.
- Systems with no existing counters and no reusable allocation-shaped buffers remain metric-free.
- `hitComposite.members` in the explosion path remains a fresh queued-payload slice, not a reusable system buffer; reusing it would corrupt queued events.
- Event-type arrays remain internal fixed atomics and are formatted only at `parameter.StatSnapshotTicks`, avoiding per-event formatting/allocation.

## Added key catalogue

All 262 surviving additions are listed below. No key was renamed or repurposed; death API unification removed the two obsolete `death.one_*` path counters documented above.

| Key | Description |
|---|---|
| `adapt.buf_cdf_hwm` (int) | High-water live length of the reusable cdf buffer/state collection. |
| `adapt.buf_counts_hwm` (int) | High-water live length of the reusable counts buffer/state collection. |
| `adapt.buf_graph_keys_hwm` (int) | High-water live length of the reusable graph keys buffer/state collection. |
| `adapt.buf_outcome_graphs_hwm` (int) | High-water live length of the reusable outcome graphs buffer/state collection. |
| `adapt.buf_outcome_samples_hwm` (int) | High-water live length of the reusable outcome samples buffer/state collection. |
| `adapt.buf_pending_deaths_hwm` (int) | High-water live length of the reusable pending deaths buffer/state collection. |
| `adapt.buf_sub_keys_hwm` (int) | High-water live length of the reusable sub keys buffer/state collection. |
| `adapt.buf_sum_fitness_hwm` (int) | High-water live length of the reusable sum fitness buffer/state collection. |
| `adapt.buf_track_keys_hwm` (int) | High-water live length of the reusable track keys buffer/state collection. |
| `adapt.buf_tracking_hwm` (int) | High-water live length of the reusable tracking buffer/state collection. |
| `adapt.buf_weight_scratch_hwm` (int) | High-water live length of the reusable weight scratch buffer/state collection. |
| `blossom.boundary_hits` (int) | blossom swept paths terminated at simulation bounds. |
| `blossom.buf_hit_entities_hwm` (int) | High-water live length of the reusable hit entities buffer/state collection. |
| `blossom.buf_processed_cells_hwm` (int) | High-water live length of the reusable processed cells buffer/state collection. |
| `blossom.grid_steps` (int) | Grid-traversal steps executed by swept blossom movers. |
| `blossom.protected_rejects` (int) | blossom interactions rejected by the applicable protection mask. |
| `blossom.wall_collisions` (int) | Resolved blossom contacts with blocking wall cells. |
| `boost.cursor_rejects` (int) | Requests rejected because boost could not resolve a roster cursor. |
| `boost.disabled_rejects` (int) | Action requests dropped while the boost system was disabled. |
| `bullet.boundary_hits` (int) | bullet swept paths terminated at simulation bounds. |
| `bullet.disabled_rejects` (int) | Action requests dropped while the bullet system was disabled. |
| `bullet.grid_steps` (int) | Grid-traversal steps executed by swept bullet movers. |
| `bullet.wall_collisions` (int) | Resolved bullet contacts with blocking wall cells. |
| `cleaner.boundary_steps` (int) | cleaner swept traversal steps rejected by simulation bounds. |
| `cleaner.buf_entities_hwm` (int) | High-water live length of the reusable entities buffer/state collection. |
| `cleaner.cursor_rejects` (int) | Requests rejected because cleaner could not resolve a roster cursor. |
| `cleaner.disabled_rejects` (int) | Action requests dropped while the cleaner system was disabled. |
| `cleaner.grid_steps` (int) | Grid-traversal steps executed by swept cleaner movers. |
| `cleaner.wall_collisions` (int) | Resolved cleaner contacts with blocking wall cells. |
| `combat.absorbed_attacker_cursor` (int) | Damage points rejected by immunity from attacks attributed to cursor. |
| `combat.absorbed_attacker_drain` (int) | Damage points rejected by immunity from attacks attributed to drain. |
| `combat.absorbed_attacker_eye` (int) | Damage points rejected by immunity from attacks attributed to eye. |
| `combat.absorbed_attacker_pylon` (int) | Damage points rejected by immunity from attacks attributed to pylon. |
| `combat.absorbed_attacker_quasar` (int) | Damage points rejected by immunity from attacks attributed to quasar. |
| `combat.absorbed_attacker_snake_body` (int) | Damage points rejected by immunity from attacks attributed to snake body. |
| `combat.absorbed_attacker_snake_head` (int) | Damage points rejected by immunity from attacks attributed to snake head. |
| `combat.absorbed_attacker_storm` (int) | Damage points rejected by immunity from attacks attributed to storm. |
| `combat.absorbed_attacker_swarm` (int) | Damage points rejected by immunity from attacks attributed to swarm. |
| `combat.absorbed_attacker_tower` (int) | Damage points rejected by immunity from attacks attributed to tower. |
| `combat.absorbed_defender_cursor` (int) | Damage points rejected by immunity while cursor was the defender type. |
| `combat.absorbed_defender_drain` (int) | Damage points rejected by immunity while drain was the defender type. |
| `combat.absorbed_defender_eye` (int) | Damage points rejected by immunity while eye was the defender type. |
| `combat.absorbed_defender_pylon` (int) | Damage points rejected by immunity while pylon was the defender type. |
| `combat.absorbed_defender_quasar` (int) | Damage points rejected by immunity while quasar was the defender type. |
| `combat.absorbed_defender_snake_body` (int) | Damage points rejected by immunity while snake body was the defender type. |
| `combat.absorbed_defender_snake_head` (int) | Damage points rejected by immunity while snake head was the defender type. |
| `combat.absorbed_defender_storm` (int) | Damage points rejected by immunity while storm was the defender type. |
| `combat.absorbed_defender_swarm` (int) | Damage points rejected by immunity while swarm was the defender type. |
| `combat.absorbed_defender_tower` (int) | Damage points rejected by immunity while tower was the defender type. |
| `combat.attacker_rejects` (int) | Attack requests rejected because neither origin nor owner had an attributable combat type. |
| `combat.chain_depth_max` (int) | Highest chain-attack depth emitted during the session. |
| `combat.chain_depth_total` (int) | Sum of emitted chain depths, weighted by follow-up count. |
| `combat.chain_followups` (int) | Number of direct follow-up attacks emitted by chain profiles. |
| `combat.container_rejects` (int) | Attack requests rejected because the resolved composite was a non-damageable container. |
| `combat.cursor_rejects` (int) | Requests rejected because combat could not resolve a roster cursor. |
| `combat.damage_attacker_cursor` (int) | Damage points dealt by attackers attributed to cursor. |
| `combat.damage_attacker_drain` (int) | Damage points dealt by attackers attributed to drain. |
| `combat.damage_attacker_eye` (int) | Damage points dealt by attackers attributed to eye. |
| `combat.damage_attacker_pylon` (int) | Damage points dealt by attackers attributed to pylon. |
| `combat.damage_attacker_quasar` (int) | Damage points dealt by attackers attributed to quasar. |
| `combat.damage_attacker_snake_body` (int) | Damage points dealt by attackers attributed to snake body. |
| `combat.damage_attacker_snake_head` (int) | Damage points dealt by attackers attributed to snake head. |
| `combat.damage_attacker_storm` (int) | Damage points dealt by attackers attributed to storm. |
| `combat.damage_attacker_swarm` (int) | Damage points dealt by attackers attributed to swarm. |
| `combat.damage_attacker_tower` (int) | Damage points dealt by attackers attributed to tower. |
| `combat.damage_defender_cursor` (int) | Damage points dealt to defenders of type cursor. |
| `combat.damage_defender_drain` (int) | Damage points dealt to defenders of type drain. |
| `combat.damage_defender_eye` (int) | Damage points dealt to defenders of type eye. |
| `combat.damage_defender_pylon` (int) | Damage points dealt to defenders of type pylon. |
| `combat.damage_defender_quasar` (int) | Damage points dealt to defenders of type quasar. |
| `combat.damage_defender_snake_body` (int) | Damage points dealt to defenders of type snake body. |
| `combat.damage_defender_snake_head` (int) | Damage points dealt to defenders of type snake head. |
| `combat.damage_defender_storm` (int) | Damage points dealt to defenders of type storm. |
| `combat.damage_defender_swarm` (int) | Damage points dealt to defenders of type swarm. |
| `combat.damage_defender_tower` (int) | Damage points dealt to defenders of type tower. |
| `combat.disabled_rejects` (int) | Action requests dropped while the combat system was disabled. |
| `combat.effect_kinetic` (int) | Kinetic effect applications that resolved to an impulse. |
| `combat.effect_stun` (int) | Stun effect applications that changed target state. |
| `combat.effect_vampire` (int) | Vampire effect applications that emitted an energy reward. |
| `combat.kinetic_immune_rejects` (int) | Kinetic effects rejected by kinetic immunity, enrage, or a dead target. |
| `combat.relation_rejects` (int) | Direct-hit requests rejected because the hit entity was not a member of the target composite. |
| `combat.stun_immune_rejects` (int) | Stun effects rejected by species/state immunity. |
| `combat.target_rejects` (int) | Attack requests rejected because the target or required target member lacked combat state. |
| `death.batch_blossom` (int) | Death requests routed through the blossom-effect processor. |
| `death.batch_count` (int) | Resolved unified `EventDeathBatch` payloads. |
| `death.batch_decay` (int) | Death requests routed through the decay-effect processor. |
| `death.batch_dust` (int) | Death requests routed through the dust-effect processor. |
| `death.batch_entities_total` (int) | Total entity entries presented across resolved death requests. |
| `death.batch_fadeout` (int) | Death requests routed through the fadeout-effect processor. |
| `death.batch_flash` (int) | Death requests routed through the flash-effect processor. |
| `death.batch_other` (int) | Death requests using an effect outside the optimized processors. |
| `death.batch_silent` (int) | Death requests processed without an effect. |
| `death.batch_size_max` (int) | Largest entity count observed in one death request. |
| `death.buf_destroy_hwm` (int) | High-water live length of the reusable destroy buffer/state collection. |
| `death.disabled_rejects` (int) | Death requests dropped while the death system was disabled. |
| `death.missing_effect_data` (int) | Deaths whose requested effect lacked the required position/glyph/wall data. |
| `death.missing_entities` (int) | Death entries that no longer existed when resolved. |
| `death.payload_rejects` (int) | Death events rejected because their payload type was invalid. |
| `death.protected_rejects` (int) | Death entries rejected by `ProtectFromDeath`. |
| `death.tagged` (int) | Entities processed from the `DeathComponent` tick path. |
| `death.zero_rejects` (int) | Zero entity identifiers rejected by the death pipeline. |
| `decay.boundary_hits` (int) | decay swept paths terminated at simulation bounds. |
| `decay.buf_hit_entities_hwm` (int) | High-water live length of the reusable hit entities buffer/state collection. |
| `decay.buf_processed_cells_hwm` (int) | High-water live length of the reusable processed cells buffer/state collection. |
| `decay.grid_steps` (int) | Grid-traversal steps executed by swept decay movers. |
| `decay.protected_rejects` (int) | decay interactions rejected by the applicable protection mask. |
| `decay.wall_collisions` (int) | Resolved decay contacts with blocking wall cells. |
| `drain.boundary_reflections` (int) | Resolved drain reflections at simulation bounds. |
| `drain.buf_drain_cache_hwm` (int) | High-water live length of the reusable drain cache buffer/state collection. |
| `drain.buf_pending_spawns_hwm` (int) | High-water live length of the reusable pending spawns buffer/state collection. |
| `drain.grid_steps` (int) | Grid-traversal steps executed by swept drain movers. |
| `drain.killed_by_lifecycle` (int) | drain deaths with no resolved roster-cursor killer. |
| `drain.killed_by_player` (int) | drain deaths credited to a resolved roster cursor. |
| `drain.paused` (bool) | Whether a spawning hold applies to this instance's own cursor. A session-wide region holds every participant; a quasar holds only the cursor it was fused from. |
| `drain.protected_rejects` (int) | drain interactions rejected by the applicable protection mask. |
| `drain.wall_collisions` (int) | Resolved drain contacts with blocking wall cells. |
| `dust.boundary_reflections` (int) | Resolved dust reflections at simulation bounds. |
| `dust.buf_collision_cells_hwm` (int) | High-water live length of the reusable collision cells buffer/state collection. |
| `dust.buf_collision_impulses_hwm` (int) | High-water live length of the reusable collision impulses buffer/state collection. |
| `dust.buf_combat_headers_hwm` (int) | High-water live length of the reusable combat headers buffer/state collection. |
| `dust.buf_death_hwm` (int) | High-water live length of the reusable death buffer/state collection. |
| `dust.buf_destroy_hwm` (int) | High-water live length of the reusable destroy buffer/state collection. |
| `dust.buf_flash_hwm` (int) | High-water live length of the reusable flash buffer/state collection. |
| `dust.buf_transform_hwm` (int) | High-water live length of the reusable transform buffer/state collection. |
| `dust.grid_steps` (int) | Grid-traversal steps executed by swept dust movers. |
| `dust.wall_collisions` (int) | Resolved dust contacts with blocking wall cells. |
| `energy.cursor_rejects` (int) | Requests rejected because energy could not resolve a roster cursor. |
| `energy.disabled_rejects` (int) | Action requests dropped while the energy system was disabled. |
| `energy.missing_energy_rejects` (int) | Resolved cursor requests rejected because the cursor lacked an energy component. |
| `energy.penalty_rejects` (int) | Energy penalties rejected by boost or ember protection. |
| `event.invalid` (int) | Dispatched events whose type was outside the declared event-type range. |
| `event.settle_exhausted` (int) | Settle operations that reached the pass cap with events still queued. |
| `event.settle_input` (int) | Nonempty event settle passes attributed to source label `input`. |
| `event.settle_loop` (int) | Nonempty event settle passes attributed to source label `loop`. |
| `event.settle_post` (int) | Nonempty event settle passes attributed to source label `post`. |
| `event.settle_pre` (int) | Nonempty event settle passes attributed to source label `pre`. |
| `event.settle_reset` (int) | Nonempty event settle passes attributed to source label `reset`. |
| `event.settle_settle` (int) | Nonempty event settle passes attributed to source label `settle`. |
| `explosion.buf_centers_hwm` (int) | High-water live length of the reusable centers buffer/state collection. |
| `explosion.buf_composite_index_hwm` (int) | High-water live length of the reusable composite index buffer/state collection. |
| `explosion.buf_composites_hwm` (int) | High-water live length of the reusable composites buffer/state collection. |
| `explosion.buf_drains_hwm` (int) | High-water live length of the reusable drains buffer/state collection. |
| `explosion.buf_dust_entries_hwm` (int) | High-water live length of the reusable dust entries buffer/state collection. |
| `explosion.buf_entities_hwm` (int) | High-water live length of the reusable entities buffer/state collection. |
| `explosion.buf_seen_cells_hwm` (int) | High-water live length of the reusable seen cells buffer/state collection. |
| `explosion.cursor_rejects` (int) | Requests rejected because explosion could not resolve a roster cursor. |
| `explosion.disabled_rejects` (int) | Action requests dropped while the explosion system was disabled. |
| `eye.boundary_reflections` (int) | Resolved eye reflections at simulation bounds. |
| `eye.buf_ga_pending_deaths_hwm` (int) | High-water live length of the reusable ga pending deaths buffer/state collection. |
| `eye.buf_ga_track_keys_hwm` (int) | High-water live length of the reusable ga track keys buffer/state collection. |
| `eye.buf_ga_tracking_hwm` (int) | High-water live length of the reusable ga tracking buffer/state collection. |
| `eye.buf_ga_typefit_hwm` (int) | High-water live length of the reusable ga typefit buffer/state collection. |
| `eye.despawned` (int) | eye instances removed by cancellation or integrity cleanup. |
| `eye.killed_by_lifecycle` (int) | eye deaths with no resolved roster-cursor killer. |
| `eye.killed_by_player` (int) | eye deaths credited to a resolved roster cursor. |
| `eye.physics_steps` (int) | Physics integration substeps executed for eye movers. |
| `eye.protected_player_rejects` (int) | eye interactions rejected on player-domain victims. |
| `eye.protected_rejects` (int) | eye interactions rejected by the applicable protection mask. |
| `eye.spawn_failures` (int) | eye spawn requests that could not produce an entity. |
| `eye.spawned` (int) | Successfully created eye lifecycle instances. |
| `eye.wall_collisions` (int) | Resolved eye contacts with blocking wall cells. |
| `fuse.buf_pending_hwm` (int) | High-water live length of the reusable pending buffer/state collection. |
| `fuse.disabled_rejects` (int) | Action requests dropped while the fuse system was disabled. |
| `fuse.spawn_failures` (int) | fuse spawn requests that could not produce an entity. |
| `glyph.buf_placement_hwm` (int) | High-water live length of the reusable placement buffer/state collection. |
| `gold.cursor_rejects` (int) | Requests rejected because gold could not resolve a roster cursor. |
| `gold.disabled_rejects` (int) | Action requests dropped while the gold system was disabled. |
| `gold.spawn_failures` (int) | gold spawn requests that could not produce an entity. |
| `heat.cursor_rejects` (int) | Requests rejected because heat could not resolve a roster cursor. |
| `heat.disabled_rejects` (int) | Action requests dropped while the heat system was disabled. |
| `loot.boundary_reflections` (int) | Resolved loot reflections at simulation bounds. |
| `loot.buf_pity_hwm` (int) | High-water live length of the reusable pity buffer/state collection. |
| `loot.buf_routes_hwm` (int) | High-water count of owner flow fields held at once. |
| `loot.routes` (int) | Owner flow fields currently held: one per cursor that has dropped something and still exists. |
| `loot.route_recomputes` (int) | Owner flow fields rebuilt, cumulative. A drop in flight rebuilds its owner's field when that cursor moves past the dirty distance, so this rises with player movement while loot is out and not otherwise. |
| `loot.unreachable` (int) | Drops with no route home this tick: their owner is gone, or walls seal them off. They bleed to rest rather than press against what is in the way, so a sustained nonzero reading is geometry, not motion. |
| `loot.physics_steps` (int) | Physics integration substeps executed for loot movers. |
| `loot.wall_collisions` (int) | Resolved loot contacts with blocking wall cells. |
| `missile.boundary_hits` (int) | missile swept paths terminated at simulation bounds. |
| `missile.disabled_rejects` (int) | Action requests dropped while the missile system was disabled. |
| `missile.grid_steps` (int) | Grid-traversal steps executed by swept missile movers. |
| `missile.wall_collisions` (int) | Resolved missile contacts with blocking wall cells. |
| `motion_marker.buf_base_markers_hwm` (int) | High-water live length of the reusable base markers buffer/state collection. |
| `motion_marker.buf_base_positions_hwm` (int) | High-water live length of the reusable base positions buffer/state collection. |
| `motion_marker.buf_colored_markers_hwm` (int) | High-water live length of the reusable colored markers buffer/state collection. |
| `nav.buf_groups_hwm` (int) | High-water live length of the reusable groups buffer/state collection. |
| `nugget.cursor_rejects` (int) | Requests rejected because nugget could not resolve a roster cursor. |
| `nugget.disabled_rejects` (int) | Action requests dropped while the nugget system was disabled. |
| `nugget.spawn_failures` (int) | nugget spawn requests that could not produce an entity. |
| `network.barrier_applied_local` (int) | Deferred local artifacts admitted at their playout boundary. Only the three that still take one: an arrival, a departure and a reset. Everything else a participant produces is in `network.crossings_local`. |
| `network.barrier_applied_peer` (int) | Peer artifacts admitted at their playout boundary. |
| `network.barrier_deferred` (int) | Local crossing artifacts the barrier took ownership of, which since Phase 4 is only the three that create or destroy a shared entity. |
| `network.barrier_late` (int) | Artifacts admitted after their scheduled apply tick. Under an authority this is an ordering decision rather than a divergence — the host applies what reaches it in the order it reaches it — but it is still what says a participant's link is not keeping the playout lead. |
| `network.barrier_peer_applied` (bool) | Whether the most recent boundary admitted a peer artifact. |
| `network.barrier_peer_artifacts` (int) | Peer artifacts admitted at the most recent boundary. |
| `network.barrier_peer_lag_ticks` (int) | Closed-epoch lag beyond the negotiated playout lead. |
| `network.barrier_ran_without_peer` (int) | Tick boundaries reached before every required peer epoch marker. |
| `network.crossings_received` (int) | Peer crossing artifacts decoded and admitted. |
| `network.crossings_sent` (int) | Local crossing artifacts sent in closed epochs. |
| `network.crossings_local` (int) | Local crossing artifacts applied in the tick that produced them rather than at the playout lead (D-3 as Phase 4 changed it). |
| `network.corrections_received` (int) | Authoritative corrections reassembled from the wire. |
| `network.artifacts_refused` (int) | Peer artifacts the authority refused — an arrival or a departure produced by a participant that is not the coordinator. |
| `network.digest_mismatches` (int) | Runtime parity samples that disagreed. A gauge under weakened D-11, not a fault: a guest differs from the host between corrections by design. |
| `network.drift_part` (string) | The surface the last disagreement was in: positions, kinetics, combat, context, status or snapshot. |
| `network.drift_tick` (int) | The tick it was seen on. |
| `network.lag_ticks` (int) | How far behind the newest tick any peer has been seen closing this instance stands, measured every tick. |
| `network.stale` (bool) | Whether that lag exceeds the playout lead, past which this participant's own crossings reach the host after the ticks they name. Shown in the status bar as `LAG n`. |
| `network.connected` (bool) | Whether at least one session peer is currently connected. |
| `network.frames_dropped` (int) | Transport frames rejected by encoding, framing, ordering or identity checks. |
| `network.map_latched` (bool) | Whether the host-authored D-14 map dimensions are latched. |
| `network.peers` (int) | Current connected session-peer count. |
| `network.state` (string) | Operator connection state: off, connecting, connected or disconnected. |
| `network.state_applied` (int) | Owner-authored cursor snapshots applied to remote cursors. |
| `network.artifacts_pre_install` (int) | Peer artifacts refused because the installed world already contains them — everything due at or before a capture's tick (D-22). Non-zero on a joiner is the ordering working, not a loss. |
| `network.artifacts_refused_tick` (int) | Peer artifacts naming an apply tick past `parameter.NetworkApplyWindowTicks` ahead of this instance. The window admits a fresh join's whole catch-up, so a non-zero count is a sender whose ticks are not a participant's position. |
| `network.artifacts_schedule_full` (int) | Peer artifacts turned away because the barrier schedule was at `NetworkScheduledMax` entries or `NetworkScheduledBytes`. Distinct from `frames_dropped`: these are frames this instance read and refused, which is a different diagnosis from one it could not read. |
| `network.join_lag_ticks` (int) | Ticks this participant still stood behind the session after its join caught up. Zero is the ordinary result; anything above the playout lead refuses the join. |
| `network.link_rtt_ms` (int) | The worst peer's smoothed round trip, in milliseconds — the unit a person reads and the unit `tc netem delay` speaks. A loopback link rounds to zero here, which is why the microsecond form exists beside it. |
| `network.link_rtt_us` (int) | The same round trip at the resolution the estimator works in, so a local session can show that the measurement exists at all. |
| `network.link_jitter_ms` (int) | Its smoothed variation. A cadence is held at or above twice this: publishing faster than a link's own timing noise only bunches corrections. |
| `network.link_bps` (int) | The delivery rate that link was measured at, in bytes per second. Read it with `link_saturated`: a rate measured while nothing was queued is a lower bound on capacity and not a measurement of it. |
| `network.link_loss_pct` (int) | The share of probes that went unanswered. An unanswered probe is the only loss signal this protocol has — nothing acknowledges a correction and nothing repairs an epoch. |
| `network.link_saturated` (bool) | Whether the link, rather than the sender, was the limit while `link_bps` was measured — from a standing backlog or a round trip inflated past its own baseline. |
| `ping.cursor_rejects` (int) | Requests rejected because ping could not resolve a roster cursor. |
| `snapshot.bytes` (int) | Encoded length of the last shared-world capture this instance read. |
| `snapshot.capture_us` (int) | What that read cost under the world lock — the one stall a join is allowed. |
| `snapshot.encode_us` (int) | What encoding it cost outside the lock, which stalls nothing. |
| `snapshot.stage_us` (int) | What resolving the last received capture into a staging world cost. |
| `snapshot.commit_us` (int) | What writing it into the live world cost. |
| `snapshot.install_tick` (int) | The tick the last installed capture described. |
| `snapshot.catch_up_ticks` (int) | Ticks simulated after the install to close the gap the transfer opened. |
| `snapshot.keyframes` (int) | Whole captures this host has published, as a join's gate or as a cadence keyframe. |
| `snapshot.corrections_sent` (int) | Corrections broadcast, keyframes and deltas together. |
| `snapshot.correction_bytes_sent` (int) | Their total encoded size, which is the uplink the cadence costs. |
| `snapshot.corrections_applied` (int) | Corrections this instance installed. |
| `snapshot.corrections_refused` (int) | Corrections that could not be resolved — a delta naming a keyframe this instance does not hold, or a body its header does not describe. Not an error: the next keyframe is self-sufficient. |
| `snapshot.corrections_superseded` (int) | Corrections a fresher one overtook before they were applied, plus any the queue dropped. |
| `snapshot.correction_entries` (int) | Component cells the last correction moved: how far this instance's prediction had drifted when the authority arrived. |
| `snapshot.correction_entities` (int) | The distinct shared entities behind them. Shown in the status bar as `COR n`. |
| `snapshot.correction_cells` (int) | The largest distance a shared placement moved — the correction a player would actually see. |
| `snapshot.correction_tick` (int) | The tick the last applied correction described. |
| `snapshot.cadence_ticks` (int) | The correction cadence in force, in ticks. The publication timeline's base — the fastest peer's plan — rather than any one peer's, which `:session` and `CadenceReport` name individually. |
| `snapshot.cadence_keyframe_interval` (int) | Corrections between whole captures at that cadence. |
| `snapshot.cadence_keyframe_period_ticks` (int) | Their product: the ticks the session leaves between whole authoritative worlds. This is the value the convergence floor bounds, and the first one to read — a value above `SnapshotFloorKeyframeTicks` is a defect in the controller rather than a slow link. |
| `snapshot.cadence_keyframe_age_ticks` (int) | The *receiving* end of the same guarantee: how long this instance has actually gone without a whole authoritative world. The host promises one per floor window; this says whether one arrived. |
| `snapshot.cadence_uplink_bps` (int) | What the schedule in force costs, in bytes per second. |
| `snapshot.cadence_budget_bps` (int) | What the tightest link was measured to allow after the utilisation share. Zero means no link has been measured *while saturated*, which on a loopback or a fat link is the ordinary case. |
| `snapshot.cadence_floor_bps` (int) | What the convergence floor costs on a world this size: the cheapest schedule that still delivers a whole world per floor window. A budget below it is the unrecoverable condition. |
| `snapshot.cadence_constrained` (bool) | The link moved the operating point *worse* than nominal. This is the design working: prediction carries more, the magnitude rises and stays bounded. Shown in the status bar as `LNK`. |
| `snapshot.cadence_floor_breached` (bool) | No schedule the controller may choose delivers a whole world inside the guaranteed window, or none arrived. This is not the design working, and it is the one condition adaptation may not hide. Shown as `LINK!`. |
| `snapshot.manifests_sent` (int) | Correction indexes this host published. |
| `snapshot.manifests_received` (int) | Indexes this receiver was sent, including the ones an older one was superseded by before it could be answered. |
| `snapshot.manifest_bytes_sent` (int) | What those indexes cost, per peer served. On a converged link this is the whole of a correction. |
| `snapshot.manifest_bytes_received` (int) | The same figure from the receiving end. |
| `snapshot.corrections_hash_only` (int) | Corrections that carried no state at all: the roots agreed and the receiver proved it with a hash. This is the number the whole selective path exists to raise. |
| `snapshot.sections_compared` (int) | Section hashes examined while answering indexes. One per section per manifest, which is the cost of proving convergence. |
| `snapshot.pages_compared` (int) | Page hashes computed while descending into the sections that disagreed. Zero on a converged correction — the descent stops at the root. |
| `snapshot.hash_us` (int) | What indexing and comparing one capture cost *outside* the world lock. Read it beside `snapshot.capture_us`: if the second grows with the first, work has moved under the lock that should not have. |
| `snapshot.selective_bytes` (int) | What a non-keyframe correction currently costs on the wire, measured rather than assumed, and the figure the cadence controller and admission are priced from. |
| `snapshot.shards_requested` (int) | Pages a receiver put in play by sending its own hashes for them. |
| `snapshot.shards_sent` (int) | Pages actually returned, which is the subset that differed. |
| `snapshot.shards_received` (int) | Pages that arrived, including those in a repair later refused. |
| `snapshot.shards_refused` (int) | Pages in a repair that was not applied — a baseline this instance has moved past, a failed proof, a request this host could not build. Not an error: every one of them ends at a keyframe. |
| `snapshot.shards_applied` (int) | Pages spliced into a capture whose root then verified. |
| `snapshot.shard_bytes_sent` (int) | What the repairs cost, per peer served. |
| `snapshot.shard_bytes_received` (int) | The same from the receiving end, including refused repairs. |
| `snapshot.request_bytes` (int) | What the answers to indexes cost — an acknowledgement when the roots agreed, a page vector when they did not. |
| `snapshot.pages_repaired` (int) | Pages a verified repair replaced. |
| `snapshot.entities_repaired` (int) | Distinct entities behind them, in the component-store sections. |
| `snapshot.cells_repaired` (int) | Indexed cells the repairs carried, across every section. |
| `snapshot.proof_failures` (int) | Repairs whose rows did not reproduce their declared page hash, or whose result did not reproduce the root the repair declared. Nothing was written in either case. |
| `snapshot.baseline_refusals` (int) | Indexes or repairs describing a run, session or baseline this instance is not holding. |
| `snapshot.keyframe_fallbacks` (int) | Times the exchange gave up and a whole world was sent or asked for: a retention miss, a failed proof, a repair wider than the capture it was repairing toward, or a floor the receiver could not meet. |
| `snapshot.replay_suffix_records` (int) | Crossings this instance is currently retaining for replay. |
| `snapshot.replay_records` (int) | Crossings replayed after corrections, cumulative. Each one is an action this participant took that a correction would otherwise have undone. |
| `snapshot.replay_overflow` (int) | Retained crossings dropped by the tick, count or byte bound. |
| `snapshot.replay_skipped` (int) | Corrections that installed the authority alone because the suffix was incomplete. A partial replay is a guess, so there is no partial answer. |
| `snapshot.replay_suffix_unavailable` (bool) | Whether the suffix was unavailable at the last correction. |
| `ping.disabled_rejects` (int) | Action requests dropped while the ping system was disabled. |
| `player.cursor_rejects` (int) | Requests rejected because player could not resolve a roster cursor. |
| `player.spawn_failures` (int) | player spawn requests that could not produce an entity. |
| `pylon.despawned` (int) | pylon instances removed by cancellation or integrity cleanup. |
| `pylon.killed_by_lifecycle` (int) | pylon deaths with no resolved roster-cursor killer. |
| `pylon.killed_by_player` (int) | pylon deaths credited to a resolved roster cursor. |
| `pylon.spawn_failures` (int) | pylon spawn requests that could not produce an entity. |
| `pylon.spawned` (int) | Successfully created pylon lifecycle instances. |
| `quasar.boundary_reflections` (int) | Resolved quasar reflections at simulation bounds. |
| `quasar.despawned` (int) | quasar instances removed by cancellation or integrity cleanup. |
| `quasar.killed_by_lifecycle` (int) | quasar deaths with no resolved roster-cursor killer. |
| `quasar.killed_by_player` (int) | quasar deaths credited to a resolved roster cursor. |
| `quasar.physics_steps` (int) | Physics integration substeps executed for quasar movers. |
| `quasar.protected_player_rejects` (int) | quasar interactions rejected on player-domain victims. |
| `quasar.protected_rejects` (int) | quasar interactions rejected by the applicable protection mask. |
| `quasar.spawn_failures` (int) | quasar spawn requests that could not produce an entity. |
| `quasar.spawned` (int) | Successfully created quasar lifecycle instances. |
| `quasar.wall_collisions` (int) | Resolved quasar contacts with blocking wall cells. |
| `shield.cursor_rejects` (int) | Requests rejected because shield could not resolve a roster cursor. |
| `shield.disabled_rejects` (int) | Action requests dropped while the shield system was disabled. |
| `session.all_defeated` (bool) | Every currently rostered cursor has crossed its terminal heat/energy state. |
| `snake.boundary_reflections` (int) | Resolved snake reflections at simulation bounds. |
| `snake.despawned` (int) | snake instances removed by cancellation or integrity cleanup. |
| `snake.killed_by_lifecycle` (int) | snake deaths with no resolved roster-cursor killer. |
| `snake.killed_by_player` (int) | snake deaths credited to a resolved roster cursor. |
| `snake.physics_steps` (int) | Physics integration substeps executed for snake movers. |
| `snake.protected_player_rejects` (int) | snake interactions rejected on player-domain victims. |
| `snake.protected_rejects` (int) | snake interactions rejected by the applicable protection mask. |
| `snake.spawn_failures` (int) | snake spawn requests that could not produce an entity. |
| `snake.spawned` (int) | Successfully created snake lifecycle instances. |
| `snake.wall_collisions` (int) | Resolved snake contacts with blocking wall cells. |
| `soft_collision.buf_drains_hwm` (int) | High-water live length of the reusable drains buffer/state collection. |
| `soft_collision.buf_pylons_hwm` (int) | High-water live length of the reusable pylons buffer/state collection. |
| `soft_collision.buf_quasars_hwm` (int) | High-water live length of the reusable quasars buffer/state collection. |
| `soft_collision.buf_storms_hwm` (int) | High-water live length of the reusable storms buffer/state collection. |
| `soft_collision.buf_swarms_hwm` (int) | High-water live length of the reusable swarms buffer/state collection. |
| `soft_collision.collisions` (int) | Soft-collision pairs that resolved to separation impulses. |
| `soft_collision.immune_rejects` (int) | Soft-collision pairs skipped because either entity was collision-immune. |
| `spatial.cell_occupancy_hwm` (int) | Highest successful entity occupancy observed in any spatial cell. |
| `spatial.cell_overflows` (int) | Spatial insertions dropped because a cell already held 31 entities. |
| `spatial.cell_saturations` (int) | Transitions where a cell first reached the 31-entity capacity. |
| `spatial.indexed_entities` (int) | Snapshot-cadence gauge of entities currently represented in spatial cells. |
| `spatial.max_cell_occupancy` (int) | Snapshot-cadence gauge of the fullest current spatial cell. |
| `spatial.occupied_cells` (int) | Snapshot-cadence gauge of nonempty spatial cells. |
| `spatial.position_batch_hwm` (int) | Largest pending position batch committed during the session. |
| `spatial.positions_hwm` (int) | Highest live length of the dense position store. |
| `spirit.buf_destroy_next_tick_hwm` (int) | High-water live length of the reusable destroy next tick buffer/state collection. |
| `storm.boundary_reflections` (int) | Resolved storm reflections at simulation bounds. |
| `storm.buf_ellipse_offsets_hwm` (int) | High-water live length of the reusable ellipse offsets buffer/state collection. |
| `storm.buf_member_excludes_hwm` (int) | High-water live length of the reusable member excludes buffer/state collection. |
| `storm.buf_pending_blue_spawns_hwm` (int) | High-water live length of the reusable pending blue spawns buffer/state collection. |
| `storm.despawned` (int) | storm instances removed by cancellation or integrity cleanup. |
| `storm.killed_by_lifecycle` (int) | storm deaths with no resolved roster-cursor killer. |
| `storm.killed_by_player` (int) | storm deaths credited to a resolved roster cursor. |
| `storm.physics_steps` (int) | Physics integration substeps executed for storm movers. |
| `storm.protected_player_rejects` (int) | storm interactions rejected on player-domain victims. |
| `storm.protected_rejects` (int) | storm interactions rejected by the applicable protection mask. |
| `storm.spawn_failures` (int) | storm spawn requests that could not produce an entity. |
| `storm.spawned` (int) | Successfully created storm lifecycle instances. |
| `storm.wall_collisions` (int) | Resolved storm contacts with blocking wall cells. |
| `swarm.boundary_reflections` (int) | Resolved swarm reflections at simulation bounds. |
| `swarm.despawned` (int) | swarm instances removed by cancellation or integrity cleanup. |
| `swarm.killed_by_lifecycle` (int) | swarm deaths with no resolved roster-cursor killer. |
| `swarm.killed_by_player` (int) | swarm deaths credited to a resolved roster cursor. |
| `swarm.physics_steps` (int) | Physics integration substeps executed for swarm movers. |
| `swarm.protected_player_rejects` (int) | swarm interactions rejected on player-domain victims. |
| `swarm.protected_rejects` (int) | swarm interactions rejected by the applicable protection mask. |
| `swarm.spawn_failures` (int) | swarm spawn requests that could not produce an entity. |
| `swarm.spawned` (int) | Successfully created swarm lifecycle instances. |
| `swarm.wall_collisions` (int) | Resolved swarm contacts with blocking wall cells. |
| `tower.despawned` (int) | tower instances removed by cancellation or integrity cleanup. |
| `tower.killed_by_lifecycle` (int) | tower deaths with no resolved roster-cursor killer. |
| `tower.killed_by_player` (int) | tower deaths credited to a resolved roster cursor. |
| `tower.spawn_failures` (int) | tower spawn requests that could not produce an entity. |
| `tower.spawned` (int) | Successfully created tower lifecycle instances. |
| `typing.buf_delete_hwm` (int) | High-water live length of the reusable delete buffer/state collection. |
| `typing.cursor_rejects` (int) | Requests rejected because typing could not resolve a roster cursor. |
| `typing.disabled_rejects` (int) | Action requests dropped while the typing system was disabled. |
| `wall.buf_pending_push_checks_hwm` (int) | High-water live length of the reusable pending push checks buffer/state collection. |
| `weapon.orbs_reaped` (int) | Orbs the `Orb` store held that no loadout justified — a duplicate, one owned by a cursor this instance does not simulate, or one whose charges are gone. Zero is the ordinary reading; a rising count is an orb lifecycle the store-derived index disagrees with. |
| `weapon.cursor_rejects` (int) | Requests rejected because weapon could not resolve a roster cursor. |
| `weapon.disabled_rejects` (int) | Action requests dropped while the weapon system was disabled. |
| `event.dead_by_type` (string) | Snapshot-cadence sparse `EventType=count` dead-letter summary. |
| `event.dispatch_by_type` (string) | Snapshot-cadence sparse `EventType=count` dispatch summary. |

## Headless evidence

The deterministic headless script followed by one snapshot interval reported:

- 215 ticks, 204 event dispatches, 441 live positioned entities, and 934 created entities.
- Two bit-packed single deaths, zero fallback deaths, five death batches, and 14 batch entity entries.
- The fast-path-to-batch ratio was 2:5 by dispatch (0.40 packed events per batch), or 1:7 by entity workload (2 packed entities to 14 batch entries).
- `entity.count` and `event.queue_len` matched their live stores at the snapshot boundary.
