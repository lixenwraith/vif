//go:build (linux || freebsd || windows) && !vif_headless && !vif_noaudio

package main

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/vlog"
)

// speakerOrder is the order a presenting run tries the build's own speakers in,
// before the engine finds a player by name: a sound server's stream ahead of the
// card the server owns. The file that has one sets its open in speakerOpens.
var (
	speakerOrder = [...]string{"pulse", "alsa", "wasapi"}
	speakerOpens = map[string]func(forced bool) (io.WriteCloser, error){}
)

// The speakers that open outlive the run's scenario changes, as the window does,
// and close as the process ends
func init() {
	var own io.WriteCloser
	speakersOn = func() {
		if platformHost.Audio == nil && own == nil {
			if own = openSpeakers(); own != nil {
				platformHost.Audio = own
			}
		}
	}
	speakersOff = func() {
		if own != nil {
			own.Close()
		}
	}
}

// openSpeakers opens the first of the build's speakers that plays here, or the one
// -ab names alone. Nil, where none plays or -ab names a player, leaves the engine
// to find one by name.
func openSpeakers() io.WriteCloser {
	var why []string
	for _, name := range speakerOrder {
		open := speakerOpens[name]
		if open == nil || flagAudioBackend != "" && flagAudioBackend != name {
			continue
		}
		s, err := open(flagAudioBackend == name)
		if err == nil {
			vlog.Info("app", "msg", "speakers open", "speakers", name)
			return s
		}
		why = append(why, name+": "+err.Error())
	}
	if why != nil {
		vlog.Info("app", "msg", "no speakers of the build's own", "why", strings.Join(why, "; "))
	}
	return nil
}

// reopening plays through what open opens and keeps it playing across its loss: a
// failed write closes it and drops the writes that follow, and open is tried again
// at most once a second, so the mixer, which a write error silences for the run,
// sees none. The first open's error is the speakers'.
func reopening(name string, open func() (io.WriteCloser, error)) (io.WriteCloser, error) {
	out, err := open()
	if err != nil {
		return nil, err
	}
	return &relay{name: name, open: open, out: out}, nil
}

// relay is reopening's writer. Its lock is held through a write, so Close frees
// nothing a write is inside: an output still inside one is left to the exit.
type relay struct {
	mu     sync.Mutex
	name   string
	open   func() (io.WriteCloser, error)
	out    io.WriteCloser // nil while lost
	lost   time.Time      // the last loss or failed open
	closed bool
}

func (r *relay) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if r.out == nil && time.Since(r.lost) >= time.Second {
		var err error
		if r.out, err = r.open(); err != nil {
			r.lost = time.Now()
		} else {
			vlog.Info("app", "msg", "speakers back", "speakers", r.name)
		}
	}
	if r.out == nil {
		return len(b), nil
	}
	if _, err := r.out.Write(b); err != nil {
		r.out.Close()
		r.out, r.lost = nil, time.Now()
		vlog.Warn("app", "msg", "speakers lost", "speakers", r.name, "error", err.Error())
	}
	return len(b), nil
}

func (r *relay) Close() error {
	if !r.mu.TryLock() {
		return nil
	}
	defer r.mu.Unlock()
	r.closed = true
	if r.out == nil {
		return nil
	}
	return r.out.Close()
}
