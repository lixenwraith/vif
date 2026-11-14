// Command vif-settings brings each settings file it is given, a vif.toml or an
// input/keymap.toml, up to this build's in place (paths.UpgradeSettings,
// input.UpgradeKeymap), as make install does the ones an earlier install left and a
// run its player's own: what this build does not take goes, what it ships and the
// file lacks comes with its comments, and every other line stays.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: vif-settings <%s | %s>...\n", paths.SettingsFile, paths.KeymapConfigFile)
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		if err := upgrade(path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func upgrade(path string) error {
	up := paths.UpgradeSettings
	switch filepath.Base(path) {
	case paths.SettingsFile:
	case paths.KeymapConfigFile:
		up = input.UpgradeKeymap
	default:
		return fmt.Errorf("neither a %s nor a %s", paths.SettingsFile, paths.KeymapConfigFile)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, notes, err := up(data)
	if err != nil || len(notes) == 0 {
		if err == nil {
			fmt.Println("keep    " + path)
		}
		return err
	}
	if err := (paths.FileStore{Path: path}).Store(out); err != nil {
		return err
	}
	fmt.Println("upgrade " + path)
	for _, n := range notes {
		fmt.Println("        " + n)
	}
	return nil
}
