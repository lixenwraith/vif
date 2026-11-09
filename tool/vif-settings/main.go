// Command vif-settings brings the vif.toml it is given up to this build's in
// place (paths.UpgradeSettings), as make install does one an earlier install left:
// what this build does not take goes, what it ships and the file lacks comes with
// its comments, and every other line stays.
package main

import (
	"fmt"
	"os"

	"github.com/lixenwraith/vif/internal/paths"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: vif-settings <vif.toml>")
		os.Exit(2)
	}
	if err := upgrade(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func upgrade(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, notes, err := paths.UpgradeSettings(data)
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
