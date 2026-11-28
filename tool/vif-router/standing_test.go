package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/probe"
)

// TestTheRouterKeepsTheStandingSessionsItNames: each standing session is made under
// its scenario's name, watch-only, its bots in every seat, shedding none and with no
// deadline; again once it ended, no sooner than its retry; and one of a name the
// router keeps none under is removed. A watch between its pods finds it starting.
func TestTheRouterKeepsTheStandingSessionsItNames(t *testing.T) {
	ctx, log := context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil))
	kube := &fakeKube{}
	cfg := testAllocatorConfig(t)
	var err error
	if cfg.Standing, err = parseStanding("fighter:2"); err != nil {
		t.Fatal(err)
	}
	controller := newAllocator(kube, fakeHealth{state: sessionHealth{
		Live: true, Phase: "occupied", Clock: probe.ClockRunning, Tick: 90}}, cfg)
	id := standingID("fighter")
	if _, err := controller.routeSession(ctx, id, network.AdmitWatch); !errors.Is(err, errFeedOver) {
		t.Fatalf("a watch before the standing session stands answered %v, want it starting again", err)
	}
	stray := cfg.Workload
	stray.WatchOnly = true
	if _, err := controller.createObjects(ctx, "00000000000000aa", stray); err != nil {
		t.Fatal(err)
	}

	made := map[string]time.Time{}
	controller.keepPass(ctx, made, log)
	if !slices.Contains(kube.deletedJobs, sessionPrefix+"00000000000000aa") {
		t.Errorf("a standing session the router keeps none of stayed: deleted %v", kube.deletedJobs)
	}
	if len(kube.jobObjects) != 2 {
		t.Fatalf("the keeper made %d Job(s) beside the stray, want the one it keeps", len(kube.jobObjects)-1)
	}
	encoded, _ := json.Marshal(kube.jobObjects[1])
	service, _ := json.Marshal(kube.svcObjects[1])
	text := string(encoded)
	for _, want := range []string{`"name":"vif-session-` + id + `"`, `"vif.lixenwraith.dev/watch-only":"true"`,
		`"-players","2","-bot","2"`, `"-shed-window","0"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the standing Job does not contain %s", want)
		}
	}
	if strings.Contains(text, "activeDeadlineSeconds") || !strings.Contains(string(service), `"vif.lixenwraith.dev/watch-only":"true"`) {
		t.Errorf("the standing Job holds a deadline, or its Service is not marked: %s", service)
	}
	if sessions, err := controller.listSessions(ctx); err != nil || len(sessions) != 1 || !sessions[0].WatchOnly {
		t.Errorf("the listing reads %+v, %v; want the standing session, watch-only", sessions, err)
	}
	if ip, err := controller.routeSession(ctx, id, network.AdmitWatch); err != nil || ip != "10.42.0.20" {
		t.Errorf("a watch of the standing session reached %q, %v", ip, err)
	}

	controller.keepPass(ctx, made, log)
	if len(kube.jobObjects) != 2 {
		t.Fatal("the keeper made a standing session that stands again")
	}
	kube.mu.Lock()
	kube.jobs[0].Status.Failed = 1
	kube.mu.Unlock()
	controller.keepPass(ctx, made, log)
	controller.keepPass(ctx, made, log)
	if !slices.Contains(kube.deletedJobs, sessionPrefix+id) || len(kube.jobObjects) != 2 {
		t.Fatalf("an ended standing session was not removed, or made again within its retry: deleted %v", kube.deletedJobs)
	}
	made[id] = made[id].Add(-standingRetry)
	controller.keepPass(ctx, made, log)
	if len(kube.jobObjects) != 3 {
		t.Fatal("an ended standing session was not made again")
	}
}

// TestAStoppingRouterRemovesItsStandingSessionsAlone: what the router keeps goes
// with it, pods and all, before it stops; a player's session stays.
func TestAStoppingRouterRemovesItsStandingSessionsAlone(t *testing.T) {
	ctx := context.Background()
	kube := &fakeKube{}
	cfg := testAllocatorConfig(t)
	var err error
	if cfg.Standing, err = parseStanding("fighter:2,td:1"); err != nil {
		t.Fatal(err)
	}
	controller := newAllocator(kube, fakeHealth{state: sessionHealth{
		Live: true, Ready: true, Capacity: 4, Phase: "waiting"}}, cfg)
	controller.newID = func() (string, error) { return "00000000000000aa", nil }
	if _, err := controller.createSession(ctx, sessionRequest{}); err != nil {
		t.Fatal(err)
	}
	controller.keepPass(ctx, map[string]time.Time{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := controller.removeStanding(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := kube.listJobs(ctx)
	pods, _ := kube.listPods(ctx, labelPartOf+"="+partOfName)
	if len(jobs) != 1 || len(pods) != 1 || jobs[0].Metadata.Name != sessionPrefix+"00000000000000aa" {
		t.Fatalf("the router stopped leaving %d Job(s) and %d pod(s), want the player's alone", len(jobs), len(pods))
	}
}
