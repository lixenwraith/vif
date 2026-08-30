package logfile

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	scanBufSize = 1 << 20
	publishEach = 4096
)

// scanPart is one source's private scan output, merged after completion.
type scanPart struct {
	metas []Meta
	snaps []Snapshot
}

// Open indexes one or more files and starts background scanning. A single
// source grows the published view incrementally; several sources publish once,
// merged by timestamp, so a row index is stable for the life of the index.
func Open(paths ...string) (*Index, error) {
	if len(paths) == 0 {
		return nil, os.ErrInvalid
	}
	x := &Index{subN: newInterner(), msgN: newInterner()}

	files := make([]*os.File, 0, len(paths))
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			closeAll(files)
			return nil, err
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			closeAll(files)
			return nil, err
		}
		abs := p
		if a, err := filepath.Abs(p); err == nil {
			abs = a
		}
		x.srcs = append(x.srcs, &Source{Path: abs, Name: filepath.Base(p), Size: fi.Size()})
		files = append(files, f)
	}

	x.publish(nil, nil, nil, nil)
	go x.scanAll(files)
	return x, nil
}

func closeAll(fs []*os.File) {
	for _, f := range fs {
		f.Close()
	}
}

// publish stores immutable slice headers; a later append reallocates rather
// than mutating what readers already hold.
func (x *Index) publish(metas []Meta, snaps []Snapshot, subs, msgs []string) {
	x.metas.Store(&metas)
	x.snaps.Store(&snaps)
	x.subs.Store(&subs)
	x.msgs.Store(&msgs)
}

func (x *Index) scanAll(files []*os.File) {
	live := len(x.srcs) == 1
	parts := make([]scanPart, len(x.srcs))

	var wg sync.WaitGroup
	for i := range x.srcs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			x.scanSource(uint16(i), files[i], &parts[i], live)
		}(i)
	}
	wg.Wait()

	if live {
		x.publish(parts[0].metas, parts[0].snaps, x.subN.table(), x.msgN.table())
		return
	}
	metas, snaps := mergeParts(parts)
	x.publish(metas, snaps, x.subN.table(), x.msgN.table())
}

// scanSource indexes one file. In live mode it republishes as it goes.
func (x *Index) scanSource(src uint16, f *os.File, part *scanPart, live bool) {
	s := x.srcs[src]
	defer f.Close()
	defer s.done.Store(true)

	br := bufio.NewReaderSize(f, scanBufSize)

	var (
		line     []byte
		off      int64
		lastPub  int
		lastTS   int64
		curID    uint32
		curR     uint32
		curT     uint32
		haveSnap bool

		// Journal bookkeeping: jseq is dense by construction, so any step other
		// than +1 is a record the writer dropped.
		lastSeq  uint64
		haveSeq  bool
		domCount int64
		gapCount int64
	)

	// Estimated row count avoids repeated regrowth on multi-MB files.
	if s.Size > 0 {
		part.metas = make([]Meta, 0, int(s.Size/160)+64)
	}

	// closedSnaps excludes the still-growing last group: its Count is mutated
	// in place, and readers must never observe a header containing it.
	closedSnaps := func() []Snapshot {
		if len(part.snaps) == 0 {
			return nil
		}
		return part.snaps[:len(part.snaps)-1]
	}

	for {
		var err error
		line, err = readLine(br, line)
		raw := trimEOL(line)

		if len(raw) > 0 {
			m, jseq := parseMeta(raw, off, src, x.subN, x.msgN)
			if m.Flags&FlagMalformed != 0 {
				s.bad.Add(1)
			}
			if m.Dom != DomNone {
				domCount++
				if domCount == 1 {
					s.dom.Store(1) // HasDomains only asks whether there is one
				}
			}
			if jseq != 0 {
				if haveSeq && jseq != lastSeq+1 {
					gapCount++
					s.gaps.Store(gapCount)
				}
				lastSeq, haveSeq = jseq, true
			}
			// Ordering key: a line without a usable stamp inherits the previous
			// one and is rendered as unstamped.
			if m.TS == 0 {
				m.TS, m.Flags = lastTS, m.Flags|FlagNoTime
			} else {
				lastTS = m.TS
			}
			idx := uint32(len(part.metas))

			// A stat record whose (run,tick) differs from the previous stat
			// record opens a new group. Frame is excluded: it is stamped by the
			// render goroutine and can change mid-snapshot.
			if x.subN.table()[m.Sub] == SubStat {
				if !haveSnap || curR != m.Run || curT != m.Tick {
					part.snaps = append(part.snaps, Snapshot{
						Head: idx, Run: m.Run, Tick: m.Tick, Frame: m.Frame, Src: src,
					})
					curID = uint32(len(part.snaps))
					curR, curT, haveSnap = m.Run, m.Tick, true
					m.Flags |= FlagSnapHead
				}
				part.snaps[curID-1].Count++
				m.Snap = curID
			}
			part.metas = append(part.metas, m)
		}

		off += int64(len(line))
		s.scanned.Store(off)

		if live && len(part.metas)-lastPub >= publishEach {
			x.publish(part.metas, closedSnaps(), x.subN.table(), x.msgN.table())
			lastPub = len(part.metas)
		}
		if err != nil {
			if err != io.EOF {
				s.failure.Store(&scanErr{err})
			}
			break
		}
	}
	s.scanned.Store(off)
}

// mergeParts interleaves per-source rows by timestamp and renumbers snapshot
// ids into one global space. Sources are individually monotonic, so one linear
// k-way pass suffices.
func mergeParts(parts []scanPart) ([]Meta, []Snapshot) {
	total := 0
	base := make([]uint32, len(parts))
	var snaps []Snapshot
	for i := range parts {
		total += len(parts[i].metas)
		base[i] = uint32(len(snaps))
		snaps = append(snaps, parts[i].snaps...)
	}

	out := make([]Meta, 0, total)
	cur := make([]int, len(parts))
	for {
		best := -1
		for i := range parts {
			if cur[i] >= len(parts[i].metas) {
				continue
			}
			if best < 0 || parts[i].metas[cur[i]].TS < parts[best].metas[cur[best]].TS {
				best = i
			}
		}
		if best < 0 {
			break
		}
		m := parts[best].metas[cur[best]]
		cur[best]++
		if m.Snap != 0 {
			m.Snap += base[best]
			if m.Flags&FlagSnapHead != 0 {
				snaps[m.Snap-1].Head = uint32(len(out))
			}
		}
		out = append(out, m)
	}
	return out, snaps
}

// readLine appends the next line, terminator included, into buf.
func readLine(br *bufio.Reader, buf []byte) ([]byte, error) {
	buf = buf[:0]
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return buf, err
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// parseMeta extracts the indexed fields from one line. Unparseable lines are
// flagged and kept, never dropped. jseq is the journal record counter, 0 for
// every line that is not a journal record.
func parseMeta(line []byte, off int64, src uint16, subN, msgN *interner) (m Meta, jseq uint64) {
	m = Meta{Off: off, Len: uint32(len(line)), Src: src, Lvl: LevelBad, Flags: FlagMalformed}
	i := skipSpace(line, 0)
	if i >= len(line) || line[i] != '{' {
		return m, 0
	}

	// sub and fields are resolved after the pass: the discriminator a record
	// without one falls back to depends on its sub, and key order is the
	// writer's business, not ours.
	var sub, fields []byte

	ok := eachField(line, i, func(k, v []byte, kind byte) bool {
		switch string(k) {
		case "time":
			if kind == KStr {
				if ns, good := parseRFC3339Nano(strTok(v)); good {
					m.TS = ns
				}
			}
		case "level":
			if kind == KStr {
				m.Lvl = ParseLevel(strTok(v))
			}
		case "sub":
			if kind == KStr {
				sub = strTok(v)
			}
		case "run":
			m.Run = parseUint32(v)
		case "tick":
			m.Tick = parseUint32(v)
		case "frame":
			m.Frame = parseUint32(v)
		case "trace":
			if kind == KStr && len(v) > 2 {
				m.Flags |= FlagTrace
			}
		case "fields":
			if kind == KObj {
				fields = v
			}
		}
		return true
	})

	if id := subN.intern(sub); id <= 0xffff {
		m.Sub = uint16(id)
	}
	if string(sub) == SubAnchor {
		m.Flags |= FlagAnchor
	}

	// Only the journal carries a domain and a jseq, so ordinary records pay for
	// the discriminator alone and stop at the first field.
	jrn := string(sub) == SubJournal
	msg, dom, seq := scanFields(fields, jrn || m.Flags&FlagAnchor != 0)
	m.Dom = dom
	if msg == nil {
		msg = syntheticMsgTok(sub)
	}
	m.Msg = msgN.intern(msg)
	if jrn {
		jseq = seq
	}

	if ok {
		m.Flags &^= FlagMalformed
	}
	return m, jseq
}

// scanFields makes one pass over the fields object for everything the index row
// needs. deep also collects the journal's domain and jseq; without it the pass
// stops at the first discriminator, which the writer always emits first.
// A nil discriminator means the record has none: the caller falls back on the sub.
func scanFields(fields []byte, deep bool) (msg []byte, dom Domain, jseq uint64) {
	if fields == nil {
		return nil, DomNone, 0
	}
	best := len(discriminatorKeys) // rank of the discriminator found so far
	eachField(fields, 0, func(k, v []byte, kind byte) bool {
		if kind == KStr {
			for rank, name := range discriminatorKeys {
				if rank >= best {
					break
				}
				if string(k) == name {
					msg, best = strTok(v), rank
					break
				}
			}
		}
		if !deep {
			return best > 0
		}
		switch {
		case kind == KStr && string(k) == "domain":
			dom = ParseDomain(strTok(v))
		case kind == KNum && string(k) == "jseq":
			jseq = parseUint64(v)
		}
		return true
	})
	return msg, dom, jseq
}
