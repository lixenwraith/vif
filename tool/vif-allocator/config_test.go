package main

import (
	"io"
	"testing"
	"time"
)

func TestParseConfigUsesFleetDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-image", "docker.io/library/vi-fighter:test",
		"-join-host", "play.example.com",
		"-page-base", "https://play.example.com/projects/vi-fighter/session/",
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
		{"-join-host", "play.example.com", "-page-base", "https://play.example.com/session/"},
		{"-image", "vi-fighter:test", "-page-base", "https://play.example.com/session/"},
		{"-image", "vi-fighter:test", "-join-host", "play.example.com"},
	} {
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Fatalf("parseConfig(%q) succeeded", args)
		}
	}
}
