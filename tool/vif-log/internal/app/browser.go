package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/ui"
)

// fileRow is one entry in the open panel.
type fileRow struct {
	name   string
	dir    bool
	up     bool
	marked bool
	size   int64
	mtime  time.Time
}

// browser is the directory navigator behind the open overlay.
type browser struct {
	dir   string
	rows  []fileRow
	panel ui.Panel
	err   string
}

// load reads dir: parent first, then directories, then files, each group most
// recently modified first — the log you just produced is the one you want.
func (b *browser) load(dir string) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	b.dir, b.err, b.rows = dir, "", b.rows[:0]

	des, err := os.ReadDir(dir)
	if err != nil {
		b.err = err.Error()
	}
	if parent := filepath.Dir(dir); parent != dir {
		b.rows = append(b.rows, fileRow{name: "..", dir: true, up: true})
	}

	rows := make([]fileRow, 0, len(des))
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		rows = append(rows, fileRow{
			name: de.Name(), dir: de.IsDir(), size: fi.Size(), mtime: fi.ModTime(),
		})
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
	b.panel.Reset()
}

func (b *browser) selected() (fileRow, bool) {
	if b.panel.Sel < 0 || b.panel.Sel >= len(b.rows) {
		return fileRow{}, false
	}
	return b.rows[b.panel.Sel], true
}

// toggleMark selects a file for multi-open; directories are not markable.
func (b *browser) toggleMark() {
	if b.panel.Sel < 0 || b.panel.Sel >= len(b.rows) || b.rows[b.panel.Sel].dir {
		return
	}
	b.rows[b.panel.Sel].marked = !b.rows[b.panel.Sel].marked
	b.panel.Move(1)
}

// marked returns the absolute paths of every marked file.
func (b *browser) marked() []string {
	var out []string
	for _, r := range b.rows {
		if r.marked {
			out = append(out, filepath.Join(b.dir, r.name))
		}
	}
	return out
}

// --- app hooks -------------------------------------------------------------

// openBrowser shows the file panel, refreshing the listing.
func (a *App) openBrowser() {
	dir := a.browse.dir
	if dir == "" {
		dir = "."
	}
	a.browse.load(dir)
	a.overlay = ovOpen
}

// browserEnter descends into the selected directory, or loads the marked set
// (falling back to the cursor file when nothing is marked).
func (a *App) browserEnter() {
	if paths := a.browse.marked(); len(paths) > 0 {
		if err := a.openFiles(paths); err != nil {
			a.say(tui.ToastError, err.Error())
			return
		}
		a.overlay = ovNone
		return
	}
	row, ok := a.browse.selected()
	if !ok {
		return
	}
	path := filepath.Join(a.browse.dir, row.name)
	if row.dir {
		a.browse.load(path)
		return
	}
	if err := a.openFiles([]string{path}); err != nil {
		a.say(tui.ToastError, err.Error())
		return
	}
	a.overlay = ovNone
}

func (a *App) browserUp() { a.browse.load(filepath.Dir(a.browse.dir)) }

// --- render ----------------------------------------------------------------

func (a *App) renderBrowser(root tui.Region) {
	b := &a.browse
	b.panel.Rows = len(b.rows)
	b.panel.Title = "open - " + tui.TruncateLeft(b.dir, max(12, root.W/3))
	b.panel.Hint = "spc mark   enter open   esc close"
	b.panel.Status = b.status()

	body := b.panel.Render(root, &a.th)
	if body.W < 24 || body.H < 1 {
		return
	}

	const sizeW, timeW = 7, 16
	nameW := max(8, body.W-sizeW-timeW-2)

	for y := 0; y < body.H; y++ {
		i := b.panel.Scroll + y
		if i >= len(b.rows) {
			break
		}
		row := b.rows[i]

		bg := a.th.FocusBg
		if i == b.panel.Sel {
			bg = a.th.CursorBg
		}
		for x := 0; x < body.W; x++ {
			body.Cell(x, y, ' ', a.th.Fg, bg, terminal.AttrNone)
		}

		name, fg, attr := row.name, a.th.Fg, terminal.AttrNone
		if row.dir {
			name, fg, attr = name+"/", a.th.Accent, terminal.AttrBold
		}
		if row.marked {
			name, fg, attr = "◆ "+name, a.th.Selected, terminal.AttrBold
		}
		body.Text(0, y, tui.Truncate(name, nameW), fg, bg, attr)
		if !row.dir {
			body.Text(nameW+1, y, tui.PadLeft(humanSize(row.size), sizeW),
				a.th.NumFg, bg, terminal.AttrNone)
		}
		if !row.up {
			body.Text(nameW+sizeW+2, y, row.mtime.Format("2006-01-02 15:04"),
				a.th.HintFg, bg, terminal.AttrDim)
		}
	}
}

// status is the untruncated name of the selection, or the read error.
func (b *browser) status() string {
	if b.err != "" {
		return b.err
	}
	if row, ok := b.selected(); ok {
		return row.name
	}
	return b.dir
}

// humanSize renders a byte count in at most six characters.
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
