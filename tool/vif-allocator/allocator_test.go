package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeKube struct {
	mu sync.Mutex

	jobs       []job
	services   []service
	pods       []pod
	slices     []endpointSlice
	jobObjects []map[string]any
	svcObjects []map[string]any

	createJobErr     error
	createServiceErr error
	deletedJobs      []string
	deletedServices  []string
}

func (f *fakeKube) listJobs(context.Context) ([]job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]job(nil), f.jobs...), nil
}

func (f *fakeKube) listServices(context.Context) ([]service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]service(nil), f.services...), nil
}

func (f *fakeKube) listPods(context.Context, string) ([]pod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pod(nil), f.pods...), nil
}

func (f *fakeKube) listEndpointSlices(context.Context, string) ([]endpointSlice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]endpointSlice(nil), f.slices...), nil
}

func (f *fakeKube) createJob(_ context.Context, object map[string]any) (job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createJobErr != nil {
		return job{}, f.createJobErr
	}
	meta := object["metadata"].(map[string]any)
	name := meta["name"].(string)
	created := job{Metadata: objectMeta{
		Name:              name,
		Namespace:         meta["namespace"].(string),
		UID:               "uid-" + name,
		CreationTimestamp: "2026-09-11T12:00:00Z",
		Labels:            meta["labels"].(map[string]string),
	}}
	f.jobObjects = append(f.jobObjects, object)
	f.jobs = append(f.jobs, created)
	return created, nil
}

func (f *fakeKube) createService(_ context.Context, object map[string]any) (service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createServiceErr != nil {
		return service{}, f.createServiceErr
	}
	meta := object["metadata"].(map[string]any)
	spec := object["spec"].(map[string]any)
	portObject := spec["ports"].([]any)[0].(map[string]any)
	ownerObject := meta["ownerReferences"].([]any)[0].(map[string]any)
	name := meta["name"].(string)
	id := meta["labels"].(map[string]string)[labelSession]
	truth := true
	created := service{
		Metadata: objectMeta{
			Name:      name,
			Namespace: meta["namespace"].(string),
			Labels:    meta["labels"].(map[string]string),
			OwnerReferences: []ownerReference{{
				APIVersion:         ownerObject["apiVersion"].(string),
				Kind:               ownerObject["kind"].(string),
				Name:               ownerObject["name"].(string),
				UID:                ownerObject["uid"].(string),
				Controller:         &truth,
				BlockOwnerDeletion: &truth,
			}},
		},
		Spec: serviceSpec{
			Type:                  spec["type"].(string),
			Selector:              spec["selector"].(map[string]string),
			ExternalTrafficPolicy: spec["externalTrafficPolicy"].(string),
			Ports: []servicePort{{
				Name:       portObject["name"].(string),
				Port:       portObject["port"].(int),
				TargetPort: portObject["targetPort"],
				NodePort:   portObject["nodePort"].(int),
				Protocol:   portObject["protocol"].(string),
			}},
		},
	}
	f.svcObjects = append(f.svcObjects, object)
	f.services = append(f.services, created)
	f.pods = append(f.pods, pod{
		Metadata: objectMeta{Name: name + "-pod", Labels: sessionLabels(id)},
		Status: podStatus{
			Phase:      "Running",
			PodIP:      "10.42.0.20",
			Conditions: []condition{{Type: "Ready", Status: "True"}},
		},
	})
	f.slices = append(f.slices, endpointSlice{
		Metadata: objectMeta{Name: name + "-slice", Labels: map[string]string{
			"kubernetes.io/service-name": name,
		}},
		Endpoints: []sliceEndpoint{{
			Addresses:  []string{"10.42.0.20"},
			Conditions: endpointConditions{Ready: &truth},
		}},
	})
	return created, nil
}

func (f *fakeKube) deleteJob(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedJobs = append(f.deletedJobs, name)
	for i, item := range f.jobs {
		if item.Metadata.Name == name {
			f.jobs = append(f.jobs[:i], f.jobs[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeKube) deleteService(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedServices = append(f.deletedServices, name)
	for i, item := range f.services {
		if item.Metadata.Name == name {
			f.services = append(f.services[:i], f.services[i+1:]...)
			break
		}
	}
	return nil
}

type fakeHealth struct {
	state sessionHealth
	err   error
}

func (f fakeHealth) probe(context.Context, string) (sessionHealth, error) {
	return f.state, f.err
}

func testAllocatorConfig() allocatorConfig {
	return allocatorConfig{
		Workload: workloadConfig{
			Namespace: "vif",
			Image:     "docker.io/library/vi-fighter:test",
			Players:   4,
			MapSize:   "120x40",
			FirstJoin: "90s",
			Empty:     "90s",
			Drain:     "20s",
		},
		JoinHost:       "play.example.com",
		PageBase:       "https://play.example.com/projects/vi-fighter/session/",
		PortFirst:      31700,
		PortLast:       31709,
		ReadyTimeout:   100 * time.Millisecond,
		PollInterval:   time.Millisecond,
		CleanupTimeout: 100 * time.Millisecond,
	}
}

func TestCreateSessionReservesPortAndOwnsService(t *testing.T) {
	kube := &fakeKube{
		services: []service{{Spec: serviceSpec{Ports: []servicePort{{NodePort: 31700}}}}},
	}
	controller := newAllocator(kube, fakeHealth{state: sessionHealth{
		Live: true, Ready: true, Capacity: 4, Phase: "waiting",
	}}, testAllocatorConfig())
	controller.newID = func() (string, error) { return "abc123", nil }

	created, err := controller.createSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "abc123" || created.Port != 31701 || !created.Routable {
		t.Fatalf("unexpected session: %+v", created)
	}
	if created.JoinTarget != "play.example.com:31701" ||
		created.PageURL != "https://play.example.com/projects/vi-fighter/session/31701/" {
		t.Fatalf("unexpected links: %+v", created)
	}
	if len(kube.svcObjects) != 1 {
		t.Fatalf("created %d Services, want 1", len(kube.svcObjects))
	}
	meta := kube.svcObjects[0]["metadata"].(map[string]any)
	owner := meta["ownerReferences"].([]any)[0].(map[string]any)
	if owner["uid"] != "uid-vif-session-abc123" || owner["blockOwnerDeletion"] != true {
		t.Fatalf("unexpected owner reference: %#v", owner)
	}
}

func TestCreateSessionRefusesFullFleetBeforeCreatingJob(t *testing.T) {
	kube := &fakeKube{}
	for port := 31700; port <= 31709; port++ {
		kube.services = append(kube.services, service{Spec: serviceSpec{Ports: []servicePort{{NodePort: port}}}})
	}
	controller := newAllocator(kube, fakeHealth{}, testAllocatorConfig())

	_, err := controller.createSession(context.Background())
	if !errors.Is(err, errFleetFull) {
		t.Fatalf("got %v, want fleet full", err)
	}
	if len(kube.jobObjects) != 0 {
		t.Fatal("allocator created a Job for a full fleet")
	}
}

func TestCreateSessionRollsBackJobWhenServiceFails(t *testing.T) {
	kube := &fakeKube{createServiceErr: fmt.Errorf("admission failed")}
	controller := newAllocator(kube, fakeHealth{}, testAllocatorConfig())
	controller.newID = func() (string, error) { return "rollback", nil }

	if _, err := controller.createSession(context.Background()); err == nil {
		t.Fatal("expected Service failure")
	}
	if len(kube.deletedJobs) != 1 || kube.deletedJobs[0] != "vif-session-rollback" {
		t.Fatalf("deleted Jobs = %v", kube.deletedJobs)
	}
}

func TestCreateSessionCancellationRollsBackWithoutBecomingReadinessFailure(t *testing.T) {
	kube := &fakeKube{}
	controller := newAllocator(kube, fakeHealth{state: sessionHealth{Live: true}}, testAllocatorConfig())
	controller.newID = func() (string, error) { return "canceled", nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := controller.createSession(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, errSessionNotReady) {
		t.Fatalf("error = %v, want context cancellation only", err)
	}
	if len(kube.deletedServices) != 1 || len(kube.deletedJobs) != 1 {
		t.Fatalf("cleanup deleted Services=%v Jobs=%v", kube.deletedServices, kube.deletedJobs)
	}
}

func TestReconcileDeletesPartialAndOrphanObjects(t *testing.T) {
	truth := true
	validJob := job{Metadata: objectMeta{
		Name: "vif-session-valid", Namespace: "vif", UID: "uid-valid", Labels: sessionLabels("valid"),
	}}
	kube := &fakeKube{
		jobs: []job{
			validJob,
			{Metadata: objectMeta{Name: "vif-session-partial", UID: "uid-partial", Labels: sessionLabels("partial")}},
		},
		services: []service{
			{Metadata: objectMeta{
				Name: "vif-session-valid", Namespace: "vif", Labels: sessionLabels("valid"),
				OwnerReferences: []ownerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "vif-session-valid", UID: "uid-valid", Controller: &truth, BlockOwnerDeletion: &truth}},
			}, Spec: serviceSpec{
				Type:                  "NodePort",
				Selector:              map[string]string{labelSession: "valid"},
				ExternalTrafficPolicy: "Local",
				Ports: []servicePort{{
					Name: "game", Port: 7777, TargetPort: "game", NodePort: 31700, Protocol: "TCP",
				}},
			}},
			{Metadata: objectMeta{Name: "vif-session-orphan", Labels: sessionLabels("orphan")}},
		},
	}
	controller := newAllocator(kube, fakeHealth{}, testAllocatorConfig())

	result, err := controller.reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.JobsDeleted != 1 || result.ServicesDeleted != 1 {
		t.Fatalf("unexpected reconciliation result: %+v", result)
	}
	if len(kube.deletedJobs) != 1 || kube.deletedJobs[0] != "vif-session-partial" {
		t.Fatalf("deleted Jobs = %v", kube.deletedJobs)
	}
	if len(kube.deletedServices) != 1 || kube.deletedServices[0] != "vif-session-orphan" {
		t.Fatalf("deleted Services = %v", kube.deletedServices)
	}
}

func TestReadyAddressesStayScopedToTheirService(t *testing.T) {
	ready := true
	items := []endpointSlice{
		{
			Metadata: objectMeta{Labels: map[string]string{
				"kubernetes.io/service-name": "vif-session-one",
			}},
			Endpoints: []sliceEndpoint{{
				Addresses:  []string{"10.42.0.20"},
				Conditions: endpointConditions{Ready: &ready},
			}},
		},
		{
			Metadata: objectMeta{Labels: map[string]string{
				"kubernetes.io/service-name": "vif-session-two",
			}},
			Endpoints: []sliceEndpoint{{
				Addresses:  []string{"10.42.0.21"},
				Conditions: endpointConditions{Ready: &ready},
			}},
		},
	}

	addresses := readyAddressesByService(items)
	if !addresses["vif-session-one"]["10.42.0.20"] || addresses["vif-session-one"]["10.42.0.21"] {
		t.Fatalf("unexpected first Service addresses: %v", addresses["vif-session-one"])
	}
	if !addresses["vif-session-two"]["10.42.0.21"] || addresses["vif-session-two"]["10.42.0.20"] {
		t.Fatalf("unexpected second Service addresses: %v", addresses["vif-session-two"])
	}
}
