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

func TestJournalFlagAcceptsIndependentDirectory(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		enabled bool
		dir     string
	}{
		{args: []string{"-j"}, enabled: true},
		{args: []string{"-j=./journal"}, enabled: true, dir: "./journal"},
		{args: []string{"-journal=./journal"}, enabled: true, dir: "./journal"},
		{args: []string{"-j=false"}},
	} {
		fs := flag.NewFlagSet("journal", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		journal := newSetFlag(true, parseOutputDirFlag)
		fs.Var(&journal, "j", "")
		fs.Var(&journal, "journal", "")
		if err := fs.Parse(tt.args); err != nil {
			t.Fatalf("Parse(%v) error = %v", tt.args, err)
		}
		if journal.set != tt.enabled || journal.value != tt.dir {
			t.Errorf("Parse(%v) = enabled %v dir %q, want %v %q",
				tt.args, journal.set, journal.value, tt.enabled, tt.dir)
		}
	}
}

func TestConfigFlagAliasesShareOneConfig(t *testing.T) {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := newConfigFlags()
	cfg.register(fs)
	args := []string{
		"-config-dir", "root", "-config-game", "game.toml",
		"-config-content", "content", "-config-keymap", "keys.toml",
		"-config-music", "music.toml", "-config-sounds", "sounds.toml",
		"-config-embedded=false",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if cfg.dir != "root" || cfg.game != "game.toml" || cfg.content != "content" ||
		cfg.keymap != "keys.toml" || cfg.music != "music.toml" || cfg.sounds != "sounds.toml" || cfg.embedded {
		t.Fatalf("config flags = %+v", cfg)
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

func TestSessionFlags(t *testing.T) {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var session sessionFlags
	session.register(fs)
	if err := fs.Parse([]string{"-host", ":7777", "-join", "host.example:7777"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if session.host != ":7777" || session.join != "host.example:7777" {
		t.Fatalf("session flags = host %q join %q", session.host, session.join)
	}
	if err := session.validateInvocation(false, true, ""); err == nil {
		t.Fatal("session flags accepted -check")
	}
	if err := (sessionFlags{players: 4}).validateInvocation(false, false, ""); err != nil {
		t.Fatalf("a solo run rejected its later host cap: %v", err)
	}
	if err := (sessionFlags{join: "host.example:7777", players: 4}).validateInvocation(false, false, ""); err == nil {
		t.Fatal("a joining guest accepted a host lobby cap")
	}
}

func TestScriptInvocation(t *testing.T) {
	hosted := sessionFlags{host: ":7777", players: 2}
	if err := validateInvocation(false, false, "", "scenario.toml", false, hosted); err != nil {
		t.Fatalf("hosted script rejected: %v", err)
	}
	if err := validateInvocation(false, false, "", "scenario.toml", true, hosted); err != nil {
		t.Fatalf("watched hosted script rejected: %v", err)
	}
	if err := validateInvocation(false, false, "run.jrn", "scenario.toml", false, sessionFlags{}); err == nil {
		t.Fatal("-replay and -script were accepted together")
	}
	if err := validateInvocation(true, false, "", "scenario.toml", false, sessionFlags{}); err == nil {
		t.Fatal("-schema and -script were accepted together")
	}
	if err := validateInvocation(false, false, "", "", true, sessionFlags{}); err == nil {
		t.Fatal("-watch was accepted without a script to present")
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
