package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// standingPoll is how often the keeper looks for a standing session gone.
	standingPoll = 5 * time.Second
	// standingRetry is the least time between two makings of one standing session,
	// so one whose pod never stands costs a create a minute rather than a loop.
	standingRetry = time.Minute
)

// standingEntry is a session the router keeps for the lobby: a scenario its bots
// play, filling every seat, under the name its scenario gives it.
type standingEntry struct {
	ID       string
	Scenario string
	Bots     int
}

// parseStanding reads -standing: scenario:bots entries, comma-separated.
func parseStanding(raw string) ([]standingEntry, error) {
	if raw == "" {
		return nil, nil
	}
	var out []standingEntry
	for _, item := range strings.Split(raw, ",") {
		scenario, count, _ := strings.Cut(item, ":")
		bots, err := strconv.Atoi(count)
		switch {
		case !scenarioPattern.MatchString(scenario) || err != nil:
			return nil, fmt.Errorf("-standing %q is not scenario:bots", item)
		case bots < 1 || bots > sessionBotsMax:
			return nil, fmt.Errorf("-standing %s seats %d bots, which must be 1 to %d", scenario, bots, sessionBotsMax)
		case slices.ContainsFunc(out, func(e standingEntry) bool { return e.Scenario == scenario }):
			return nil, fmt.Errorf("-standing names %s twice", scenario)
		}
		out = append(out, standingEntry{ID: standingID(scenario), Scenario: scenario, Bots: bots})
	}
	return out, nil
}

// standingID is the name a standing session answers to in every pod it is made in,
// so its links outlive each one: its scenario's digest, cut to a session ID.
func standingID(scenario string) string {
	sum := sha256.Sum256([]byte("vif-standing:" + scenario))
	return hex.EncodeToString(sum[:8])
}

// keep makes each standing session at start and again whenever it is gone, until
// ctx ends; see doc/kubernetes-fleet.md §9.
func (a *allocator) keep(ctx context.Context, log *slog.Logger) {
	made := make(map[string]time.Time)
	tick := time.NewTicker(standingPoll)
	defer tick.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, standingPoll)
		a.keepPass(pass, made, log)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// keepPass makes the standing sessions that are gone, at most one a standingRetry
// each, and removes one that finished, whose name its successor takes, and one
// under a name the router keeps none under.
func (a *allocator) keepPass(ctx context.Context, made map[string]time.Time, log *slog.Logger) {
	jobs, err := a.kube.listJobs(ctx)
	if err != nil {
		log.Warn("standing sessions not read", "error", err)
		return
	}
	found := make(map[string]job)
	for _, item := range jobs {
		if id, ok := sessionID(item.Metadata); ok && isStanding(item.Metadata) {
			found[id] = item
		}
	}
	for id, item := range found {
		if !a.standing(id) && item.Metadata.DeletionTimestamp == nil {
			a.remove(ctx, log, id, "the router keeps none of its name")
		}
	}
	for _, e := range a.cfg.Standing {
		item, ok := found[e.ID]
		switch {
		case ok && item.Metadata.DeletionTimestamp == nil && jobFinished(item):
			a.remove(ctx, log, e.ID, "it ended")
		case ok || time.Since(made[e.ID]) < standingRetry:
			// standing, going, or made too lately to make again
		default:
			made[e.ID] = time.Now()
			if err := a.createStanding(ctx, e); err != nil {
				log.Warn("standing session not made", "session", e.ID, "scenario", e.Scenario, "error", err)
				continue
			}
			log.Info("standing session made", "session", e.ID, "scenario", e.Scenario, "bots", e.Bots)
		}
	}
}

// remove deletes a standing session's objects, for why.
func (a *allocator) remove(ctx context.Context, log *slog.Logger, id, why string) {
	log.Info("standing session removed", "session", id, "reason", why)
	if err := a.removeObjects(ctx, id); err != nil {
		log.Warn("standing session not removed", "session", id, "error", err)
	}
}

// createStanding makes e as a create does, under its own name and charged to no
// address: watch-only, its bots filling every seat.
func (a *allocator) createStanding(ctx context.Context, e standingEntry) error {
	workload := a.cfg.Workload
	workload.Scenario, workload.Players, workload.Bots, workload.WatchOnly = e.Scenario, e.Bots, e.Bots, true
	a.createMu.Lock()
	defer a.createMu.Unlock()
	if err := a.room(ctx, workload); err != nil {
		return err
	}
	_, err := a.createObjects(ctx, e.ID, workload)
	return err
}

// removeStanding deletes every standing session and waits for their pods to go, so
// a router stopped for an update leaves the fleet as its players left it.
func (a *allocator) removeStanding(ctx context.Context) error {
	jobs, err := a.kube.listJobs(ctx)
	if err != nil {
		return fmt.Errorf("list Jobs: %w", err)
	}
	var joined error
	for _, item := range jobs {
		if id, ok := sessionID(item.Metadata); ok && isStanding(item.Metadata) {
			joined = errors.Join(joined, a.removeObjects(ctx, id))
		}
	}
	for joined == nil {
		pods, err := a.kube.listPods(ctx, standingSelector)
		switch {
		case err != nil:
			return fmt.Errorf("list Pods: %w", err)
		case len(pods) == 0:
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d standing pod(s) still going: %w", len(pods), ctx.Err())
		case <-time.After(a.cfg.PollInterval):
		}
	}
	return joined
}

// removeObjects deletes session id's Service and its Job, whose pod goes with it.
func (a *allocator) removeObjects(ctx context.Context, id string) error {
	return errors.Join(a.kube.deleteService(ctx, sessionPrefix+id), a.kube.deleteJob(ctx, sessionPrefix+id))
}

// isStanding reports a standing session's object.
func isStanding(meta objectMeta) bool {
	return isManaged(meta.Labels) && meta.Labels[labelWatchOnly] == "true"
}
