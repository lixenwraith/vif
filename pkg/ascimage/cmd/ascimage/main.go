// Command ascimage turns images into terminal cells: it writes them as .aimg
// files or ANSI text, prints and describes them, and browses and views them
// full screen, converting rasters live at the size shown.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/inline"
)

// command is a row of the command table
type command struct {
	name, args, summary, long string
	setup                     func(fs *flag.FlagSet, o *opts)
	run                       func(o *opts, args []string) error
}

var commands []command

func init() {
	commands = []command{
		{
			name: "view", args: "[flags] [dir | image]", summary: "browse a directory, or view an image (the default)",
			long: `Browse a directory, the working one when none is named: its images and
folders, and a preview of the one under the cursor; Enter shows it full
screen. An image opens full screen, its directory behind it. ? lists the keys.`,
			setup: func(fs *flag.FlagSet, o *opts) {
				renderFlags(fs, o)
				fs.StringVar(&o.zoom, "z", "fit", "the zoom an image starts at: fit, or a `percentage` of 1:1")
				fs.BoolVar(&o.noStatus, "no-status", false, "start without the status bar")
			},
			run: cmdView,
		},
		{
			name: "convert", args: "[flags] <input> <output> | <input>... <dir>", summary: "write images as .aimg files or ANSI text",
			long: `Write an input to the output its extension names: .aimg holds the cells in
true color and 256 colors at once, .aimgz the same gzipped, any other name
(- for stdout) ANSI text in -c's depth. Several inputs, or a directory as the
output, land in it under their own names, in the form -to names. An .aimg
input keeps its cells: it writes to ANSI, or to .aimg with new anchors.`,
			setup: func(fs *flag.FlagSet, o *opts) {
				renderFlags(fs, o)
				fs.IntVar(&o.width, "w", 80, "output width in `cells`")
				fs.IntVar(&o.ax, "ax", 0, "anchor x an .aimg carries")
				fs.IntVar(&o.ay, "ay", 0, "anchor y an .aimg carries")
				fs.StringVar(&o.to, "to", "aimg", "the `form` written into a directory: aimg, aimgz or ans")
			},
			run: cmdConvert,
		},
		{
			name: "print", args: "[flags] <input>...", summary: "print images, fitted to the terminal",
			long: `Print each input as ANSI text, as large as fits the terminal, or -w cells
wide; out of a terminal, 80 cells wide. An .aimg prints as it is. Several
inputs print each under its name.`,
			setup: func(fs *flag.FlagSet, o *opts) {
				renderFlags(fs, o)
				fs.IntVar(&o.width, "w", 0, "width in `cells`; 0 fits the terminal")
			},
			run: cmdPrint,
		},
		{
			name: "info", args: "<input>...", summary: "describe images and .aimg files",
			long: `Describe each input: a raster's format and pixels, an .aimg's cells, render
mode and anchor, and the file's size. An .aimg is read whole, so a file info
describes is one ascimage opens.`,
			setup: func(*flag.FlagSet, *opts) {},
			run:   cmdInfo,
		},
		{
			name: "help", args: "[command]", summary: "show the usage of ascimage or a command",
			setup: func(*flag.FlagSet, *opts) {},
			run:   cmdHelp,
		},
	}
}

func find(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// opts are the flags the commands share; each command sets up those it reads
type opts struct {
	mode, depth, zoom string
	aspect            float64
	to                string
	width, ax, ay     int
	noStatus          bool
	set               map[string]bool // the flags the command line gave
}

func renderFlags(fs *flag.FlagSet, o *opts) {
	fs.StringVar(&o.mode, "m", "quadrant", "render `mode`: quadrant, or bg for a background color a cell")
	fs.StringVar(&o.depth, "c", "auto", "color `depth`: auto, true, 256 or 16")
	fs.Float64Var(&o.aspect, "aspect", ascimage.DefaultAspect, "a cell's `width` over its height on the display")
}

// modes and depths name the render modes and the color depths, for the flags,
// the status lines and the export form alike
var modes = []renderMode{{"quadrant", ascimage.ModeQuadrant}, {"bg", ascimage.ModeBackgroundOnly}}

type renderMode struct {
	name string
	mode ascimage.RenderMode
}

type depth struct {
	name string
	mode terminal.ColorMode
}

var depths = []depth{{"true", terminal.ColorModeTrueColor}, {"256", terminal.ColorMode256}, {"16", terminal.ColorMode16}}

func modeName(m ascimage.RenderMode) string {
	for _, x := range modes {
		if x.mode == m {
			return x.name
		}
	}
	return "unknown"
}

func depthName(d terminal.ColorMode) string {
	for _, x := range depths {
		if x.mode == d {
			return x.name
		}
	}
	return "none"
}

func (o *opts) renderMode() (ascimage.RenderMode, error) {
	for _, x := range modes {
		if o.mode == x.name {
			return x.mode, nil
		}
	}
	return 0, usageError(fmt.Sprintf("-m %q: want quadrant or bg", o.mode))
}

func (o *opts) colorMode() (terminal.ColorMode, error) {
	if o.depth == "auto" {
		return terminal.DetectColorMode(), nil
	}
	for _, x := range depths {
		if o.depth == x.name {
			return x.mode, nil
		}
	}
	return 0, usageError(fmt.Sprintf("-c %q: want auto, true, 256 or 16", o.depth))
}

// render is the mode, depth and aspect the render flags give
func (o *opts) render() (ascimage.RenderMode, terminal.ColorMode, error) {
	mode, err := o.renderMode()
	if err != nil {
		return 0, 0, err
	}
	depth, err := o.colorMode()
	if err != nil {
		return 0, 0, err
	}
	if !(o.aspect >= 0.1 && o.aspect <= 4) {
		return 0, 0, usageError(fmt.Sprintf("-aspect %g: want 0.1 to 4", o.aspect))
	}
	return mode, depth, nil
}

// usageError is a command called wrong: exit status 2, and a pointer to its usage
type usageError string

func (e usageError) Error() string { return string(e) }

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd, rest := find("view"), args
	if len(args) > 0 {
		switch a := args[0]; {
		case a == "-h" || a == "-help" || a == "--help":
			usage(os.Stdout)
			return 0
		case find(a) != nil:
			cmd, rest = find(a), args[1:]
		case strings.HasPrefix(a, "-") || openable(a):
			// view's flags, a directory or an image: view is the default
		default:
			fmt.Fprintf(os.Stderr, "ascimage: %q is not a command, a directory or an image; run ascimage help\n", a)
			return 2
		}
	}
	o := &opts{set: map[string]bool{}}
	fs := flagSet(cmd, o, os.Stderr)
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fs.Visit(func(f *flag.Flag) { o.set[f.Name] = true })
	if err := cmd.run(o, fs.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "ascimage %s: %v\n", cmd.name, err)
		if errors.As(err, new(usageError)) {
			fmt.Fprintf(os.Stderr, "run ascimage help %s for its usage\n", cmd.name)
			return 2
		}
		return 1
	}
	return 0
}

func flagSet(cmd *command, o *opts, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("ascimage "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(out)
	cmd.setup(fs, o)
	fs.Usage = func() { commandUsage(fs.Output(), cmd, fs) }
	return fs
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ascimage turns images into terminal cells: .aimg files and ANSI text, printed
or shown full screen.

Usage:
  ascimage [view flags] [dir | image]
  ascimage <command> [flags] [args]

Commands:
`)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, `
Images are PNG, JPEG and GIF, and .aimg, the cells ascimage writes. A directory
or an image given without a command opens in view. Flags come before paths.
Run ascimage help <command> for a command's flags.
`)
}

func commandUsage(w io.Writer, cmd *command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "Usage: ascimage %s %s\n", cmd.name, cmd.args)
	if cmd.long != "" {
		fmt.Fprintf(w, "\n%s\n", cmd.long)
	}
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	if n > 0 {
		fmt.Fprint(w, "\nFlags:\n")
		fs.PrintDefaults()
	}
}

func cmdHelp(_ *opts, args []string) error {
	switch len(args) {
	case 0:
		usage(os.Stdout)
		return nil
	case 1:
		cmd := find(args[0])
		if cmd == nil {
			return usageError(fmt.Sprintf("no command %q", args[0]))
		}
		commandUsage(os.Stdout, cmd, flagSet(cmd, &opts{}, os.Stdout))
		return nil
	}
	return usageError("help takes one command at most")
}

// fitWidth is the width at which a raster's cells fill a box of cells without
// passing it, at least 1; a box of no height bounds the width alone
func fitWidth(srcW, srcH, boxW, boxH int, aspect float64) int {
	w := max(1, boxW)
	if _, h := ascimage.CalculateOutputSize(srcW, srcH, w, aspect); boxH > 0 && h > boxH {
		w = max(1, int(float64(boxH)*float64(srcW)/(float64(srcH)*aspect)))
	}
	return min(w, ascimage.MaxSide)
}

// gridFits refuses a width whose grid passes the limits an .aimg keeps, which
// bound what a conversion may allocate too
func gridFits(srcW, srcH, width int, aspect float64) error {
	w, h := ascimage.CalculateOutputSize(srcW, srcH, width, aspect)
	if w > ascimage.MaxSide || h > ascimage.MaxSide || w*h > ascimage.MaxCells {
		return fmt.Errorf("%dx%d cells: a side is at most %d and a grid %d", w, h, ascimage.MaxSide, ascimage.MaxCells)
	}
	return nil
}

func cmdConvert(o *opts, args []string) error {
	mode, depth, err := o.render()
	if err != nil {
		return err
	}
	if len(args) < 2 {
		return usageError("want an input and an output, or inputs and a directory")
	}
	if o.width < 1 || o.width > ascimage.MaxSide {
		return usageError(fmt.Sprintf("-w %d: want 1 to %d cells", o.width, ascimage.MaxSide))
	}
	inputs, last := args[:len(args)-1], args[len(args)-1]
	outs := []string{last}
	if fi, err := os.Stat(last); len(inputs) > 1 || err == nil && fi.IsDir() {
		if err != nil || !fi.IsDir() {
			return usageError(last + ": several inputs are written into a directory")
		}
		if o.to != "aimg" && o.to != "aimgz" && o.to != "ans" {
			return usageError(fmt.Sprintf("-to %q: want aimg, aimgz or ans", o.to))
		}
		outs = outs[:0]
		for _, in := range inputs {
			base := filepath.Base(in)
			outs = append(outs, filepath.Join(last, strings.TrimSuffix(base, filepath.Ext(base))+"."+o.to))
		}
	} else if o.set["to"] {
		return usageError("-to names a directory's outputs; a file's extension names its own")
	} else if ext := filepath.Ext(last); listed(last) && !isAIMG(last) {
		return usageError(last + ": ascimage writes .aimg and .aimgz files and ANSI text, not " + strings.ToUpper(ext[1:]))
	}

	p := inline.New(os.Stderr)
	failed := 0
	for i, in := range inputs {
		if len(inputs) > 1 {
			p.Update(fmt.Sprintf("%s %d/%d %s", inline.Spinner(i), i+1, len(inputs), in))
		}
		what, err := convertOne(o, in, outs[i], mode, depth)
		if err != nil {
			if len(inputs) == 1 {
				p.Done()
				return err
			}
			p.Log("%s %v", p.Paint("failed:", inline.FgANSI(color.ANSIRed).Bold()), err)
			failed++
			continue
		}
		if outs[i] != "-" {
			p.Log("wrote %s: %s", outs[i], what)
		}
	}
	p.Done()
	if failed > 0 {
		return fmt.Errorf("%d of %d inputs failed", failed, len(inputs))
	}
	return nil
}

// convertOne writes one input to one output and says what it wrote
func convertOne(o *opts, in, out string, mode ascimage.RenderMode, depth terminal.ColorMode) (string, error) {
	src, err := load(in)
	if err != nil {
		return "", err
	}
	return convertSource(o, src, out, mode, depth)
}

// convertSource writes a loaded input to one output, as convertOne does
func convertSource(o *opts, src *source, out string, mode ascimage.RenderMode, depth terminal.ColorMode) (string, error) {
	in := src.path
	if src.dual != nil && (o.set["w"] || o.set["m"]) {
		return "", fmt.Errorf("%s: an .aimg keeps its own cells, so -w and -m do not apply", in)
	}
	if out != "-" {
		if a, b := statOf(in), statOf(out); a != nil && b != nil && os.SameFile(a, b) {
			return "", fmt.Errorf("%s: would write over its own input", out)
		}
	}
	if src.dual == nil {
		w, h := src.dims()
		if err := gridFits(w, h, o.width, o.aspect); err != nil {
			return "", fmt.Errorf("%s at -w %d: %w", in, o.width, err)
		}
	}

	if isAIMG(out) {
		d := src.dual
		if d == nil {
			d = ascimage.ConvertImageDual(src.img, o.width, mode, o.aspect)
		} else {
			kept := *d
			d = &kept
		}
		if src.dual == nil || o.set["ax"] {
			d.AnchorX = o.ax
		}
		if src.dual == nil || o.set["ay"] {
			d.AnchorY = o.ay
		}
		if err := ascimage.SaveDualMode(out, d); err != nil {
			return "", err
		}
		return fmt.Sprintf("%dx%d cells, %s, %s", d.Width, d.Height, strings.ToLower(d.RenderMode.String()), fileSize(out)), nil
	}

	var img *ascimage.ConvertedImage
	if src.dual != nil {
		img = src.dual.ToConvertedImage(depth)
	} else {
		img = ascimage.ConvertImage(src.img, o.width, mode, depth, o.aspect)
	}
	if err := writeANSI(img, out); err != nil {
		return "", err
	}
	return fmt.Sprintf("%dx%d cells of ANSI text, %s", img.Width, img.Height, fileSize(out)), nil
}

// isAIMG reports whether a name asks for an .aimg, plain or gzipped
func isAIMG(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, ascimage.Extension) || strings.EqualFold(ext, ascimage.ExtensionGzip)
}

func fileSize(path string) string {
	if fi := statOf(path); fi != nil {
		return humanSize(fi.Size())
	}
	return "size unknown"
}

func statOf(path string) os.FileInfo {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return fi
}

// writeANSI writes img as ANSI text to a file, or to stdout for "-"
func writeANSI(img *ascimage.ConvertedImage, output string) error {
	if output == "-" {
		return ascimage.WriteANSI(os.Stdout, img)
	}
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	if err := ascimage.WriteANSI(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func cmdPrint(o *opts, args []string) error {
	mode, depth, err := o.render()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return usageError("want an image to print")
	}
	if o.width < 0 || o.width > ascimage.MaxSide {
		return usageError(fmt.Sprintf("-w %d: want 0 to %d cells", o.width, ascimage.MaxSide))
	}
	p := inline.New(os.Stdout)
	boxW, boxH := 80, 0
	if p.IsTerminal() {
		boxW, boxH = p.Size()
		boxH-- // the prompt's row
		if len(args) > 1 {
			boxH-- // the name's
		}
	}
	failed := 0
	for _, in := range args {
		if err := printOne(o, p, in, len(args) > 1, mode, depth, boxW, boxH); err != nil {
			fmt.Fprintf(os.Stderr, "ascimage print: %v\n", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d inputs failed", failed, len(args))
	}
	return nil
}

func printOne(o *opts, p *inline.Printer, in string, named bool, mode ascimage.RenderMode, depth terminal.ColorMode, boxW, boxH int) error {
	src, err := load(in)
	if err != nil {
		return err
	}
	var img *ascimage.ConvertedImage
	if src.dual != nil {
		if o.set["w"] || o.set["m"] {
			return fmt.Errorf("%s: an .aimg keeps its own cells, so -w and -m do not apply", in)
		}
		img = src.dual.ToConvertedImage(depth)
	} else {
		w, h := src.dims()
		width := o.width
		if width == 0 {
			width = fitWidth(w, h, boxW, boxH, o.aspect)
		}
		if err := gridFits(w, h, width, o.aspect); err != nil {
			return fmt.Errorf("%s at -w %d: %w", in, width, err)
		}
		img = ascimage.ConvertImage(src.img, width, mode, depth, o.aspect)
	}
	if named {
		fmt.Println(p.Paint(in, inline.Style{}.Bold()))
	}
	return ascimage.WriteANSI(os.Stdout, img)
}

func cmdInfo(_ *opts, args []string) error {
	if len(args) == 0 {
		return usageError("want an image to describe")
	}
	failed := 0
	for _, in := range args {
		src, err := probe(in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ascimage info: %v\n", err)
			failed++
			continue
		}
		fmt.Printf("%s: %s\n", in, src.describe())
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d inputs failed", failed, len(args))
	}
	return nil
}

func cmdView(o *opts, args []string) error {
	mode, depth, err := o.render()
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return usageError("view takes one directory or image")
	}
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	start := newView(true, 100)
	if o.zoom != "fit" {
		pct, err := strconv.Atoi(strings.TrimSuffix(o.zoom, "%"))
		if err != nil || pct < zooms[0] || pct > zooms[len(zooms)-1] {
			return usageError(fmt.Sprintf("-z %q: want fit, or %d to %d", o.zoom, zooms[0], zooms[len(zooms)-1]))
		}
		start = newView(false, pct)
	}
	explicit := o.depth != "auto"
	return runTUI(path, look{mode: mode, depth: depth, aspect: o.aspect}, start, !o.noStatus, depth, explicit)
}
