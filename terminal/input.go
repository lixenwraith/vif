package terminal

import (
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// EventType distinguishes input event categories
type EventType uint8

const (
	EventKey EventType = iota
	EventResize
	EventPaste  // Future: bracketed paste
	EventMouse  // Future: mouse support
	EventError  // Read error
	EventClosed // Input closed
)

// Event represents a terminal input event
type Event struct {
	Type      EventType
	Key       Key
	Rune      rune
	Modifiers Modifier
	Width     int // For EventResize
	Height    int // For EventResize
	Err       error
}

// inputReader handles raw stdin parsing
type inputReader struct {
	fd      int
	eventCh chan Event
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// escapeTimeout is the duration to wait after ESC to distinguish
// standalone ESC from escape sequence start
const escapeTimeout = 50 * time.Millisecond

// newInputReader creates a new input reader
func newInputReader(fd int) *inputReader {
	return &inputReader{
		fd:      fd,
		eventCh: make(chan Event, 64),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// start begins reading input in a goroutine
func (r *inputReader) start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go r.readLoop()
}

// stop signals the reader to stop
func (r *inputReader) stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	r.mu.Unlock()

	close(r.stopCh)
	// Wait with timeout - don't block forever if read is stuck
	select {
	case <-r.doneCh:
	case <-time.After(100 * time.Millisecond):
		// Reader stuck on blocking read, proceed anyway
	}
}

// events returns the event channel
func (r *inputReader) events() <-chan Event {
	return r.eventCh
}

// readLoop is the main input reading goroutine
func (r *inputReader) readLoop() {
	defer close(r.doneCh)

	buf := make([]byte, 256)

	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		// Use poll to check if data is available with timeout
		// This allows checking stopCh periodically
		ready, err := r.pollRead(100) // 100ms timeout
		if err != nil {
			select {
			case <-r.stopCh:
				return
			case r.eventCh <- Event{Type: EventError, Err: err}:
			}
			return
		}

		if !ready {
			continue // Timeout, check stopCh again
		}

		// Data available, read it
		n, err := unix.Read(r.fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN {
				continue
			}
			select {
			case <-r.stopCh:
				return
			case r.eventCh <- Event{Type: EventError, Err: err}:
			}
			return
		}

		if n == 0 {
			continue
		}

		r.parseInput(buf[:n])
	}
}

// pollRead checks if data is available on fd with timeout (milliseconds)
// Returns true if data available, false on timeout
func (r *inputReader) pollRead(timeoutMs int) (bool, error) {
	fds := []unix.PollFd{
		{Fd: int32(r.fd), Events: unix.POLLIN},
	}

	n, err := unix.Poll(fds, timeoutMs)
	if err != nil {
		if err == unix.EINTR {
			return false, nil // Interrupted, treat as timeout
		}
		return false, err
	}

	return n > 0 && (fds[0].Revents&unix.POLLIN) != 0, nil
}

// parseInput parses raw bytes into events
func (r *inputReader) parseInput(data []byte) {
	i := 0
	for i < len(data) {
		select {
		case <-r.stopCh:
			return
		default:
		}

		b := data[i]

		// Check for escape sequence
		if b == 0x1b {
			// Try to parse escape sequence
			consumed, ev := r.parseEscape(data[i:])
			if consumed > 0 {
				r.sendEvent(ev)
				i += consumed
				continue
			}
			// Standalone ESC
			r.sendEvent(Event{Type: EventKey, Key: KeyEscape})
			i++
			continue
		}

		// Control characters
		if b < 0x20 {
			ev := r.parseControl(b)
			r.sendEvent(ev)
			i++
			continue
		}

		// DEL
		if b == 0x7f {
			r.sendEvent(Event{Type: EventKey, Key: KeyBackspace})
			i++
			continue
		}

		// Regular character or UTF-8
		if b < 0x80 {
			r.sendEvent(Event{Type: EventKey, Key: KeyRune, Rune: rune(b)})
			i++
			continue
		}

		// UTF-8 multi-byte
		rn, size := decodeRune(data[i:])
		if size > 0 {
			r.sendEvent(Event{Type: EventKey, Key: KeyRune, Rune: rn})
			i += size
		} else {
			i++ // Skip invalid byte
		}
	}
}

// parseEscape attempts to parse an escape sequence
// Returns (bytes consumed, event) or (0, empty) if not a sequence
func (r *inputReader) parseEscape(data []byte) (int, Event) {
	if len(data) < 2 {
		// Need more data, try to read with timeout
		extra := r.readMoreWithTimeout()
		if len(extra) == 0 {
			return 0, Event{} // Standalone ESC
		}
		data = append(data, extra...)
	}

	// CSI sequence: ESC [
	if data[1] == '[' {
		return r.parseCSI(data)
	}

	// SS3 sequence: ESC O
	if data[1] == 'O' {
		return r.parseSS3(data)
	}

	// Alt+key: ESC followed by printable
	if data[1] >= 0x20 && data[1] < 0x7f {
		return 2, Event{
			Type:      EventKey,
			Key:       KeyRune,
			Rune:      rune(data[1]),
			Modifiers: ModAlt,
		}
	}

	return 0, Event{} // Unknown, treat ESC as standalone
}

// parseCSI parses a CSI sequence (ESC [ ...)
func (r *inputReader) parseCSI(data []byte) (int, Event) {
	// Minimum: ESC [ X
	if len(data) < 3 {
		return 0, Event{}
	}

	// Find end of sequence (letter or ~)
	end := 2
	for end < len(data) && end < 16 {
		b := data[end]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' {
			end++
			break
		}
		if b < 0x20 || b > 0x7e {
			// Invalid character in sequence
			return 0, Event{}
		}
		end++
	}

	seq := string(data[2:end])
	if key, mod, ok := lookupCSI(seq); ok {
		return end, Event{Type: EventKey, Key: key, Modifiers: mod}
	}

	return 0, Event{} // Unknown CSI sequence
}

// parseSS3 parses an SS3 sequence (ESC O ...)
func (r *inputReader) parseSS3(data []byte) (int, Event) {
	if len(data) < 3 {
		return 0, Event{}
	}

	seq := string(data[2:3])
	if key, mod, ok := lookupSS3(seq); ok {
		return 3, Event{Type: EventKey, Key: key, Modifiers: mod}
	}

	return 0, Event{}
}

// parseControl maps control characters to keys
func (r *inputReader) parseControl(b byte) Event {
	switch b {
	case 0x00: // Ctrl+Space or Ctrl+@
		return Event{Type: EventKey, Key: KeyCtrlSpace}
	case 0x01:
		return Event{Type: EventKey, Key: KeyCtrlA}
	case 0x02:
		return Event{Type: EventKey, Key: KeyCtrlB}
	case 0x03:
		return Event{Type: EventKey, Key: KeyCtrlC}
	case 0x04:
		return Event{Type: EventKey, Key: KeyCtrlD}
	case 0x05:
		return Event{Type: EventKey, Key: KeyCtrlE}
	case 0x06:
		return Event{Type: EventKey, Key: KeyCtrlF}
	case 0x07:
		return Event{Type: EventKey, Key: KeyCtrlG}
	case 0x08: // Ctrl+H or Backspace
		return Event{Type: EventKey, Key: KeyBackspace}
	case 0x09: // Tab
		return Event{Type: EventKey, Key: KeyTab}
	case 0x0a, 0x0d: // LF, CR (Enter)
		return Event{Type: EventKey, Key: KeyEnter}
	case 0x0b:
		return Event{Type: EventKey, Key: KeyCtrlK}
	case 0x0c:
		return Event{Type: EventKey, Key: KeyCtrlL}
	case 0x0e:
		return Event{Type: EventKey, Key: KeyCtrlN}
	case 0x0f:
		return Event{Type: EventKey, Key: KeyCtrlO}
	case 0x10:
		return Event{Type: EventKey, Key: KeyCtrlP}
	case 0x11:
		return Event{Type: EventKey, Key: KeyCtrlQ}
	case 0x12:
		return Event{Type: EventKey, Key: KeyCtrlR}
	case 0x13:
		return Event{Type: EventKey, Key: KeyCtrlS}
	case 0x14:
		return Event{Type: EventKey, Key: KeyCtrlT}
	case 0x15:
		return Event{Type: EventKey, Key: KeyCtrlU}
	case 0x16:
		return Event{Type: EventKey, Key: KeyCtrlV}
	case 0x17:
		return Event{Type: EventKey, Key: KeyCtrlW}
	case 0x18:
		return Event{Type: EventKey, Key: KeyCtrlX}
	case 0x19:
		return Event{Type: EventKey, Key: KeyCtrlY}
	case 0x1a:
		return Event{Type: EventKey, Key: KeyCtrlZ}
	case 0x1b: // ESC (shouldn't reach here normally)
		return Event{Type: EventKey, Key: KeyEscape}
	case 0x1c:
		return Event{Type: EventKey, Key: KeyCtrlBackslash}
	case 0x1d:
		return Event{Type: EventKey, Key: KeyCtrlBracketRight}
	case 0x1e:
		return Event{Type: EventKey, Key: KeyCtrlCaret}
	case 0x1f:
		return Event{Type: EventKey, Key: KeyCtrlUnderscore}
	}
	return Event{Type: EventKey, Key: KeyNone}
}

// readMoreWithTimeout attempts to read more data with a short timeout
func (r *inputReader) readMoreWithTimeout() []byte {
	ready, err := r.pollRead(int(escapeTimeout / time.Millisecond))
	if err != nil || !ready {
		return nil
	}

	// Optimization: Reuse a small buffer on stack or pool if possible
	// For escape sequences, 16 bytes is usually enough
	// Since we return the slice, it must be allocated, but we can avoid large buffers
	buf := make([]byte, 16)
	n, err := unix.Read(r.fd, buf)
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}

// sendEvent sends an event to the channel, non-blocking
func (r *inputReader) sendEvent(ev Event) {
	select {
	case r.eventCh <- ev:
	default:
		// Channel full, drop event (shouldn't happen with 64 buffer)
	}
}

// decodeRune decodes the first UTF-8 rune from data
func decodeRune(data []byte) (rune, int) {
	if len(data) == 0 {
		return 0, 0
	}

	b := data[0]
	if b < 0x80 {
		return rune(b), 1
	}

	var size int
	var min rune
	var r rune

	switch {
	case b&0xe0 == 0xc0:
		size = 2
		min = 0x80
		r = rune(b & 0x1f)
	case b&0xf0 == 0xe0:
		size = 3
		min = 0x800
		r = rune(b & 0x0f)
	case b&0xf8 == 0xf0:
		size = 4
		min = 0x10000
		r = rune(b & 0x07)
	default:
		return 0xFFFD, 1 // Invalid, return replacement char
	}

	if len(data) < size {
		return 0xFFFD, 1
	}

	for i := 1; i < size; i++ {
		if data[i]&0xc0 != 0x80 {
			return 0xFFFD, 1
		}
		r = r<<6 | rune(data[i]&0x3f)
	}

	if r < min {
		return 0xFFFD, 1 // Overlong encoding
	}

	return r, size
}