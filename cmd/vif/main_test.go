package main

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func TestLogFlags(t *testing.T) {
	tests := []struct {
		name                string
		args                []string
		enabled             bool
		dir, level, scope   string
		statTicks, recTicks int
	}{
		{name: "unset"},
		{name: "bare log", args: []string{"-l"}, enabled: true},
		{name: "log directory", args: []string{"-l=./tmp"}, enabled: true, dir: "./tmp"},
		{
			name: "bare log keeps directory", args: []string{"-l=./tmp", "-l"},
			enabled: true, dir: "./tmp",
		},
		{name: "log alias", args: []string{"-log=./tmp"}, enabled: true, dir: "./tmp"},
		{
			name:    "all values",
			args:    []string{"-lv", "trace", "-ls=afs", "-lt", "200", "-lr=0"},
			enabled: true, level: "trace", scope: "afs", statTicks: 200, recTicks: -1,
		},
		{
			name: "tick disables", args: []string{"-lt=0", "-lr=0"},
			enabled: true, statTicks: -1, recTicks: -1,
		},
		{name: "scope alias", args: []string{"-log-scope=afs"}, enabled: true, scope: "afs"},
		{name: "explicit log disable", args: []string{"-l=false"}},
		{
			name:    "implied log survives direct disable",
			args:    []string{"-lv", "info", "-l=false"},
			enabled: true, level: "info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, logs, _ := newDiagnosticFlagSet()
			if err := fs.Parse(tt.args); err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := logs.enabled(); got != tt.enabled {
				t.Errorf("enabled() = %v, want %v", got, tt.enabled)
			}
			if got := logs.dir.value; got != tt.dir {
				t.Errorf("dir = %q, want %q", got, tt.dir)
			}
			if got := logs.level.value; got != tt.level {
				t.Errorf("level = %q, want %q", got, tt.level)
			}
			if got := logs.scope.value; got != tt.scope {
				t.Errorf("scope = %q, want %q", got, tt.scope)
			}
			if got := logs.stat.value; got != tt.statTicks {
				t.Errorf("stat ticks = %d, want %d", got, tt.statTicks)
			}
			if got := logs.rec.value; got != tt.recTicks {
				t.Errorf("rec ticks = %d, want %d", got, tt.recTicks)
			}
		})
	}
}

func TestLogFlagsImplyLogging(t *testing.T) {
	tests := [][]string{
		{"-lv", "info"},
		{"-ls", "afs"},
		{"-lt", "1"},
		{"-lr", "1"},
	}
	for _, args := range tests {
		fs, logs, _ := newDiagnosticFlagSet()
		if err := fs.Parse(args); err != nil {
			t.Fatalf("Parse(%v) error = %v", args, err)
		}
		if !logs.enabled() {
			t.Errorf("Parse(%v) did not enable logging", args)
		}
	}
}

func TestScopeFlagValidatesDuringParse(t *testing.T) {
	fs, _, _ := newDiagnosticFlagSet()
	err := fs.Parse([]string{"-ls", "not-a-scope"})
	if err == nil || !strings.Contains(err.Error(), "unknown scope") {
		t.Fatalf("Parse() error = %v, want unknown scope", err)
	}
}

func TestTickFlagRejectsNegativeValues(t *testing.T) {
	fs, _, _ := newDiagnosticFlagSet()
	err := fs.Parse([]string{"-lt", "-1"})
	if err == nil || !strings.Contains(err.Error(), "non-negative tick count") {
		t.Fatalf("Parse() error = %v, want non-negative tick count", err)
	}
}

func TestDevFlagTriState(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		fallback bool
		set      bool
		want     bool
	}{
		{name: "unset defers false", fallback: false, want: false},
		{name: "unset defers true", fallback: true, want: true},
		{name: "bare forces on", args: []string{"-dev"}, set: true, want: true},
		{name: "false forces off", args: []string{"-dev=false"}, fallback: true, set: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, _, dev := newDiagnosticFlagSet()
			if err := fs.Parse(tt.args); err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if dev.set != tt.set {
				t.Errorf("set = %v, want %v", dev.set, tt.set)
			}
			if got := dev.valueOr(tt.fallback); got != tt.want {
				t.Errorf("valueOr(%v) = %v, want %v", tt.fallback, got, tt.want)
			}
		})
	}
}

func newDiagnosticFlagSet() (*flag.FlagSet, *logFlags, *setFlag[bool]) {
	fs := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	logs := newLogFlags()
	logs.register(fs)
	dev := newSetFlag(true, parseBoolFlag)
	fs.Var(&dev, "dev", "")
	return fs, logs, &dev
}
