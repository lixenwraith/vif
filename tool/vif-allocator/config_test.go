package main

import (
	"io"
	"slices"
	"testing"
	"time"
)

func TestParseConfigUsesFleetDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-image", "docker.io/library/vif:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/projects/vif/session/",
		"-log-stream-url", "http://127.0.0.1:8081/stream",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9080" || cfg.KubeAPI != "https://127.0.0.1:6443" {
		t.Fatalf("unexpected listener configuration: %+v", cfg)
	}
	if cfg.Allocator.PortFirst != 31700 || cfg.Allocator.PortLast != 31709 {
		t.Fatalf("unexpected port range: %d-%d", cfg.Allocator.PortFirst, cfg.Allocator.PortLast)
	}
	if cfg.Allocator.Workload.FirstJoin != "90s" ||
		cfg.Allocator.Workload.Empty != "90s" ||
		cfg.Allocator.Workload.Drain != "20s" {
		t.Fatalf("unexpected lifecycle defaults: %+v", cfg.Allocator.Workload)
	}
	if cfg.Allocator.ReadyTimeout != 75*time.Second {
		t.Fatalf("ready timeout = %s", cfg.Allocator.ReadyTimeout)
	}
}

func TestParseConfigRequiresFixedSiteValues(t *testing.T) {
	for _, args := range [][]string{
		{"-join-host", "play.example.com", "-page-base", "https://play.example.com/session/", "-log-stream-url", "http://127.0.0.1:8081/stream"},
		{"-image", "vif:test", "-page-base", "https://play.example.com/session/", "-log-stream-url", "http://127.0.0.1:8081/stream"},
		{"-image", "vif:test", "-join-host", "play.example.com", "-log-stream-url", "http://127.0.0.1:8081/stream"},
		{"-image", "vif:test", "-join-host", "play.example.com", "-page-base", "https://play.example.com/session/"},
	} {
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Fatalf("parseConfig(%q) succeeded", args)
		}
	}
}

func TestParseConfigRejectsUnsafeLogStreamURL(t *testing.T) {
	base := []string{
		"-image", "docker.io/library/vif:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/session/",
	}
	for _, target := range []string{
		"https://127.0.0.1:8081/stream",
		"http://localhost:8081/stream",
		"http://192.0.2.10:8081/stream",
		"http://127.0.0.1/stream",
		"http://127.0.0.1:8081/status",
		"http://127.0.0.1:8081/stream?token=value",
		"http://user@127.0.0.1:8081/stream",
	} {
		args := append(append([]string{}, base...), "-log-stream-url", target)
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Fatalf("parseConfig accepted unsafe log stream URL %q", target)
		}
	}
}

// TestRequestBoundsFailClosed pins what an unconfigured deployment will accept: the
// default roster and nothing more verbose than debug, so publishing the API does not
// hand an anonymous caller the fleet's log rate or a sixteen-player world.
func TestRequestBoundsFailClosed(t *testing.T) {
	base := []string{
		"-image", "docker.io/library/vif:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/session/",
		"-log-stream-url", "http://127.0.0.1:8081/stream",
	}
	cfg, err := parseConfig(base, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Allocator.PlayersMax != cfg.Allocator.Workload.Players {
		t.Fatalf("players-max = %d, want the -players default %d",
			cfg.Allocator.PlayersMax, cfg.Allocator.Workload.Players)
	}
	if slices.Contains(cfg.Allocator.LogLevels, "trace") {
		t.Fatalf("trace is selectable by default: %v", cfg.Allocator.LogLevels)
	}
	for _, extra := range [][]string{
		{"-players", "8", "-players-max", "4"},
		{"-players-max", "17"},
		{"-log-level-min", "shout"},
		{"-log-level", "trace"},
	} {
		args := append(append([]string{}, base...), extra...)
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Fatalf("parseConfig accepted %v", extra)
		}
	}
}

// TestAnUnsetUnitVariableKeepsTheDefault is the failure mode a unit has and a
// command line does not. systemd expands a variable the environment file never
// defined to an empty argument, so every flag the ExecStart names is passed with
// nothing behind it. A flag with a default must take the default; a numeric one
// must not fail to parse; a required one must still say which it is.
func TestAnUnsetUnitVariableKeepsTheDefault(t *testing.T) {
	required := []string{
		"-image", "docker.io/library/vif:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/projects/vif/session/",
		"-log-stream-url", "http://127.0.0.1:8081/stream",
	}
	unset := func(extra ...string) []string { return append(slices.Clone(required), extra...) }

	for _, args := range [][]string{
		unset(),
		unset("-scenario", "", "-wad", ""),
		unset("-scenario=", "-wad="),
		unset("-listen", "", "-players-max", "", "-log-level-min", "", "-scenario", "", "-wad", ""),
	} {
		cfg, err := parseConfig(args, io.Discard)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if cfg.Allocator.WadDir != defaultWadDir || cfg.Allocator.Workload.Scenario != "main" ||
			cfg.Listen != ":9080" || cfg.Allocator.PlayersMax != cfg.Allocator.Workload.Players {
			t.Errorf("%v: an empty variable overrode a default: %+v", args, cfg.Allocator)
		}
	}

	cfg, err := parseConfig(unset("-wad", "/srv/wad", "-scenario", "td"), io.Discard)
	if err != nil || cfg.Allocator.WadDir != "/srv/wad" || cfg.Allocator.Workload.Scenario != "td" {
		t.Fatalf("a set variable was not honoured: %+v, %v", cfg.Allocator, err)
	}
	if _, err := parseConfig(unset("-wad", "relative"), io.Discard); err == nil {
		t.Error("a relative volume was accepted")
	}
	if _, err := parseConfig([]string{"-image", "", "-join-host", "h",
		"-page-base", "https://h/p/", "-log-stream-url", "http://127.0.0.1:8081/stream"},
		io.Discard); err == nil {
		t.Error("an empty required flag was accepted")
	}
}

func TestTheBrowserRouteAndItsBridgeAreOneSwitch(t *testing.T) {
	base := []string{
		"-image", "vif:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/session/",
		"-log-stream-url", "http://127.0.0.1:8081/stream",
	}
	for _, half := range [][]string{
		{"-web-origin", "https://play.example.com"},
		{"-ws-bridge-image", "ws-bridge:test"},
		{"-web-origin", "https://play.example.com/vif", "-ws-bridge-image", "ws-bridge:test"},
	} {
		if _, err := parseConfig(append(slices.Clone(base), half...), io.Discard); err == nil {
			t.Errorf("parseConfig(%q) published a half-configured browser route", half)
		}
	}
	cfg, err := parseConfig(append(slices.Clone(base),
		"-web-origin", "https://play.example.com", "-ws-bridge-image", "ws-bridge:test"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := (&allocator{cfg: cfg.Allocator}).webSocketURL("0123456789abcdef"); got !=
		"wss://play.example.com/vif/ws/0123456789abcdef" {
		t.Fatalf("webSocketURL = %q", got)
	}
}
