package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
)

// forms are what the export writes, the extensions convert reads its form from
var forms = []string{"aimg", "aimgz", "ans"}

type field uint8

const (
	fOut field = iota
	fForm
	fWidth
	fMode
	fDepth
	fAX
	fAY
)

var fieldLabels = [...]string{"output", "form", "width", "mode", "color", "anchor x", "anchor y"}

// exportForm is the dialog w opens. It writes the images in hand as convert
// does, one to the file it names or several into the folder it names; the
// fields it shows are those that apply to its images and its form.
type exportForm struct {
	paths   []string
	batch   bool
	rasters bool // some are rasters, so width and mode apply
	focus   int  // an index into fields()
	out     *tui.TextFieldState
	width   *tui.TextFieldState
	ax, ay  *tui.TextFieldState
	form    int // into forms
	mode    int // into modes
	depth   int // into depths
	err     string
}

// exportTargets are the marked images in the browser's order, or the cursor's
func (a *app) exportTargets() []string {
	var out []string
	for _, i := range a.br.rows {
		if e := a.br.all[i]; !e.dir && a.br.marks[e.name] {
			out = append(out, a.pathOf(e))
		}
	}
	if e, ok := a.cursorEntry(); len(out) == 0 && ok && !e.dir {
		out = append(out, a.pathOf(e))
	}
	return out
}

func (a *app) openExport(paths []string) {
	if len(paths) == 0 {
		a.say(tui.ToastWarning, "no image here to write")
		return
	}
	e := &exportForm{paths: paths, batch: len(paths) > 1}
	for _, p := range paths {
		if s, _ := a.source(p); s != nil && s.dual == nil || s == nil && !isAIMG(p) {
			e.rasters = true
		}
	}
	if !e.rasters && !e.batch {
		e.form = slices.Index(forms, "ans") // an .aimg written as itself would overwrite its input
	}
	e.mode = max(0, slices.IndexFunc(modes, func(m renderMode) bool { return m.mode == a.lk.mode }))
	e.depth = max(0, slices.IndexFunc(depths, func(d depth) bool { return d.mode == a.lk.depth }))
	e.out = tui.NewTextFieldState(".")
	if !e.batch {
		base := filepath.Base(paths[0])
		e.out.SetValue(strings.TrimSuffix(base, filepath.Ext(base)) + "." + forms[e.form])
	}
	e.width = tui.NewTextFieldState("80")
	e.ax, e.ay = tui.NewTextFieldState("0"), tui.NewTextFieldState("0")
	for _, f := range []*tui.TextFieldState{e.width, e.ax, e.ay} {
		f.Accept = tui.AcceptInteger
	}
	a.ex, a.overlay = e, ovExport
}

func (e *exportForm) fields() []field {
	fs := []field{fOut, fForm}
	if e.rasters {
		fs = append(fs, fWidth, fMode)
	}
	if forms[e.form] == "ans" {
		return append(fs, fDepth)
	}
	return append(fs, fAX, fAY)
}

func (e *exportForm) text(f field) *tui.TextFieldState {
	return map[field]*tui.TextFieldState{fOut: e.out, fWidth: e.width, fAX: e.ax, fAY: e.ay}[f]
}

func (e *exportForm) paste(s string) {
	if t := e.text(e.fields()[e.focus]); t != nil {
		t.Paste(s)
		e.syncForm()
	}
}

// syncExt gives a single output the extension of the form chosen, and
// syncForm the form its extension names, so the two never disagree
func (e *exportForm) syncExt() {
	if !e.batch {
		v := e.out.Value()
		e.out.SetValue(strings.TrimSuffix(v, filepath.Ext(v)) + "." + forms[e.form])
	}
}

func (e *exportForm) syncForm() {
	if i := slices.Index(forms, strings.TrimPrefix(strings.ToLower(filepath.Ext(e.out.Value())), ".")); !e.batch && i >= 0 {
		e.form = i
	}
}

func (e *exportForm) handleKey(a *app, ev terminal.Event) {
	fs := e.fields()
	e.focus = min(e.focus, len(fs)-1)
	ring := tui.Focus{Index: e.focus, Len: len(fs)}
	switch {
	case ev.Key == terminal.KeyEscape:
		a.overlay, a.ex = ovNone, nil
		return
	case ev.Key == terminal.KeyEnter:
		e.submit(a)
		return
	case ring.HandleKey(ev.Key, ev.Modifiers):
		e.focus = ring.Index
		return
	case ev.Key == terminal.KeyUp:
		e.focus = (e.focus + len(fs) - 1) % len(fs)
		return
	case ev.Key == terminal.KeyDown:
		e.focus = (e.focus + 1) % len(fs)
		return
	}
	e.err = ""
	switch f := fs[e.focus]; f {
	case fForm:
		if n, ok := tui.StepChoice(ev.Key, ev.Rune, e.form, len(forms)); ok {
			e.form = n
			e.syncExt()
		}
	case fMode:
		e.mode, _ = tui.StepChoice(ev.Key, ev.Rune, e.mode, len(modes))
	case fDepth:
		e.depth, _ = tui.StepChoice(ev.Key, ev.Rune, e.depth, len(depths))
	default:
		if e.text(f).HandleKey(ev.Key, ev.Rune, ev.Modifiers) && f == fOut {
			e.syncForm()
		}
	}
}

// fail shows an error at a field and moves the focus there
func (e *exportForm) fail(f field, msg string) {
	e.err = fieldLabels[f] + ": " + msg
	e.focus = max(0, slices.Index(e.fields(), f))
}

func (e *exportForm) submit(a *app) {
	o := &opts{aspect: a.lk.aspect, set: map[string]bool{}}
	if e.rasters {
		w, err := strconv.Atoi(e.width.Value())
		if err != nil || w < 1 || w > ascimage.MaxSide {
			e.fail(fWidth, fmt.Sprintf("1 to %d cells", ascimage.MaxSide))
			return
		}
		o.width = w
	}
	form := forms[e.form]
	if form != "ans" {
		var err error
		if o.ax, err = strconv.Atoi(e.ax.Value()); err != nil {
			e.fail(fAX, "a whole number")
			return
		}
		if o.ay, err = strconv.Atoi(e.ay.Value()); err != nil {
			e.fail(fAY, "a whole number")
			return
		}
		o.set["ax"], o.set["ay"] = true, true
	}
	out := strings.TrimSpace(e.out.Value())
	if out == "" {
		e.fail(fOut, "a name to write")
		return
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(a.br.dir, out)
	}
	outs := []string{out}
	if e.batch {
		if fi, err := os.Stat(out); err != nil || !fi.IsDir() {
			e.fail(fOut, "a folder to write the images into")
			return
		}
		outs = outs[:0]
		for _, p := range e.paths {
			base := filepath.Base(p)
			outs = append(outs, filepath.Join(out, strings.TrimSuffix(base, filepath.Ext(base))+"."+form))
		}
	} else if ext := filepath.Ext(out); listed(out) && !isAIMG(out) {
		e.fail(fOut, "an .aimg, an .aimgz or ANSI text, not "+strings.ToUpper(ext[1:]))
		return
	}
	mode, depth := modes[e.mode].mode, depths[e.depth].mode
	write := func() { e.write(a, o, outs, mode, depth) }
	existing := 0
	for _, p := range outs {
		if statOf(p) != nil {
			existing++
		}
	}
	if existing == 0 {
		write()
		return
	}
	a.confirmMsg = fmt.Sprintf("%s exists. Overwrite it?", filepath.Base(outs[0]))
	if len(outs) > 1 {
		a.confirmMsg = fmt.Sprintf("%d of the %d files exist. Overwrite them?", existing, len(outs))
	}
	a.confirm, a.onConfirm, a.overlay = tui.NewConfirmState(false), write, ovConfirm
}

func (e *exportForm) write(a *app, o *opts, outs []string, mode ascimage.RenderMode, depth terminal.ColorMode) {
	var done []string
	var firstErr error
	for i, p := range e.paths {
		src, err := a.source(p)
		if src == nil && err == nil {
			src, err = load(p)
		}
		var what string
		if err == nil {
			what, err = convertSource(o, src, outs[i], mode, depth)
		}
		if err != nil {
			firstErr = firstOf(firstErr, err)
			continue
		}
		a.ld.forget(outs[i])
		done = append(done, fmt.Sprintf("%s: %s", filepath.Base(outs[i]), what))
	}
	a.overlay, a.ex = ovNone, nil
	a.reload()
	switch {
	case firstErr != nil && len(done) == 0:
		a.say(tui.ToastError, firstErr.Error())
	case firstErr != nil:
		a.say(tui.ToastWarning, fmt.Sprintf("wrote %d, %d failed: %v", len(done), len(e.paths)-len(done), firstErr))
	case len(done) == 1:
		a.say(tui.ToastSuccess, "wrote "+done[0])
	default:
		dir := filepath.Dir(outs[0])
		if rel, err := filepath.Rel(a.br.dir, dir); err == nil && !strings.HasPrefix(rel, "..") {
			dir = rel
		}
		a.say(tui.ToastSuccess, fmt.Sprintf("wrote %d files into %s", len(done), dir))
	}
}

// firstOf keeps the first of two errors
func firstOf(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

func (e *exportForm) render(a *app, root tui.Region) {
	fs := e.fields()
	title := "write " + filepath.Base(e.paths[0])
	if e.batch {
		title = fmt.Sprintf("write %d images into a folder", len(e.paths))
	}
	box := tui.Center(root, min(68, root.W-2), min(len(fs)+5, root.H))
	in := box.Frame(title, a.th)
	modeNames := make([]string, len(modes))
	for i, m := range modes {
		modeNames[i] = m.name
	}
	depthNames := make([]string, len(depths))
	for i, d := range depths {
		depthNames[i] = d.name
	}
	y := 1
	for i, f := range fs {
		focused := i == e.focus
		val, rows := in.Field(y, 9, tui.Field{Label: fieldLabels[f]}, focused, a.th)
		switch f {
		case fForm:
			val.Radio(0, 0, forms, e.form, a.th)
		case fMode:
			val.Radio(0, 0, modeNames, e.mode, a.th)
		case fDepth:
			val.Radio(0, 0, depthNames, e.depth, a.th)
		default:
			hint := map[field]string{fOut: "a file, or a folder for several", fWidth: "80", fAX: "0", fAY: "0"}[f]
			val.TextInput(e.text(f), hint, focused, a.th)
		}
		y += rows
	}
	note, style := "Tab moves · ←/→ chooses · ↵ writes · Esc cancels", a.th.Muted
	if e.err != "" {
		note, style = e.err, a.th.Error
	}
	in.TextStyled(0, in.H-1, tui.Truncate(note, in.W), style)
}
