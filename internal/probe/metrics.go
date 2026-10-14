package probe

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/status"
)

// metricPrefix namespaces every exported series. One prefix rather than a per-
// subsystem one: the registry's own keys already carry the subsystem, and a
// scrape wants to select this process apart from everything else on the node
// before it selects anything within it.
const metricPrefix = "vif_"

// handleMetrics renders the status registry in the Prometheus text format. It
// instruments nothing; it only names what the registry already counts. Strings are
// omitted: they are states (the FSM's active state, the drift surface) whose
// natural exposition is a label set this does not model.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if s.registry == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Collected before writing so the output is sorted: a scrape does not require
	// it, but a person diffing two of them does.
	lines := make([]string, 0, 256)
	s.registry.Ints.Range(func(key string, v *atomic.Int64) {
		lines = append(lines, sample(key, strconv.FormatInt(v.Load(), 10)))
	})
	s.registry.Floats.Range(func(key string, v *status.AtomicFloat) {
		lines = append(lines, sample(key, strconv.FormatFloat(v.Get(), 'g', -1, 64)))
	})
	s.registry.Bools.Range(func(key string, v *atomic.Bool) {
		n := "0"
		if v.Load() {
			n = "1"
		}
		lines = append(lines, sample(key, n))
	})
	sort.Strings(lines)

	w.WriteHeader(http.StatusOK)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	_, _ = w.Write([]byte(b.String()))
}

// sample renders one series. Every registry value is reported as a gauge: the
// counters among them are monotone only within a run, and a reset re-bases them,
// which is exactly what a counter must not do.
func sample(key, value string) string {
	return metricPrefix + metricName(key) + " " + value
}

// metricName maps a registry key onto the Prometheus name grammar: every character
// outside [A-Za-z0-9] becomes an underscore ("player.0.heat" to "player_0_heat"),
// and metricPrefix keeps a digit off the front. Keys differing only in punctuation
// would collide; the registry's keys differ in their words.
func metricName(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
