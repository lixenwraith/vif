package snapshot

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/status"
)

// Telemetry is what a capture, a correction and the selective exchange cost the
// instance that ran them. It is per-instance and excluded from the compared
// surface: a host publishes what a read cost it and a guest what an install cost
// it, and neither is a fact about the world they share.
type Telemetry struct {
	// Capture and install cost.
	CaptureUS, EncodeUS, Bytes, StageUS, CommitUS, InstallTick, CatchUp *atomic.Int64

	// The cadence: what the host published, what the guest did with it. A refused
	// delta is one whose keyframe this instance does not hold and a superseded one
	// was overtaken; both resolve at the next keyframe rather than being errors.
	Sent, SentBytes, Keyframes, Applied, Refused, Superseded *atomic.Int64

	// How far this instance's prediction had drifted when the authority arrived:
	// component cells, the entities behind them, and the largest placement shift.
	CorrectionEntries, CorrectionEntities, CorrectionCells, CorrectionTick *atomic.Int64

	// The operating point in force. KeyframePeriod is CadenceTicks times
	// KeyframeInterval, which is what the convergence floor bounds. The three rates
	// are bytes per second: what the schedule costs, what the tightest link allows,
	// and what the floor costs on a world this size.
	CadenceTicks, KeyframeInterval, KeyframePeriod *atomic.Int64
	UplinkBps, BudgetBps, FloorBps                 *atomic.Int64
	Constrained, FloorBreached                     *atomic.Bool

	// KeyframeAge is the receiving end of the floor: ticks since a whole
	// authoritative world last arrived.
	KeyframeAge *atomic.Int64

	// The index exchange: what it cost, and how often it proved convergence
	// outright. HashOnly is the case the design is for.
	ManifestSent, ManifestRecv, ManifestBytesSent, ManifestBytesRecv *atomic.Int64
	HashOnly, SectionsCompared, PagesCompared                        *atomic.Int64

	// The repair, counted at every point a shard can be at, so a gap between two
	// of them names which side dropped it.
	ShardsRequested, ShardsSent, ShardsRecv, ShardsRefused, ShardsApplied *atomic.Int64
	ShardBytesSent, ShardBytesRecv, RequestBytes, SelectiveBytes          *atomic.Int64

	// What a repair moved, and what refused one. Neither refusal is an error
	// condition: both end at the keyframe fallback.
	PagesRepaired, EntitiesRepaired, CellsRepaired    *atomic.Int64
	ProofFailures, BaselineRefusals, KeyframeFallback *atomic.Int64

	// HashUS is what indexing and comparing one capture cost outside the world
	// lock, beside CaptureUS which is the bounded read inside it.
	HashUS *atomic.Int64

	// The bounded replay suffix: what is held, what the last correction re-applied,
	// what retention dropped, and how many corrections fell back to the authority.
	ReplaySuffix, ReplayReplayed, ReplayOverflow, ReplaySkipped *atomic.Int64
	ReplayUnusable                                              *atomic.Bool

	// Authority continuity. StaleTerm counts artifacts dropped as belonging to a
	// generation the session has left, the ordinary in-flight case across a handoff.
	StaleTerm, HandoffBytes *atomic.Int64

	// The relay role's bounded staleness, made countable. The bytes are priced
	// against the relaying participant's own link, never the authority's.
	RelayRetained, RelayServed, RelayUnserved *atomic.Int64
	RelayBytesSent, RelayBytesRecv            *atomic.Int64
}

// NewTelemetry reserves the cells. Called during construction, because a key
// first written after Freeze is counted late rather than stored.
func NewTelemetry(reg *status.Registry) Telemetry {
	i := reg.Ints.Get
	b := reg.Bools.Get
	return Telemetry{
		CaptureUS: i("snapshot.capture_us"), EncodeUS: i("snapshot.encode_us"),
		Bytes: i("snapshot.bytes"), StageUS: i("snapshot.stage_us"),
		CommitUS: i("snapshot.commit_us"), InstallTick: i("snapshot.install_tick"),
		CatchUp: i("snapshot.catch_up_ticks"),

		Sent: i("snapshot.corrections_sent"), SentBytes: i("snapshot.correction_bytes_sent"),
		Keyframes: i("snapshot.keyframes"), Applied: i("snapshot.corrections_applied"),
		Refused: i("snapshot.corrections_refused"), Superseded: i("snapshot.corrections_superseded"),

		CorrectionEntries:  i("snapshot.correction_entries"),
		CorrectionEntities: i("snapshot.correction_entities"),
		CorrectionCells:    i("snapshot.correction_cells"),
		CorrectionTick:     i("snapshot.correction_tick"),

		CadenceTicks:     i("snapshot.cadence_ticks"),
		KeyframeInterval: i("snapshot.cadence_keyframe_interval"),
		KeyframePeriod:   i("snapshot.cadence_keyframe_period_ticks"),
		UplinkBps:        i("snapshot.cadence_uplink_bps"), BudgetBps: i("snapshot.cadence_budget_bps"),
		FloorBps:      i("snapshot.cadence_floor_bps"),
		Constrained:   b("snapshot.cadence_constrained"),
		FloorBreached: b("snapshot.cadence_floor_breached"),
		KeyframeAge:   i("snapshot.cadence_keyframe_age_ticks"),

		ManifestSent: i("snapshot.manifests_sent"), ManifestRecv: i("snapshot.manifests_received"),
		ManifestBytesSent: i("snapshot.manifest_bytes_sent"),
		ManifestBytesRecv: i("snapshot.manifest_bytes_received"),
		HashOnly:          i("snapshot.corrections_hash_only"),
		SectionsCompared:  i("snapshot.sections_compared"), PagesCompared: i("snapshot.pages_compared"),

		ShardsRequested: i("snapshot.shards_requested"), ShardsSent: i("snapshot.shards_sent"),
		ShardsRecv: i("snapshot.shards_received"), ShardsRefused: i("snapshot.shards_refused"),
		ShardsApplied:  i("snapshot.shards_applied"),
		ShardBytesSent: i("snapshot.shard_bytes_sent"), ShardBytesRecv: i("snapshot.shard_bytes_received"),
		RequestBytes: i("snapshot.request_bytes"), SelectiveBytes: i("snapshot.selective_bytes"),

		PagesRepaired: i("snapshot.pages_repaired"), EntitiesRepaired: i("snapshot.entities_repaired"),
		CellsRepaired: i("snapshot.cells_repaired"), ProofFailures: i("snapshot.proof_failures"),
		BaselineRefusals: i("snapshot.baseline_refusals"),
		KeyframeFallback: i("snapshot.keyframe_fallbacks"),

		HashUS: i("snapshot.hash_us"),

		ReplaySuffix: i("snapshot.replay_suffix_records"), ReplayReplayed: i("snapshot.replay_records"),
		ReplayOverflow: i("snapshot.replay_overflow"), ReplaySkipped: i("snapshot.replay_skipped"),
		ReplayUnusable: b("snapshot.replay_suffix_unavailable"),

		StaleTerm: i("network.term_stale"), HandoffBytes: i("network.handoff_bytes"),

		RelayRetained: i("snapshot.relay_retained"), RelayServed: i("snapshot.relay_served"),
		RelayUnserved:  i("snapshot.relay_unserved"),
		RelayBytesSent: i("snapshot.relay_bytes_sent"), RelayBytesRecv: i("snapshot.relay_bytes_received"),
	}
}
