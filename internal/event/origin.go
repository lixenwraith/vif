package event

// Origin identifies the producer of an event. The dispatcher never branches on
// it; it is carried and journaled only.
type Origin uint8

const (
	OriginSystem  Origin = iota // Simulation-internal, not journaled
	OriginInput                 // Keyboard or mouse, via the mode router
	OriginMacro                 // Macro playback and auto-fire, which is a macro by another name
	OriginCommand               // Ex command line
	OriginNetwork               // Remote producer
	OriginDebug                 // Harness and out-of-band control such as :region
	OriginSession               // Session layer, from a transport observation
	OriginDevice                // This machine's own output, such as the audio mute
	originCount
)

// originNames indexes by Origin; a missing entry surfaces as "invalid" rather than ""
var originNames = [originCount]string{
	"system", "input", "macro", "command", "network", "debug", "session", "device",
}

// String returns the journal name for the origin
func (o Origin) String() string {
	if o >= originCount || originNames[o] == "" {
		return "invalid"
	}
	return originNames[o]
}

// Journaled reports whether events from this origin enter the replay journal.
// OriginSession is, because no other record implies a roster change; it differs from
// OriginNetwork in that it still has to cross. OriginDevice is not: the speakers a run
// played on are no part of the run, and whoever replays it has their own.
func (o Origin) Journaled() bool { return o != OriginSystem && o != OriginDevice && o < originCount }

// ParseOrigin resolves a journal name back to its origin
func ParseOrigin(s string) (Origin, bool) {
	for i, n := range originNames {
		if n == s {
			return Origin(i), true
		}
	}
	return OriginSystem, false
}
