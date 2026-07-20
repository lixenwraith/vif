package audio

import (
	"errors"
	"strings"
	"sync"
)

// BackendType identifies the audio backend
type BackendType int

const (
	BackendPulse BackendType = iota
	BackendPipeWire
	BackendALSA
	BackendSoX
	BackendFFplay
	BackendOSS
)

// BackendConfig describes a CLI audio backend
type BackendConfig struct {
	Type BackendType
	Name string
	Path string
	Args []string
}

// Sentinel errors
var (
	ErrNoAudioBackend = errors.New("no compatible audio backend found")
	ErrPipeClosed     = errors.New("audio pipe closed")
)

const stderrTailMax = 2048

// tailBuffer is a bounded sink for backend stderr
// Writer: backend process; readers: probe diagnostics and telemetry
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailMax {
		t.buf = t.buf[len(t.buf)-stderrTailMax:]
	}
	t.mu.Unlock()
	return len(p), nil
}

// LastLine returns the final non-empty stderr line for compact diagnostics
func (t *tailBuffer) LastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimRight(string(t.buf), "\n\r ")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

