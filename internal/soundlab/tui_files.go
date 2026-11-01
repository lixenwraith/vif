//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

// The files overlay (^O): a directory's entries, newest first, to load or merge
// a document, rip a recording, or save the tab's document into it under a name.
// Each action is a command line through Execute, as every key's is, a path with
// a space quoted.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/toml"
)

// fileRow is one entry of the files overlay
type fileRow struct {
	name  string
	dir   bool
	up    bool
	size  int64
	mtime time.Time
}

// fileBrowser is the overlay's state: the directory shown, its rows and the cursor
type fileBrowser struct {
	open   bool
	dir    string
	rows   []fileRow
	cur    int
	scroll int
	pageH  int
	err    string
}

// load reads dir: the parent first, then directories, then files, each group
// most recently modified first, the file just written being the one wanted;
// hidden files stay hidden, hidden directories show
func (b *fileBrowser) load(dir string) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	b.dir, b.err, b.rows, b.cur, b.scroll = dir, "", b.rows[:0], 0, 0
	des, err := os.ReadDir(dir)
	if err != nil {
		b.err = err.Error()
	}
	if parent := filepath.Dir(dir); parent != dir {
		b.rows = append(b.rows, fileRow{name: "..", dir: true, up: true})
	}
	rows := make([]fileRow, 0, len(des))
	for _, de := range des {
		fi, err := os.Stat(filepath.Join(dir, de.Name())) // follows a link, so a linked directory is entered
		if err != nil {
			continue
		}
		if !fi.IsDir() && strings.HasPrefix(de.Name(), ".") {
			continue
		}
		rows = append(rows, fileRow{name: de.Name(), dir: fi.IsDir(), size: fi.Size(), mtime: fi.ModTime()})
	}
	slices.SortFunc(rows, func(x, y fileRow) int {
		if x.dir != y.dir {
			if x.dir {
				return -1
			}
			return 1
		}
		return y.mtime.Compare(x.mtime)
	})
	b.rows = append(b.rows, rows...)
}

func (b *fileBrowser) selected() (fileRow, bool) {
	if b.cur < 0 || b.cur >= len(b.rows) {
		return fileRow{}, false
	}
	return b.rows[b.cur], true
}

// openFiles shows the overlay on the directory it last showed, else the pattern
// document's, else the working directory
func (a *tuiApp) openFiles() {
	dir := a.files.dir
	if dir == "" {
		dir = "."
		if src := a.s.pats.src; src != "" {
			dir = filepath.Dir(src)
		}
	}
	a.files.load(dir)
	a.files.open = true
}

// filesKey moves through the directory, opens what the cursor is on, merges a
// document, saves the tab's document here, goes up, or closes
func (a *tuiApp) filesKey(ev terminal.Event) {
	b := &a.files
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	switch {
	case ev.Key == terminal.KeyDown || r == 'j':
		b.cur++
	case ev.Key == terminal.KeyUp || r == 'k':
		b.cur--
	case ev.Key == terminal.KeyPageDown:
		b.cur += max(b.pageH-1, 1)
	case ev.Key == terminal.KeyPageUp:
		b.cur -= max(b.pageH-1, 1)
	case r == 'g' || ev.Key == terminal.KeyHome:
		b.cur = 0
	case r == 'G' || ev.Key == terminal.KeyEnd:
		b.cur = len(b.rows) - 1
	case ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyCtrlO:
		b.open = false
	case ev.Key == terminal.KeyLeft || r == 'h' || ev.Key == terminal.KeyBackspace || ev.Key == terminal.KeyCtrlH:
		b.load(filepath.Dir(b.dir))
	case ev.Key == terminal.KeyEnter || ev.Key == terminal.KeyRight || r == 'l':
		a.filesEnter(false)
	case r == 'm':
		a.filesEnter(true)
	case r == 's':
		a.promptSaveHere()
	}
	b.cur = min(max(b.cur, 0), max(len(b.rows)-1, 0))
}

// filesEnter descends into a directory, loads or merges a document by the tables
// it holds, or rips a recording under a name a prompt asks for
func (a *tuiApp) filesEnter(merge bool) {
	row, ok := a.files.selected()
	if !ok {
		return
	}
	if row.up {
		a.files.load(filepath.Dir(a.files.dir))
		return
	}
	path := filepath.Join(a.files.dir, row.name)
	if row.dir {
		a.files.load(path)
		return
	}
	switch strings.ToLower(filepath.Ext(row.name)) {
	case ".wav":
		if merge {
			fmt.Fprintln(a.log, "a recording is ripped, not merged — enter rips it")
			return
		}
		a.promptRip(path)
	case ".toml":
		kind, err := sniffDocument(path)
		if err != nil {
			fmt.Fprintf(a.log, "%s: %v\n", row.name, err)
			return
		}
		verb := "load"
		if merge {
			verb = "merge"
		}
		if a.exec(verb+" "+kind+" "+quotePath(path)) != nil {
			return
		}
		a.files.open = false
		a.kind = kindPattern
		if kind == "sound" {
			a.kind = kindSound
		}
		a.brCursor[a.kind] = 0
	default:
		fmt.Fprintf(a.log, "%s: not a .toml document or a .wav recording\n", row.name)
	}
}

// promptRip asks what to rip a recording as, the file's name made a pattern
// name, and lands the browser on the melody it heard
func (a *tuiApp) promptRip(path string) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '_'
	}, base)
	name = strings.Trim(name, "_")
	if name == "" {
		name = "rip"
	}
	a.openPrompt("rip "+filepath.Base(path)+" as: ", name, func(name string) {
		if a.exec("rip "+quotePath(path)+" "+name) != nil {
			return
		}
		a.files.open = false
		a.kind = kindPattern
		if !a.cursorTo(name + "_melody_1") {
			a.cursorTo(name + "_beat_1")
		}
	})
}

// promptSaveHere asks for the name the tab's document, patterns or sounds, is
// saved under in the directory shown, the document's own name offered
func (a *tuiApp) promptSaveHere() {
	kind, src := "pattern", a.s.pats.src
	if a.kind != kindPattern {
		kind, src = "sound", a.s.sounds.src
	}
	name := kind + "s.toml"
	if src != "" {
		name = filepath.Base(src)
	}
	dir := a.files.dir
	a.openPrompt("save "+kind+" -> "+filepath.Base(dir)+string(filepath.Separator), name, func(name string) {
		if a.exec("save "+kind+" "+quotePath(filepath.Join(dir, name))) == nil {
			a.files.load(dir)
		}
	})
}

// quotePath puts a path in quotes when a space would split it
func quotePath(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// sniffDocument says which document a TOML file is by its tables: patterns and
// songs make a pattern document, sounds, voices and includes a sound document
func sniffDocument(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var head struct {
		Pattern []map[string]any `toml:"pattern"`
		Song    []map[string]any `toml:"song"`
		Sound   []map[string]any `toml:"sound"`
		Voice   []map[string]any `toml:"voice"`
		Include []string         `toml:"include"`
	}
	if err := toml.Unmarshal(data, &head); err != nil {
		return "", err
	}
	sounds, pats := len(head.Sound)+len(head.Voice)+len(head.Include), len(head.Pattern)+len(head.Song)
	switch {
	case sounds > 0 && pats > 0:
		return "", errors.New("holds sounds and patterns both")
	case sounds > 0:
		return "sound", nil
	case pats > 0:
		return "pattern", nil
	}
	return "", errors.New("no [[pattern]], [[song]], [[sound]] or [[voice]] table")
}

// renderFiles lays the directory over the panes: a row an entry, directories
// first, the size and the time beside a file, the selection's name or the read
// error on the last row with what s would save
func (a *tuiApp) renderFiles(root tui.Region) {
	b := &a.files
	th := &a.theme
	opts := tui.DefaultOverlayOpts("files " + tui.TruncateLeft(b.dir, max(12, root.W/2)) + " — enter open  m merge  s save here  esc close")
	opts.Bg, opts.Fg, opts.TitleBg, opts.TitleFg = th.Bg, th.Selected, th.HeaderBg, th.HeaderFg
	opts.Width, opts.Height = min(root.W*8/10, 100), min(root.H*8/10, 40)
	c := root.Overlay(opts).Content
	if c.W < 24 || c.H < 2 {
		return
	}
	b.pageH = c.H - 1
	b.cur = min(max(b.cur, 0), max(len(b.rows)-1, 0))
	if b.cur < b.scroll {
		b.scroll = b.cur
	}
	if b.cur >= b.scroll+b.pageH {
		b.scroll = b.cur - b.pageH + 1
	}
	const sizeW, timeW = 7, 16
	nameW := max(8, c.W-sizeW-timeW-2)
	for y := 0; y < b.pageH; y++ {
		i := b.scroll + y
		if i >= len(b.rows) {
			break
		}
		row := b.rows[i]
		bg := th.Bg
		if i == b.cur {
			bg = th.CursorBg
		}
		for x := 0; x < c.W; x++ {
			c.Cell(x, y, ' ', th.Fg, bg, terminal.AttrNone)
		}
		name, fg, attr := row.name, th.Fg, terminal.AttrNone
		if row.dir {
			name, fg, attr = name+"/", th.HintFg, terminal.AttrBold
		}
		c.Text(0, y, tui.Truncate(name, nameW), fg, bg, attr)
		if !row.dir {
			c.Text(nameW+1, y, tui.PadLeft(humanSize(row.size), sizeW), th.Fg, bg, terminal.AttrNone)
		}
		if !row.up {
			c.Text(nameW+sizeW+2, y, row.mtime.Format("2006-01-02 15:04"), th.Unselected, bg, terminal.AttrDim)
		}
	}
	status := b.err
	if status == "" {
		status = b.dir
		if row, ok := b.selected(); ok {
			status = row.name
		}
	}
	kind := "pattern"
	if a.kind != kindPattern {
		kind = "sound"
	}
	c.Text(0, c.H-1, tui.Truncate(status, c.W), th.HintFg, th.Bg, terminal.AttrNone)
	tail := "s saves the " + kind + " document here"
	if x := c.W - tui.RuneLen(tail); x > tui.RuneLen(status)+2 {
		c.Text(x, c.H-1, tail, th.Unselected, th.Bg, terminal.AttrNone)
	}
}

// humanSize is a byte count in at most six characters
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v < 10 {
		return fmt.Sprintf("%.1f%c", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.0f%c", v, "KMGTPE"[exp])
}
