// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package collector

import "time"

// ResourceRef identifies a Kubernetes resource.
type ResourceRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// K8sEvent is a simplified Kubernetes event.
type K8sEvent struct {
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	FirstTime time.Time `json:"firstTime"`
	LastTime  time.Time `json:"lastTime"`
}

// ConfigMapRef records a ConfigMap dependency with its keys/data and where it is mounted.
type ConfigMapRef struct {
	Name       string            `json:"name"`
	Missing    bool              `json:"missing,omitempty"`
	Keys       []string          `json:"keys,omitempty"`       // all keys present in the ConfigMap
	Data       map[string]string `json:"data,omitempty"`       // values (truncated to 1 KB each)
	MountPaths []string          `json:"mountPaths,omitempty"` // volume-mount paths in containers
}

// SecretRef records a Secret dependency and whether it is missing.
// Secret content is never included.
type SecretRef struct {
	Name    string `json:"name"`
	Missing bool   `json:"missing,omitempty"`
}

// PodDependencies lists the ConfigMaps and Secrets a pod references.
type PodDependencies struct {
	ConfigMaps []ConfigMapRef `json:"configMaps,omitempty"`
	Secrets    []SecretRef    `json:"secrets,omitempty"`
}

// PodLogs holds log output, events, and dependency info for a single pod.
type PodLogs struct {
	PodName      string           `json:"podName"`
	Phase        string           `json:"phase"`
	Logs         string           `json:"logs,omitempty"`
	Truncated    bool             `json:"truncated,omitempty"`
	Error        string           `json:"error,omitempty"`
	Events       []K8sEvent       `json:"events,omitempty"`
	Dependencies *PodDependencies `json:"dependencies,omitempty"`
}

// PVCInfo holds the status and events for a PersistentVolumeClaim.
type PVCInfo struct {
	Name             string     `json:"name"`
	Phase            string     `json:"phase"`
	StorageClass     string     `json:"storageClass,omitempty"`
	RequestedStorage string     `json:"requestedStorage,omitempty"`
	Events           []K8sEvent `json:"events,omitempty"`
}

// ProblemCaseEvidence holds collected data for a single ProblemCase.
type ProblemCaseEvidence struct {
	Name             string      `json:"name"`
	Detector         string      `json:"detector"`
	Severity         string      `json:"severity"`
	Symptoms         []string    `json:"symptoms,omitempty"`
	AffectedResource ResourceRef `json:"affectedResource"`
	Events           []K8sEvent  `json:"events,omitempty"`
	Logs             string      `json:"logs,omitempty"`
	LogsTruncated    bool        `json:"logsTruncated,omitempty"`
}

// Evidence is the full evidence payload posted to /internal/evidence.
type Evidence struct {
	DiagnosticRunID    string                `json:"diagnosticRunId"`
	IncidentID         string                `json:"incidentId"`
	CollectedAt        time.Time             `json:"collectedAt"`
	RootResource       ResourceRef           `json:"rootResource"`
	RootResourceEvents []K8sEvent            `json:"rootResourceEvents,omitempty"`
	ProblemCases       []ProblemCaseEvidence `json:"problemCases,omitempty"`
	WorkloadPodLogs    []PodLogs             `json:"workloadPodLogs,omitempty"`
	PVCInfos           []PVCInfo             `json:"pvcInfos,omitempty"`
	// UserMessage is the plain-text description provided by the user for manual incidents.
	UserMessage string `json:"userMessage,omitempty"`
	// RelatedResourceEvidence holds events (and logs) for user-tagged related resources.
	RelatedResourceEvidence []RelatedResourceEvidence `json:"relatedResourceEvidence,omitempty"`
}

// RelatedResourceEvidence holds collected events and spec data for a user-tagged resource.
type RelatedResourceEvidence struct {
	Resource ResourceRef    `json:"resource"`
	Events   []K8sEvent     `json:"events,omitempty"`
	Logs     string         `json:"logs,omitempty"`
	// Spec holds kind-specific diagnostic fields extracted from the resource spec/status
	// (e.g. Ingress backend service names, Service selector, ConfigMap data, etc.).
	Spec     map[string]any `json:"spec,omitempty"`
}
