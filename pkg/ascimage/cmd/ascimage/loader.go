package main

import (
	"image"
	"time"
)

// cacheBytes bounds what the decoded images the loader keeps hold, the one in
// use always kept
const cacheBytes = 192 << 20

// loader decodes sources on a goroutine of its own, one at a time, keeping
// them in a cache bounded by their bytes. Only the app's goroutine calls its
// methods; the decoder sees nothing but the job and result channels.
type loader struct {
	jobs   chan string
	done   chan loaded
	busy   string // the path being decoded
	held   map[string]*heldSource
	failed map[string]error
	bytes  int64
	clock  uint64
}

type loaded struct {
	path string
	src  *source
	err  error
}

type heldSource struct {
	src   *source
	bytes int64
	used  uint64
}

// newLoader starts the decoder; wake tells the app a decode finished
func newLoader(wake func()) *loader {
	l := &loader{jobs: make(chan string, 1), done: make(chan loaded, 1),
		held: map[string]*heldSource{}, failed: map[string]error{}}
	go func() {
		for path := range l.jobs {
			src, err := load(path)
			l.done <- loaded{path, src, err}
			wake()
		}
	}()
	return l
}

// want takes a finished decode, then starts the first of paths neither held
// nor failed, unless a decode is running: the app names the image it shows,
// then the ones it may show next
func (l *loader) want(paths ...string) {
	select {
	case r := <-l.done:
		l.busy = ""
		if r.err != nil {
			l.failed[r.path] = r.err
		} else {
			l.put(r.path, r.src)
		}
	default:
	}
	if l.busy != "" {
		return
	}
	for _, p := range paths {
		_, held := l.held[p]
		_, failed := l.failed[p]
		if p != "" && !held && !failed {
			l.busy = p
			l.jobs <- p
			return
		}
	}
}

// get is the source decoded from path, or the error decoding it gave, or
// neither while it is pending; a source whose file has changed since is
// dropped, to be decoded again
func (l *loader) get(path string, size int64, mtime time.Time) (*source, error) {
	if h, ok := l.held[path]; ok {
		if h.src.size != size || !h.src.mtime.Equal(mtime) {
			l.forget(path)
			return nil, nil
		}
		l.clock++
		h.used = l.clock
		return h.src, nil
	}
	return nil, l.failed[path]
}

// forget drops what the loader knows of path
func (l *loader) forget(path string) {
	if h, ok := l.held[path]; ok {
		l.bytes -= h.bytes
		delete(l.held, path)
	}
	delete(l.failed, path)
}

func (l *loader) put(path string, s *source) {
	l.clock++
	h := &heldSource{src: s, bytes: s.bytes(), used: l.clock}
	l.forget(path)
	l.held[path], l.bytes = h, l.bytes+h.bytes
	for l.bytes > cacheBytes && len(l.held) > 1 {
		oldest := ""
		for p, o := range l.held {
			if p != path && (oldest == "" || o.used < l.held[oldest].used) {
				oldest = p
			}
		}
		l.forget(oldest)
	}
}

// bytes estimates what a decoded source holds
func (s *source) bytes() int64 {
	px := int64(s.w) * int64(s.h)
	if s.dual != nil {
		return px * 48 // its cells and one depth's conversion
	}
	switch s.img.(type) {
	case *image.Paletted, *image.Gray, *image.Alpha:
		return px
	case *image.YCbCr:
		return px * 2
	case *image.RGBA64, *image.NRGBA64:
		return px * 8
	}
	return px * 4
}
