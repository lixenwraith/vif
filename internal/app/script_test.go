package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

type scriptResult struct {
	stats journal.ScriptStats
	err   error
}

func TestAuthoredScriptUsesTheHeadlessInjectionAndJournalPath(t *testing.T) {
	script, err := journal.ParseScript([]byte(`
schema = 1
ticks = 1
[[action]]
tick = 0
event = "ScreenResize"
payload = "width = 100\nheight = 30"
`))
	if err != nil {
		t.Fatalf("script: %v", err)
	}

	capture := journal.NewCapture()
	a, err := NewHeadless(Config{
		Mode: ModeHeadless, Resources: resource.Options{Embedded: true}, Seed: 0x5C71,
		Width: 80, Height: 24, Journal: true, JournalSink: capture,
	})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	driver, err := journal.NewScriptDriver(scriptTarget{a: a}, script)
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if err := driver.RunAll(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if a.Context().Width != 100 || a.Context().Height != 30 {
		t.Fatalf("context = %dx%d, want scripted 100x30", a.Context().Width, a.Context().Height)
	}

	records := capture.Records()
	if len(records) != 1 {
		t.Fatalf("journal records = %d, want the one scripted event", len(records))
	}
	if got := records[0]; got.Type != event.EventScreenResize || got.Origin != event.OriginDebug ||
		got.Domain != core.DomainPlayer || got.Tick != 0 {
		t.Fatalf("scripted record = %+v", got)
	}
}

func TestRunScriptPairsHeadlessNetworkInstances(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}

	writeScript := func(name, motion string) string {
		path := filepath.Join(t.TempDir(), name)
		body := fmt.Sprintf("schema = 1\nticks = 3\nwidth = 80\nheight = 24\n[[action]]\ntick = 0\nintent = %q\n", motion)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	hostPath := writeScript("host.toml", "motion_right")
	guestPath := writeScript("guest.toml", "motion_left")

	hostResult := make(chan scriptResult, 1)
	go func() {
		stats, err := RunScript(Config{
			Resources: resource.Options{Embedded: true}, Seed: 0x5C71, HostAddress: address, Participants: 2,
		}, hostPath)
		hostResult <- scriptResult{stats: stats, err: err}
	}()

	guestConfig := Config{Resources: resource.Options{Embedded: true}, JoinAddress: address}
	deadline := time.Now().Add(2 * time.Second)
	var guest journal.ScriptStats
	for {
		guest, err = RunScript(guestConfig, guestPath)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ECONNREFUSED) || time.Now().After(deadline) {
			t.Fatalf("guest script: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if guest.Executed != 1 || guest.Ticks != 3 {
		t.Fatalf("guest stats = %+v", guest)
	}

	select {
	case result := <-hostResult:
		if result.err != nil {
			t.Fatalf("host script: %v", result.err)
		}
		if result.stats.Executed != 1 || result.stats.Ticks != 3 {
			t.Fatalf("host stats = %+v", result.stats)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host script did not finish")
	}
}
