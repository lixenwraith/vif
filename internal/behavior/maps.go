package behavior

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/parameter"
)

// mapsDoc is the maps a scenario's entry declares
type mapsDoc struct {
	Maps map[string]*mapDoc `toml:"maps"`
}

// mapDoc is one map: rows of one width, a character a cell, and the legend that
// says what each character but a space lays
type mapDoc struct {
	Rows   []string             `toml:"rows"`
	Legend map[string]legendDoc `toml:"legend"`
}

// legendDoc is what one character lays: an instance of a program, its header there
type legendDoc struct {
	Program string `toml:"program"`
}

// compileMaps builds the maps an entry's [maps] declares, in sorted-name order, each
// fault naming its map and place; a legend names a program among programs. No other
// key of entry is read.
func compileMaps(entry map[string]any, programs []string) ([]component.MapLayout, []error) {
	raw, ok := entry["maps"]
	if !ok {
		return nil, nil
	}
	doc, err := dialect.Decode[mapsDoc](map[string]any{"maps": raw})
	if err != nil {
		return nil, []error{err}
	}
	names := slices.Sorted(maps.Keys(doc.Maps))
	if len(names) > parameter.MapsMax {
		return nil, []error{fmt.Errorf("%d maps, at most %d", len(names), parameter.MapsMax)}
	}
	var out []component.MapLayout
	var errs []error
	for _, name := range names {
		layout, err := doc.Maps[name].layout(name, programs)
		if err != nil {
			for _, e := range err {
				errs = append(errs, fmt.Errorf("map '%s': %w", name, e))
			}
		}
		out = append(out, layout)
	}
	return out, errs
}

// layout reads a map's rows through its legend
func (d *mapDoc) layout(name string, programs []string) (m component.MapLayout, errs []error) {
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if !wellNamed(name) {
		fail("name is not [a-z][a-z0-9_]* of at most %d bytes", parameter.BehaviorNameMax)
	}
	if d == nil || len(d.Rows) == 0 {
		return m, append(errs, errors.New("rows lists no row"))
	}
	lays := make(map[rune]int, len(d.Legend))
	for _, key := range slices.Sorted(maps.Keys(d.Legend)) {
		r, size := utf8.DecodeRuneInString(key)
		program, known := slices.BinarySearch(programs, d.Legend[key].Program)
		switch {
		case size != len(key) || r == ' ' || r == utf8.RuneError:
			fail("legend: %.16q is not one character, a space being open map", key)
		case !known:
			fail("legend: %q: program %q is not one of the scenario's behavior programs", key, d.Legend[key].Program)
		default:
			lays[r] = program
		}
	}
	m = component.MapLayout{Name: name, Width: utf8.RuneCountInString(d.Rows[0]), Height: len(d.Rows)}
	for y, row := range d.Rows {
		if w := utf8.RuneCountInString(row); w != m.Width {
			fail("rows[%d] is %d cells wide, and the first %d", y, w, m.Width)
			continue
		}
		x := 0
		for _, r := range row {
			if program, ok := lays[r]; ok {
				m.Spawns = append(m.Spawns, component.MapSpawn{X: x, Y: y, Program: uint8(program)})
			} else if r != ' ' {
				fail("rows[%d]: %q at column %d is no entry of the legend", y, r, x)
			}
			x++
		}
	}
	if len(m.Spawns) > parameter.MapSpawnsMax {
		fail("%d instances, at most %d a map", len(m.Spawns), parameter.MapSpawnsMax)
	}
	return m, errs
}
