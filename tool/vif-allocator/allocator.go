package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	errFleetFull       = errors.New("all session ports are allocated")
	errSessionNotReady = errors.New("session did not become ready")
)

type allocatorConfig struct {
	Workload       workloadConfig
	JoinHost       string
	PageBase       string
	PortFirst      int
	PortLast       int
	ReadyTimeout   time.Duration
	PollInterval   time.Duration
	CleanupTimeout time.Duration
}

type session struct {
	ID         string        `json:"id"`
	Port       int           `json:"port"`
	PageURL    string        `json:"page_url"`
	JoinTarget string        `json:"join_target"`
	CreatedAt  string        `json:"created_at,omitempty"`
	Routable   bool          `json:"routable"`
	State      sessionHealth `json:"state"`
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

func (a *allocator) createSession(ctx context.Context) (session, error) {
	created, err := a.createTransaction(ctx)
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

func (a *allocator) createTransaction(ctx context.Context) (createdObjects, error) {
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
		createdJob, err = a.kube.createJob(ctx, buildJob(id, a.cfg.Workload))
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
		ID:         id,
		Port:       port,
		PageURL:    pageBase + "/" + strconv.Itoa(port) + "/",
		JoinTarget: net.JoinHostPort(a.cfg.JoinHost, strconv.Itoa(port)),
		CreatedAt:  createdAt,
		Routable:   routable,
		State:      state,
	}
}

func randomSessionID() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
