package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

	"github.com/lixenwraith/vi-fighter/internal/paths"
)

var (
	errFleetFull         = errors.New("all session ports are allocated")
	errSessionNotReady   = errors.New("session did not become ready")
	errRequestRefused    = errors.New("session request refused")
	errSessionUnknown    = errors.New("no such session")
	errSessionRefusing   = errors.New("session is not accepting players")
	errSessionUnroutable = errors.New("session has no reachable pod")
)

type allocatorConfig struct {
	Workload   workloadConfig
	JoinHost   string
	PageBase   string
	PortFirst  int
	PortLast   int
	PlayersMax int
	LogLevels  []string
	WadDir     string

	// WebOrigin is the one origin the browser route answers, and publishing it is
	// what turns that route on. The sidecar that serves it is Workload.BridgeImage;
	// see doc/kubernetes-fleet.md §9.
	WebOrigin        string
	WebMaxPerSession int

	ReadyTimeout   time.Duration
	PollInterval   time.Duration
	CleanupTimeout time.Duration
}

// sessionRequest is what a caller may choose about the session it is asking for.
// Every field is optional and nothing else in the workload is selectable: the
// image, the map size, the lifetime bounds, the mounts and the environment are
// the deployment's.
type sessionRequest struct {
	Players  int    `json:"players,omitempty"`
	LogLevel string `json:"log_level,omitempty"`
	Scenario string `json:"scenario,omitempty"`
}

// fleetLimits is what the deployment will accept, so a caller can offer only the
// choices this allocator would allow rather than discovering them by refusal.
type fleetLimits struct {
	PlayersMax int      `json:"players_max"`
	LogLevels  []string `json:"log_levels"`
	Scenarios  []string `json:"scenarios"`
}

type session struct {
	ID         string `json:"id"`
	Port       int    `json:"port"`
	PageURL    string `json:"page_url"`
	JoinTarget string `json:"join_target"`
	// WebSocketURL is the same session reached from a browser. Absent where the
	// deployment publishes no browser route, which is what a page keyed on it reads
	// as "this fleet is for native clients".
	WebSocketURL string        `json:"ws_url,omitempty"`
	CreatedAt    string        `json:"created_at,omitempty"`
	Routable     bool          `json:"routable"`
	State        sessionHealth `json:"state"`
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
	id        string
	port      int
	job       job
	service   service
	createdAt string
}

func newAllocator(kube kubeAPI, health healthProbe, cfg allocatorConfig) *allocator {
	return &allocator{kube: kube, health: health, cfg: cfg, newID: randomSessionID}
}

func (a *allocator) limits() fleetLimits {
	return fleetLimits{PlayersMax: a.cfg.PlayersMax, LogLevels: a.cfg.LogLevels,
		Scenarios: a.scenarios()}
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

// resolve folds a caller's choices into this deployment's workload. An omitted
// choice keeps the default; one outside what the operator opened is refused rather
// than clamped, because a session quietly given a different roster than the one it
// asked for is worse than a request the caller can correct.
func (a *allocator) resolve(req sessionRequest) (workloadConfig, error) {
	workload := a.cfg.Workload
	if req.Players != 0 {
		if req.Players < 1 || req.Players > a.cfg.PlayersMax {
			return workloadConfig{}, fmt.Errorf("%w: players must be between 1 and %d",
				errRequestRefused, a.cfg.PlayersMax)
		}
		workload.Players = req.Players
	}
	if req.LogLevel != "" {
		if !slices.Contains(a.cfg.LogLevels, req.LogLevel) {
			return workloadConfig{}, fmt.Errorf("%w: log_level must be one of %s",
				errRequestRefused, strings.Join(a.cfg.LogLevels, ", "))
		}
		workload.LogLevel = req.LogLevel
	}
	if req.Scenario != "" {
		installed := a.scenarios()
		if !slices.Contains(installed, req.Scenario) {
			return workloadConfig{}, fmt.Errorf("%w: scenario must be one of %s",
				errRequestRefused, strings.Join(installed, ", "))
		}
		workload.Scenario = req.Scenario
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

	return a.sessionRecord(created.id, created.port, created.createdAt, true, state), nil
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

	created := createdObjects{id: id, job: createdJob, createdAt: createdJob.Metadata.CreationTimestamp}
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
		createdAt   string
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
			createdAt:   item.Metadata.CreationTimestamp,
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
					break
				}
			}
			result[i] = a.sessionRecord(item.id, item.port, item.createdAt, routable, state)
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
	for _, item := range pods {
		if !isManaged(item.Metadata.Labels) || !podReady(item) || !addresses[item.Status.PodIP] {
			continue
		}
		state, err := a.health.probe(ctx, item.Status.PodIP)
		if err != nil {
			return "", fmt.Errorf("read pod health: %w", err)
		}
		if !state.Live {
			return "", errSessionUnroutable
		}
		// The same answer the create path waits for. A session at capacity or
		// draining refuses the dial itself; refusing here costs it nothing and
		// tells the caller which of the two it is.
		if !state.Ready {
			return "", fmt.Errorf("%w: %s", errSessionRefusing, state.Phase)
		}
		return item.Status.PodIP, nil
	}
	if len(pods) == 0 {
		return "", errSessionUnknown
	}
	return "", errSessionUnroutable
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

func (a *allocator) sessionRecord(id string, port int, createdAt string, routable bool, state sessionHealth) session {
	pageBase := strings.TrimRight(a.cfg.PageBase, "/")
	return session{
		ID:           id,
		Port:         port,
		PageURL:      pageBase + "/" + strconv.Itoa(port) + "/",
		JoinTarget:   net.JoinHostPort(a.cfg.JoinHost, strconv.Itoa(port)),
		WebSocketURL: a.webSocketURL(id),
		CreatedAt:    createdAt,
		Routable:     routable,
		State:        state,
	}
}

// webSocketURL is derived from the origin the route accepts rather than configured
// beside it: a page handed a URL whose origin this allocator would refuse is a
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
