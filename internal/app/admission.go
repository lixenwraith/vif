// Admission control for dials.
//
// A dedicated host is reachable by anything that can open a TCP connection to it,
// and the expensive part of a join is not the handshake but what follows: the
// coordinator allocates an identity and a roster slot, then reads, encodes and
// sends a whole world. A peer that joins and leaves in a loop therefore costs the
// session a capture per cycle while costing itself one connect, and that
// amplification is what this bounds.
//
// The key is the dialling address rather than the participant identity, because an
// identity is what the attack consumes and is released the moment the connection
// drops — counting identities would be counting the damage rather than preventing
// it. Addresses are coarse and a NAT is one key, so the budget is deliberately far
// above what a person reconnecting after a crash needs.

package app

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// admissionLimiter is a fixed-window counter per dialling host.
//
// A fixed window rather than a sliding one: the budget is two orders of magnitude
// above ordinary use, so the boundary effect a fixed window allows — twice the
// burst across two adjacent windows — costs nothing that matters, and it keeps the
// state one integer and one timestamp per key rather than a slice of them.
type admissionLimiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	max    int
	seen   map[string]*admissionCount
}

type admissionCount struct {
	opened time.Time
	count  int
}

func newAdmissionLimiter() *admissionLimiter {
	return &admissionLimiter{
		window: parameter.NetworkAdmitWindow,
		burst:  parameter.NetworkAdmitBurst,
		max:    parameter.NetworkAdmitTracked,
		seen:   make(map[string]*admissionCount),
	}
}

// admit records one dial from addr and reports whether it may proceed.
func (l *admissionLimiter) admit(addr net.Addr) error {
	key := admissionKey(addr)
	if key == "" {
		return nil // nothing to attribute a budget to; the transport is not a socket
	}
	now := time.Now() // [wall] a rate over real time, not a game one

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.seen[key]
	switch {
	case ok && now.Sub(entry.opened) >= l.window:
		entry.opened, entry.count = now, 0
	case !ok:
		l.sweepLocked(now)
		if len(l.seen) >= l.max {
			// Fails closed, and the posture is deliberate. A completed TCP handshake
			// proves the source address, so a table this wide is many real hosts
			// dialling at once rather than one forging them — and a session with a
			// roster of sixteen has no reading of that in which the next dial is the
			// one it was waiting for.
			return fmt.Errorf("admission: %d dialling hosts already tracked", len(l.seen))
		}
		entry = &admissionCount{opened: now}
		l.seen[key] = entry
	}

	if entry.count >= l.burst {
		return fmt.Errorf("admission: %s has joined %d times within %s",
			key, entry.count, l.window)
	}
	entry.count++
	return nil
}

// sweepLocked drops the keys whose window has passed. It runs only when a new key
// arrives, so the table is walked at most once per unseen host rather than per
// dial. Caller MUST hold mu.
func (l *admissionLimiter) sweepLocked(now time.Time) {
	for key, entry := range l.seen {
		if now.Sub(entry.opened) >= l.window {
			delete(l.seen, key)
		}
	}
}

// admissionKey is the host half of a network address. The port is what a dialer
// changes for free, so counting it would count nothing.
func admissionKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	s := addr.String()
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}
