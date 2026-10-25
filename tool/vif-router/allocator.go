package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
)

var (
	errFleetFull         = errors.New("all session ports are allocated")
	errSessionNotReady   = errors.New("session did not become ready")
	errRequestRefused    = errors.New("session request refused")
	errSessionUnknown    = &network.Refusal{Code: network.RefusalUnknown, Reason: "no such session"}
	errSessionUnroutable = errors.New("session has no reachable pod")
)

type allocatorConfig struct {
	Workload   workloadConfig
	JoinHost   string
	RoutePort  int
	PortFirst  int
	PortLast   int
	PlayersMax int
	BotsMax    int
	LogLevels  []string
	WadDir     string

	// WebOrigin is the one origin the browser route answers, and publishing it is
	// what turns that route on; see doc/kubernetes-fleet.md §9.
	WebOrigin string
	// RouteMax bounds one session's connections through the front door and the
	Tunnels int // doors runs may publish through the router at once; 0 publishes none
	// DoorsPerAddress bounds the doors one address holds at once; 0 leaves it to -tunnels.
	DoorsPerAddress int
	// browser route together; see holds.
	RouteMax int
	// ClientAddressHeader is the header the site's edge overwrites with the player's
	// address; ClientJoins and ClientCreates are that address's budgets.
	ClientAddressHeader string
	ClientJoins         int
	ClientCreates       int

	ReadyTimeout   time.Duration
	PollInterval   time.Duration
	CleanupTimeout time.Duration
}

// sessionRequest is what a caller may choose about the session it is asking for.
// Every field is optional and nothing else in the workload is selectable: the
// image, the lifetime bounds, the mounts and the environment are the deployment's.
// Size is the host's screen (-size), which a scenario may or may not make its map.
type sessionRequest struct {
	Players  int    `json:"players,omitempty"`
	Bots     int    `json:"bots,omitempty"`
	LogLevel string `json:"log_level,omitempty"`
	Scenario string `json:"scenario,omitempty"`
	Size     string `json:"size,omitempty"`
}

// fleetLimits is what the deployment will accept and what an omitted choice gets,
// so a caller can offer only the choices this router would allow, and name its
// defaults, rather than discovering either by refusal. Descriptions, FixedMaps,
// MinPlayers and MaxPlayers hold what each scenario's entry states, keyed by name;
// one that states nothing is absent. They sit beside Scenarios so a caller that
// reads the names alone is unaffected.
type fleetLimits struct {
	PlayersMax      int               `json:"players_max"`
	PlayersDefault  int               `json:"players_default"`
	BotsMax         int               `json:"bots_max"`
	LogLevels       []string          `json:"log_levels"`
	LogLevelDefault string            `json:"log_level_default"`
	SizeDefault     string            `json:"size_default"`
	Scenarios       []string          `json:"scenarios"`
	Descriptions    map[string]string `json:"scenario_descriptions,omitempty"`
	FixedMaps       map[string]string `json:"scenario_fixed_maps,omitempty"`
	MinPlayers      map[string]int    `json:"scenario_min_players,omitempty"`
	MaxPlayers      map[string]int    `json:"scenario_max_players,omitempty"`
	// SessionsMax is the fleet's size, its NodePort pool; Doors and DoorsMax the doors
	// runs publish through the router and the most it carries, for a page to say
	// what the site has room for.
	SessionsMax int `json:"sessions_max"`
	Doors       int `json:"doors"`
	DoorsMax    int `json:"doors_max"`
}

type session struct {
	ID   string `json:"id"`
	Port int    `json:"port"`
	// JoinTarget is the front door, one address for every session; DirectTarget is
	// this session's own NodePort. Both carry the name the pod answers to.
	JoinTarget   string `json:"join_target"`
	DirectTarget string `json:"direct_target"`
	// WebSocketURL is the same session reached from a browser. Absent where the
	// deployment publishes no browser route, which is what a page keyed on it reads
	// as "this fleet is for native clients".
	WebSocketURL string `json:"ws_url,omitempty"`
	// Scenario and Size are what it was created with; State.Map is the map it has.
	Scenario  string        `json:"scenario,omitempty"`
	Size      string        `json:"size,omitempty"`
	CreatedAt string        `json:"created_at,omitempty"`
	Routable  bool          `json:"routable"`
	State     sessionHealth `json:"state"`
}

type reconcileResult struct {
	JobsDeleted     int
	ServicesDeleted int
}

type allocator struct {
	kube   kubeAPI
	health healthProbe
	cfg    allocatorConfig
	newID  func() (string, error)

	createMu sync.Mutex
}

type createdObjects struct {
	id      string
	port    int
	job     job
	service service
}

func newAllocator(kube kubeAPI, health healthProbe, cfg allocatorConfig) *allocator {
	return &allocator{kube: kube, health: health, cfg: cfg, newID: randomSessionID}
}

func (a *allocator) limits() fleetLimits {
	w := a.cfg.Workload
	out := fleetLimits{PlayersMax: a.cfg.PlayersMax, PlayersDefault: w.Players, BotsMax: a.cfg.BotsMax,
		LogLevels: a.cfg.LogLevels, LogLevelDefault: w.LogLevel, SizeDefault: w.Size,
		Scenarios: a.scenarios(), SessionsMax: a.cfg.PortLast - a.cfg.PortFirst + 1}
	a.describe(&out)
	return out
}

// scenarios is what the node's volume holds, read per request rather than kept
// from start-up: the tree is replaced by rename while this runs, so a list taken
// at boot would advertise what was installed before the last swap. The default is
// always offered, so a volume that cannot be read still yields a session — and a
// name that is there but broken is the init container's to refuse, not this.
func (a *allocator) scenarios() []string {
	root := filepath.Join(a.cfg.WadDir, paths.ScenarioDirName)
	out := []string{a.cfg.Workload.Scenario}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == a.cfg.Workload.Scenario ||
			!scenarioPattern.MatchString(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, name, paths.ScenarioFile)); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// describeScan bounds what is read of an entry file for its description, player
// range and [map] fixed. The first two are top-level, which TOML places ahead of the
// first table; any one past this leaves the scenario offered but undescribed by it.
const describeScan = 4 << 10

// describe fills each of limits' scenarios' description, player range and [map]
// fixed from its entry file, per request for the reason the names are. A main the
// volume does not hold is the game's embedded fallback, so it is described by that
// one. An unreadable or malformed entry is left undescribed: describing is not this
// service's check to fail.
func (a *allocator) describe(limits *fleetLimits) {
	root := filepath.Join(a.cfg.WadDir, paths.ScenarioDirName)
	limits.Descriptions, limits.FixedMaps = map[string]string{}, map[string]string{}
	limits.MinPlayers, limits.MaxPlayers = map[string]int{}, map[string]int{}
	players := map[string]map[string]int{"min_players": limits.MinPlayers, "max_players": limits.MaxPlayers}
	for _, name := range limits.Scenarios {
		head, err := readHead(filepath.Join(root, name, paths.ScenarioFile), describeScan)
		if errors.Is(err, fs.ErrNotExist) && name == paths.MainScenarioName {
			head, err = fs.ReadFile(asset.DefaultScenario, asset.DefaultScenarioEntry)
		}
		if err != nil {
			continue
		}
		doc := preamble(head)
		text, _ := doc["description"].(string)
		if text = strings.TrimSpace(text); text != "" {
			limits.Descriptions[name] = text
		}
		for key, into := range players {
			if n, ok := doc[key].(int64); ok && n >= 0 && n <= parameter.MaxPlayers {
				into[name] = int(n)
			}
		}
		if size := fixedMap(head); size != "" {
			limits.FixedMaps[name] = size
		}
	}
	limits.Descriptions, limits.FixedMaps = orNil(limits.Descriptions), orNil(limits.FixedMaps)
	limits.MinPlayers, limits.MaxPlayers = orNil(limits.MinPlayers), orNil(limits.MaxPlayers)
}

// orNil is an empty map as none, so its key is omitted.
func orNil[V any](m map[string]V) map[string]V {
	if len(m) == 0 {
		return nil
	}
	return m
}

// readHead returns at most limit bytes of a file, cut back to its last whole line
// when the file runs past them.
func readHead(name string, limit int) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(head) > limit {
		head = head[:bytes.LastIndexByte(head[:limit], '\n')+1]
	}
	return head, nil
}

// preamble parses only what precedes the first table header, the one place a
// top-level key can be, so a cut read never hands the parser half a table.
func preamble(data []byte) map[string]any {
	end := len(data)
	for offset := 0; offset < len(data); {
		line := data[offset:]
		if next := bytes.IndexByte(line, '\n'); next >= 0 {
			line = line[:next+1]
		}
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && trimmed[0] == '[' {
			end = offset
			break
		}
		offset += len(line)
	}
	doc, err := toml.NewParser(data[:end]).Parse()
	if err != nil {
		return nil
	}
	return doc
}

// fixedMap reads [map] fixed, the size a scenario says its map holds throughout,
// from the [map] table alone, so a cut read never hands the parser half of another.
// The game's loader has validated it; anything but "WxH" reads as none.
func fixedMap(data []byte) string {
	var table []byte
	in := false
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			if in {
				break
			}
			in = string(trimmed) == "[map]"
			continue
		}
		if in {
			table = append(table, line...)
		}
	}
	doc, err := toml.NewParser(table).Parse()
	if err != nil {
		return ""
	}
	size, _ := doc["fixed"].(string)
	if !sizePattern.MatchString(size) {
		return ""
	}
	return size
}

// resolve folds a caller's choices into this deployment's workload. An omitted
// choice keeps the default, moved into the scenario's player range; one outside what
// the operator opened or the scenario plays is refused rather than clamped, because
// a session quietly given a different roster than the one it asked for is worse than
// a request the caller can correct.
func (a *allocator) resolve(req sessionRequest) (workloadConfig, error) {
	workload := a.cfg.Workload
	limits := a.limits()
	if req.Scenario != "" {
		if !slices.Contains(limits.Scenarios, req.Scenario) {
			return workloadConfig{}, fmt.Errorf("%w: scenario must be one of %s",
				errRequestRefused, strings.Join(limits.Scenarios, ", "))
		}
		workload.Scenario = req.Scenario
	}
	low, high := 1, a.cfg.PlayersMax
	if n, ok := limits.MinPlayers[workload.Scenario]; ok {
		low = max(low, n)
	}
	if n, ok := limits.MaxPlayers[workload.Scenario]; ok {
		high = min(high, n)
	}
	switch {
	case low > high:
		return workloadConfig{}, fmt.Errorf("%w: scenario %s needs %d players and this fleet opens %d",
			errRequestRefused, workload.Scenario, low, high)
	case req.Players == 0:
		workload.Players = min(max(workload.Players, low), high)
	case req.Players < low || req.Players > high:
		return workloadConfig{}, fmt.Errorf("%w: players must be between %d and %d for %s",
			errRequestRefused, low, high, workload.Scenario)
	default:
		workload.Players = req.Players
	}
	// Bots are roster guests and a session is ready only while a seat is free, so
	// they leave at least one for the person who asked.
	if most := min(a.cfg.BotsMax, workload.Players-1); req.Bots < 0 || req.Bots > most {
		return workloadConfig{}, fmt.Errorf("%w: bots must be between 0 and %d for %d players",
			errRequestRefused, most, workload.Players)
	}
	workload.Bots = req.Bots
	if req.LogLevel != "" {
		if !slices.Contains(a.cfg.LogLevels, req.LogLevel) {
			return workloadConfig{}, fmt.Errorf("%w: log_level must be one of %s",
				errRequestRefused, strings.Join(a.cfg.LogLevels, ", "))
		}
		workload.LogLevel = req.LogLevel
	}
	if req.Size != "" {
		if err := checkSize(req.Size); err != nil {
			return workloadConfig{}, fmt.Errorf("%w: %w", errRequestRefused, err)
		}
		workload.Size = req.Size
	}
	return workload, nil
}

func (a *allocator) createSession(ctx context.Context, req sessionRequest) (session, error) {
	workload, err := a.resolve(req)
	if err != nil {
		return session{}, err
	}
	created, err := a.createTransaction(ctx, workload)
	if err != nil {
		return session{}, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, a.cfg.ReadyTimeout)
	defer cancel()
	state, err := a.waitForReady(waitCtx, created.id, created.service.Metadata.Name)
	if err != nil {
		cleanupErr := a.cleanup(created)
		if errors.Is(err, context.Canceled) {
			return session{}, errors.Join(err, cleanupErr)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return session{}, errors.Join(
				fmt.Errorf("%w after %s", errSessionNotReady, a.cfg.ReadyTimeout),
				cleanupErr,
			)
		}
		return session{}, errors.Join(fmt.Errorf("%w: %w", errSessionNotReady, err), cleanupErr)
	}

	return a.sessionRecord(created.id, created.port, created.job.Metadata, true, state), nil
}

func (a *allocator) createTransaction(ctx context.Context, workload workloadConfig) (createdObjects, error) {
	a.createMu.Lock()
	defer a.createMu.Unlock()

	services, err := a.kube.listServices(ctx)
	if err != nil {
		return createdObjects{}, fmt.Errorf("list Services: %w", err)
	}
	ports := a.availablePorts(services)
	if len(ports) == 0 {
		return createdObjects{}, errFleetFull
	}

	var createdJob job
	var id string
	for range 5 {
		id, err = a.newID()
		if err != nil {
			return createdObjects{}, fmt.Errorf("generate session ID: %w", err)
		}
		createdJob, err = a.kube.createJob(ctx, buildJob(id, workload))
		if isAPIStatus(err, http.StatusConflict) {
			continue
		}
		if err != nil {
			// The API may have accepted the request even when the response was
			// lost. Deleting the deterministic name is harmless on a true miss.
			possiblyCreated := createdObjects{job: job{Metadata: objectMeta{Name: sessionPrefix + id}}}
			return createdObjects{}, errors.Join(fmt.Errorf("create Job: %w", err), a.cleanup(possiblyCreated))
		}
		break
	}
	if createdJob.Metadata.UID == "" {
		if err != nil {
			return createdObjects{}, fmt.Errorf("create Job after ID collisions: %w", err)
		}
		cleanupErr := a.cleanup(createdObjects{job: createdJob})
		return createdObjects{}, errors.Join(fmt.Errorf("Kubernetes returned a Job without a UID"), cleanupErr)
	}

	created := createdObjects{id: id, job: createdJob}
	for _, port := range ports {
		createdService, createErr := a.kube.createService(
			ctx,
			buildService(id, createdJob.Metadata.UID, port, a.cfg.Workload.Namespace),
		)
		if createErr == nil {
			created.port = port
			created.service = createdService
			return created, nil
		}
		if isNodePortConflict(createErr) {
			continue
		}
		// As with Job creation, clean the known name in case only the API
		// response was lost. The owned Service is deleted before its Job.
		created.service.Metadata.Name = sessionPrefix + id
		cleanupErr := a.cleanup(created)
		return createdObjects{}, errors.Join(fmt.Errorf("create Service: %w", createErr), cleanupErr)
	}

	cleanupErr := a.cleanup(created)
	return createdObjects{}, errors.Join(errFleetFull, cleanupErr)
}

func (a *allocator) waitForReady(ctx context.Context, id, serviceName string) (sessionHealth, error) {
	selector := labelSession + "=" + id
	serviceSelector := "kubernetes.io/service-name=" + serviceName
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		pods, err := a.kube.listPods(ctx, selector)
		if err != nil {
			lastErr = fmt.Errorf("list Pods: %w", err)
		} else {
			slices, sliceErr := a.kube.listEndpointSlices(ctx, serviceSelector)
			if sliceErr != nil {
				lastErr = fmt.Errorf("list EndpointSlices: %w", sliceErr)
			} else {
				addresses := readyAddresses(slices)
				for _, item := range pods {
					if item.Status.Phase == "Failed" || item.Status.Phase == "Succeeded" {
						return sessionHealth{}, fmt.Errorf("pod entered terminal phase %s", item.Status.Phase)
					}
					if !podReady(item) || !addresses[item.Status.PodIP] {
						continue
					}
					state, probeErr := a.health.probe(ctx, item.Status.PodIP)
					if probeErr != nil {
						lastErr = fmt.Errorf("read pod health: %w", probeErr)
						continue
					}
					if state.Live && state.Ready {
						return state, nil
					}
					lastErr = fmt.Errorf("pod health is live=%t ready=%t phase=%s", state.Live, state.Ready, state.Phase)
				}
			}
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return sessionHealth{}, fmt.Errorf("%v: %w", lastErr, ctx.Err())
			}
			return sessionHealth{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *allocator) listSessions(ctx context.Context) ([]session, error) {
	jobs, err := a.kube.listJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Jobs: %w", err)
	}
	services, err := a.kube.listServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Services: %w", err)
	}
	pods, err := a.kube.listPods(ctx, labelPartOf+"="+partOfName)
	if err != nil {
		return nil, fmt.Errorf("list Pods: %w", err)
	}
	slices, err := a.kube.listEndpointSlices(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list EndpointSlices: %w", err)
	}

	servicesByName := make(map[string]service, len(services))
	for _, item := range services {
		if isManaged(item.Metadata.Labels) {
			servicesByName[item.Metadata.Name] = item
		}
	}
	podsByID := make(map[string][]pod)
	for _, item := range pods {
		if !isManaged(item.Metadata.Labels) {
			continue
		}
		id := item.Metadata.Labels[labelSession]
		if id != "" {
			podsByID[id] = append(podsByID[id], item)
		}
	}
	addressesByService := readyAddressesByService(slices)

	type candidate struct {
		id          string
		serviceName string
		port        int
		job         job
		pods        []pod
	}
	var candidates []candidate
	for _, item := range jobs {
		if !isManaged(item.Metadata.Labels) || jobFinished(item) || item.Metadata.DeletionTimestamp != nil {
			continue
		}
		id, ok := sessionID(item.Metadata)
		if !ok {
			continue
		}
		service, ok := servicesByName[item.Metadata.Name]
		if !ok || !a.validService(service, item) {
			continue
		}
		port, ok := firstNodePort(service)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{
			id:          id,
			serviceName: service.Metadata.Name,
			port:        port,
			job:         item,
			pods:        podsByID[id],
		})
	}

	result := make([]session, len(candidates))
	var wg sync.WaitGroup
	for i, item := range candidates {
		i, item := i, item
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := sessionHealth{Phase: "starting", Reason: "health unavailable"}
			routable := false
			addresses := addressesByService[item.serviceName]
			for _, candidatePod := range item.pods {
				if !podReady(candidatePod) {
					if candidatePod.Status.Phase != "" {
						state.Phase = strings.ToLower(candidatePod.Status.Phase)
					}
					continue
				}
				routable = addresses[candidatePod.Status.PodIP]
				probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				observed, probeErr := a.health.probe(probeCtx, candidatePod.Status.PodIP)
				cancel()
				if probeErr == nil {
					state = observed
					state.ExpiresIn = expiresIn(observed.ExpiresIn, item.job, time.Now())
					break
				}
				state.Reason = "health unavailable: " + probeErr.Error()
			}
			result[i] = a.sessionRecord(item.id, item.port, item.job.Metadata, routable, state)
		}()
	}
	wg.Wait()
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port == result[j].Port {
			return result[i].ID < result[j].ID
		}
		return result[i].Port < result[j].Port
	})
	return result, nil
}

// routeSession resolves one session identifier to the address of its one ready
// pod. The identifier is a routing key looked up in Kubernetes state; no caller
// ever names an upstream, which is the whole of why this is a lookup rather than
// a parameter.
func (a *allocator) routeSession(ctx context.Context, id string) (string, error) {
	pods, err := a.kube.listPods(ctx, labelSession+"="+id)
	if err != nil {
		return "", fmt.Errorf("list Pods: %w", err)
	}
	slices, err := a.kube.listEndpointSlices(ctx, "kubernetes.io/service-name="+sessionPrefix+id)
	if err != nil {
		return "", fmt.Errorf("list EndpointSlices: %w", err)
	}
	addresses := readyAddresses(slices)
	var skipped []string
	for _, item := range pods {
		managed, ready, routed := isManaged(item.Metadata.Labels), podReady(item), addresses[item.Status.PodIP]
		if !managed || !ready || !routed {
			skipped = append(skipped, fmt.Sprintf("%s %s managed=%t ready=%t endpoint=%t",
				item.Metadata.Name, item.Status.Phase, managed, ready, routed))
			continue
		}
		state, err := a.health.probe(ctx, item.Status.PodIP)
		if err != nil {
			return "", fmt.Errorf("read pod health: %w", err)
		}
		if !state.Live {
			return "", fmt.Errorf("%w: %s is not live: %s", errSessionUnroutable, item.Metadata.Name, state.Reason)
		}
		// The same answer the create path waits for. A session at capacity or
		// draining refuses the dial itself; refusing here costs it nothing and
		// tells the caller which of the two it is.
		if !state.Ready {
			return "", sessionRefusing(state.Phase)
		}
		return item.Status.PodIP, nil
	}
	if len(pods) == 0 {
		return "", errSessionUnknown
	}
	return "", fmt.Errorf("%w: %s", errSessionUnroutable, strings.Join(skipped, "; "))
}

// sessionRefusing is a live pod's refusal, of the kind its phase names: a session
// draining or expired is ending, and any other one is full.
func sessionRefusing(phase string) error {
	code := network.RefusalFull
	if phase == lifecycle.PhaseDraining.String() || phase == lifecycle.PhaseExpired.String() {
		code = network.RefusalEnding
	}
	return &network.Refusal{Code: code, Reason: "session is not accepting players: " + phase}
}

func (a *allocator) reconcile(ctx context.Context) (reconcileResult, error) {
	jobs, err := a.kube.listJobs(ctx)
	if err != nil {
		return reconcileResult{}, fmt.Errorf("list Jobs: %w", err)
	}
	services, err := a.kube.listServices(ctx)
	if err != nil {
		return reconcileResult{}, fmt.Errorf("list Services: %w", err)
	}

	jobsByName := make(map[string]job)
	servicesByName := make(map[string]service)
	for _, item := range jobs {
		if isManaged(item.Metadata.Labels) {
			jobsByName[item.Metadata.Name] = item
		}
	}
	for _, item := range services {
		if isManaged(item.Metadata.Labels) {
			servicesByName[item.Metadata.Name] = item
		}
	}

	var result reconcileResult
	var joined error
	for name, item := range jobsByName {
		if jobFinished(item) || item.Metadata.DeletionTimestamp != nil {
			continue
		}
		service, ok := servicesByName[name]
		if ok && a.validService(service, item) {
			continue
		}
		if ok {
			if err := a.kube.deleteService(ctx, name); err != nil {
				joined = errors.Join(joined, fmt.Errorf("delete invalid Service %s: %w", name, err))
			} else {
				result.ServicesDeleted++
			}
			delete(servicesByName, name)
		}
		if err := a.kube.deleteJob(ctx, name); err != nil {
			joined = errors.Join(joined, fmt.Errorf("delete partial Job %s: %w", name, err))
		} else {
			result.JobsDeleted++
		}
	}
	for name, item := range servicesByName {
		owner, ok := jobsByName[name]
		if ok && a.validService(item, owner) {
			continue
		}
		if err := a.kube.deleteService(ctx, name); err != nil {
			joined = errors.Join(joined, fmt.Errorf("delete orphan Service %s: %w", name, err))
		} else {
			result.ServicesDeleted++
		}
	}
	return result, joined
}

func (a *allocator) ready(ctx context.Context) error {
	_, err := a.kube.listServices(ctx)
	return err
}

func (a *allocator) availablePorts(services []service) []int {
	used := make(map[int]bool)
	for _, item := range services {
		for _, port := range item.Spec.Ports {
			if port.NodePort != 0 {
				used[port.NodePort] = true
			}
		}
	}
	var available []int
	for port := a.cfg.PortFirst; port <= a.cfg.PortLast; port++ {
		if !used[port] {
			available = append(available, port)
		}
	}
	return available
}

func (a *allocator) validService(item service, owner job) bool {
	if item.Metadata.DeletionTimestamp != nil || item.Metadata.Namespace != a.cfg.Workload.Namespace {
		return false
	}
	jobID, jobOK := sessionID(owner.Metadata)
	serviceID, serviceOK := sessionID(item.Metadata)
	if !jobOK || !serviceOK || jobID != serviceID || !ownedByJob(item.Metadata, owner) {
		return false
	}
	if item.Spec.Type != "NodePort" || item.Spec.ExternalTrafficPolicy != "Local" ||
		item.Spec.Selector[labelSession] != jobID {
		return false
	}
	if len(item.Spec.Ports) != 1 {
		return false
	}
	servicePort := item.Spec.Ports[0]
	if servicePort.Name != "game" || servicePort.Port != 7777 ||
		servicePort.TargetPort != "game" || servicePort.Protocol != "TCP" {
		return false
	}
	port := servicePort.NodePort
	return port >= a.cfg.PortFirst && port <= a.cfg.PortLast
}

func (a *allocator) cleanup(created createdObjects) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.CleanupTimeout)
	defer cancel()
	var joined error
	if created.service.Metadata.Name != "" {
		if err := a.kube.deleteService(ctx, created.service.Metadata.Name); err != nil {
			joined = errors.Join(joined, fmt.Errorf("clean up Service: %w", err))
		}
	}
	if created.job.Metadata.Name != "" {
		if err := a.kube.deleteJob(ctx, created.job.Metadata.Name); err != nil {
			joined = errors.Join(joined, fmt.Errorf("clean up Job: %w", err))
		}
	}
	return joined
}

func (a *allocator) sessionRecord(id string, port int, job objectMeta, routable bool, state sessionHealth) session {
	link := func(port int) string {
		return "vif://" + net.JoinHostPort(a.cfg.JoinHost, strconv.Itoa(port)) + "/" + id
	}
	return session{
		ID:           id,
		Port:         port,
		JoinTarget:   link(a.cfg.RoutePort),
		DirectTarget: link(port),
		WebSocketURL: a.webSocketURL(id),
		Scenario:     job.Annotations[annotationScenario],
		Size:         job.Annotations[annotationSize],
		CreatedAt:    job.CreationTimestamp,
		Routable:     routable,
		State:        state,
	}
}

// expiresIn is the sooner of the game's countdown and the Job's wall-clock deadline:
// an occupied game reports none, and activeDeadlineSeconds still ends it.
func expiresIn(reported string, item job, now time.Time) string {
	start, err := time.Parse(time.RFC3339, item.Status.StartTime)
	if err != nil || item.Spec.ActiveDeadlineSeconds <= 0 {
		return reported
	}
	left := max(start.Add(time.Duration(item.Spec.ActiveDeadlineSeconds)*time.Second).Sub(now), 0)
	if game, err := time.ParseDuration(reported); err == nil && game <= left {
		return reported
	}
	return left.Round(time.Second).String()
}

// webSocketURL is derived from the origin the route accepts rather than configured
// beside it: a page handed a URL whose origin this router would refuse is a
// player told to dial a door that will not open.
func (a *allocator) webSocketURL(id string) string {
	if a.cfg.WebOrigin == "" {
		return ""
	}
	authority := strings.TrimPrefix(strings.TrimPrefix(a.cfg.WebOrigin, "https://"), "http://")
	scheme := "wss://"
	if strings.HasPrefix(a.cfg.WebOrigin, "http://") {
		scheme = "ws://"
	}
	return scheme + authority + wsRoutePrefix + id
}

func randomSessionID() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
