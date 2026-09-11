package main

import "strings"

const (
	applicationName = "vi-fighter"
	componentName   = "session"
	partOfName      = "vi-fighter-fleet"
	sessionPrefix   = "vif-session-"

	labelApplication = "app.kubernetes.io/name"
	labelComponent   = "app.kubernetes.io/component"
	labelPartOf      = "app.kubernetes.io/part-of"
	labelSession     = "vif.lixenwraith.dev/session"
)

type objectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	DeletionTimestamp *string           `json:"deletionTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	OwnerReferences   []ownerReference  `json:"ownerReferences,omitempty"`
}

type ownerReference struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Controller         *bool  `json:"controller,omitempty"`
	BlockOwnerDeletion *bool  `json:"blockOwnerDeletion,omitempty"`
}

type condition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type job struct {
	Metadata objectMeta `json:"metadata"`
	Status   jobStatus  `json:"status"`
}

type jobStatus struct {
	Active         int         `json:"active,omitempty"`
	Succeeded      int         `json:"succeeded,omitempty"`
	Failed         int         `json:"failed,omitempty"`
	CompletionTime *string     `json:"completionTime,omitempty"`
	Conditions     []condition `json:"conditions,omitempty"`
}

type service struct {
	Metadata objectMeta  `json:"metadata"`
	Spec     serviceSpec `json:"spec"`
}

type serviceSpec struct {
	Type                  string            `json:"type,omitempty"`
	Selector              map[string]string `json:"selector,omitempty"`
	ExternalTrafficPolicy string            `json:"externalTrafficPolicy,omitempty"`
	Ports                 []servicePort     `json:"ports"`
}

type servicePort struct {
	Name       string `json:"name,omitempty"`
	Port       int    `json:"port,omitempty"`
	TargetPort any    `json:"targetPort,omitempty"`
	NodePort   int    `json:"nodePort,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
}

type pod struct {
	Metadata objectMeta `json:"metadata"`
	Status   podStatus  `json:"status"`
}

type podStatus struct {
	Phase      string      `json:"phase,omitempty"`
	PodIP      string      `json:"podIP,omitempty"`
	Conditions []condition `json:"conditions,omitempty"`
}

type endpointSlice struct {
	Metadata  objectMeta      `json:"metadata"`
	Endpoints []sliceEndpoint `json:"endpoints"`
}

type sliceEndpoint struct {
	Addresses  []string           `json:"addresses"`
	Conditions endpointConditions `json:"conditions"`
}

type endpointConditions struct {
	Ready       *bool `json:"ready,omitempty"`
	Serving     *bool `json:"serving,omitempty"`
	Terminating *bool `json:"terminating,omitempty"`
}

type kubeList[T any] struct {
	Items []T `json:"items"`
}

func sessionLabels(id string) map[string]string {
	return map[string]string{
		labelApplication: applicationName,
		labelComponent:   componentName,
		labelPartOf:      partOfName,
		labelSession:     id,
	}
}

func isManaged(labels map[string]string) bool {
	return labels[labelApplication] == applicationName &&
		labels[labelComponent] == componentName &&
		labels[labelPartOf] == partOfName
}

func sessionID(meta objectMeta) (string, bool) {
	id := meta.Labels[labelSession]
	if id == "" || meta.Name != sessionPrefix+id {
		return "", false
	}
	return id, true
}

func jobFinished(item job) bool {
	if item.Status.CompletionTime != nil || item.Status.Succeeded > 0 || item.Status.Failed > 0 {
		return true
	}
	for _, c := range item.Status.Conditions {
		if (c.Type == "Complete" || c.Type == "Failed") && strings.EqualFold(c.Status, "true") {
			return true
		}
	}
	return false
}

func ownedByJob(meta objectMeta, owner job) bool {
	for _, ref := range meta.OwnerReferences {
		if ref.APIVersion == "batch/v1" && ref.Kind == "Job" &&
			ref.Name == owner.Metadata.Name && ref.UID == owner.Metadata.UID &&
			ref.Controller != nil && *ref.Controller &&
			ref.BlockOwnerDeletion != nil && *ref.BlockOwnerDeletion {
			return true
		}
	}
	return false
}

func firstNodePort(item service) (int, bool) {
	for _, port := range item.Spec.Ports {
		if port.NodePort != 0 {
			return port.NodePort, true
		}
	}
	return 0, false
}

func podReady(item pod) bool {
	if item.Metadata.DeletionTimestamp != nil || item.Status.Phase != "Running" || item.Status.PodIP == "" {
		return false
	}
	for _, c := range item.Status.Conditions {
		if c.Type == "Ready" {
			return strings.EqualFold(c.Status, "true")
		}
	}
	return false
}

func readyAddresses(items []endpointSlice) map[string]bool {
	result := make(map[string]bool)
	for _, slice := range items {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				continue
			}
			for _, address := range endpoint.Addresses {
				result[address] = true
			}
		}
	}
	return result
}

func readyAddressesByService(items []endpointSlice) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	for _, slice := range items {
		serviceName := slice.Metadata.Labels["kubernetes.io/service-name"]
		if serviceName == "" {
			continue
		}
		addresses := result[serviceName]
		if addresses == nil {
			addresses = make(map[string]bool)
			result[serviceName] = addresses
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				continue
			}
			for _, address := range endpoint.Addresses {
				addresses[address] = true
			}
		}
	}
	return result
}
