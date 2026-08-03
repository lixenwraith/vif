package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Dev-mode runtime capture. Race reports, fatal errors and traces from
// goroutines outside Go() reach fd 2 whatever the logger is doing; inside the
// alternate screen that output is unreadable. Capture redirects fd 2 to a file
// and DrainStderr reports new blocks as pointers into it.

const (
	stderrPrefix  = "vif-stderr-"
	raceDelimiter = "=================="
	modulePath    = "github.com/lixenwraith/vi-fighter"
	maxDrainBytes = 256 << 10
	headMaxLen    = 200
)

// RuntimeReport locates one captured block; the text stays in the file
type RuntimeReport struct {
	Path   string // capture file
	Offset int64  // byte offset of the block
	Bytes  int    // block length
	Lines  int
	Kind   string // data race, fatal, panic, summary, output
	Head   string // first line
	At     string // first vi-fighter frame, empty when absent
}

var (
	capMu    sync.Mutex
	capWrite *os.File
	capRead  *os.File
	capOff   int64
	capOrig  = -1 // dup of the original fd 2, -1 when inactive
	capStop  chan struct{}
	capSeen  int
)

// CaptureStderr redirects fd 2 into dir and returns the capture path
// Call before the terminal enters the alternate screen
func CaptureStderr(dir string) (string, error) {
	capMu.Lock()
	defer capMu.Unlock()
	if capWrite != nil {
		return capWrite.Name(), nil
	}
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, stderrPrefix+time.Now().Format("060102-150405")+".log")

	w, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	r, err := os.Open(path)
	if err != nil {
		w.Close()
		return "", err
	}
	// Saved before the redirect so teardown can hand fd 2 back
	orig, err := dupFD(2)
	if err != nil {
		w.Close()
		r.Close()
		return "", err
	}
	if err := dupToStderr(int(w.Fd())); err != nil {
		w.Close()
		r.Close()
		return "", err
	}

	capWrite, capRead, capOrig, capOff, capSeen = w, r, orig, 0, 0
	return path, nil
}

// RestoreStderr points fd 2 back at its original destination
func RestoreStderr() {
	capMu.Lock()
	defer capMu.Unlock()
	if capOrig < 0 {
		return
	}
	_ = dupToStderr(capOrig)
}

// CloseCapture restores fd 2, releases the files and discards an empty capture.
// Returns the path when it holds content. Call after the terminal is restored
// so late runtime output reaches the real terminal.
func CloseCapture() string {
	capMu.Lock()
	defer capMu.Unlock()
	if capWrite == nil {
		return ""
	}
	if capOrig >= 0 {
		_ = dupToStderr(capOrig)
		capOrig = -1
	}

	path := capWrite.Name()
	var size int64
	if st, err := capWrite.Stat(); err == nil {
		size = st.Size()
	}
	capWrite.Close()
	capRead.Close()
	capWrite, capRead = nil, nil

	if size == 0 {
		_ = os.Remove(path)
		return ""
	}
	return path
}

// StderrCapturePath returns the capture file, empty when inactive
func StderrCapturePath() string {
	capMu.Lock()
	defer capMu.Unlock()
	if capWrite == nil {
		return ""
	}
	return capWrite.Name()
}

// CaptureCount returns reports drained this session
func CaptureCount() int {
	capMu.Lock()
	defer capMu.Unlock()
	return capSeen
}

// DrainStderr reports every complete block written since the last drain
// An incomplete trailing line is held back so a block is never split
func DrainStderr(emit func(RuntimeReport)) int {
	capMu.Lock()
	defer capMu.Unlock()
	if capRead == nil {
		return 0
	}
	st, err := capRead.Stat()
	if err != nil || st.Size() <= capOff {
		return 0
	}
	n := st.Size() - capOff
	if n > maxDrainBytes {
		n = maxDrainBytes
	}

	buf := make([]byte, n)
	read, _ := capRead.ReadAt(buf, capOff)
	if read <= 0 {
		return 0
	}
	data := string(buf[:read])
	cut := strings.LastIndexByte(data, '\n')
	if cut < 0 {
		return 0
	}
	data = data[:cut+1]

	path := capWrite.Name()
	off := capOff
	capOff += int64(len(data))

	count := 0
	for _, seg := range strings.SplitAfter(data, raceDelimiter) {
		segOff := off
		off += int64(len(seg))

		body := strings.TrimSuffix(seg, raceDelimiter)
		trimmed := strings.TrimLeft(body, "\r\n \t")
		segOff += int64(len(body) - len(trimmed))
		body = strings.TrimRight(trimmed, "\r\n \t")
		if body == "" {
			continue
		}
		emit(makeReport(path, segOff, body))
		count++
	}
	capSeen += count
	return count
}

// StartStderrDrain polls the capture file until StopStderrDrain
func StartStderrDrain(interval time.Duration, emit func(RuntimeReport)) {
	capMu.Lock()
	if capRead == nil || capStop != nil {
		capMu.Unlock()
		return
	}
	stop := make(chan struct{})
	capStop = stop
	capMu.Unlock()

	Go(func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				DrainStderr(emit)
			}
		}
	})
}

// StopStderrDrain halts the poller; the caller drains once more afterwards
func StopStderrDrain() {
	capMu.Lock()
	stop := capStop
	capStop = nil
	capMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// makeReport summarizes a block into a pointer record
func makeReport(path string, off int64, body string) RuntimeReport {
	head, _, _ := strings.Cut(body, "\n")
	if len(head) > headMaxLen {
		head = head[:headMaxLen]
	}
	return RuntimeReport{
		Path:   path,
		Offset: off,
		Bytes:  len(body),
		Lines:  strings.Count(body, "\n") + 1,
		Kind:   classify(head),
		Head:   head,
		At:     firstFrame(body),
	}
}

// classify labels a block by its first line
func classify(head string) string {
	switch {
	case strings.HasPrefix(head, "WARNING: DATA RACE"):
		return "data race"
	case strings.HasPrefix(head, "fatal error:"):
		return "fatal"
	case strings.HasPrefix(head, "panic:"):
		return "panic"
	case strings.HasPrefix(head, "Found ") && strings.Contains(head, "data race"):
		return "summary"
	}
	return "output"
}

// firstFrame returns the innermost vi-fighter stack frame in a block
func firstFrame(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, modulePath) {
			return t
		}
	}
	return ""
}
