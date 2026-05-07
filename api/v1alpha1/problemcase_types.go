// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ProblemCaseSeverity is the severity level of a ProblemCase.
// +kubebuilder:validation:Enum=critical;high;medium;low
type ProblemCaseSeverity string

const (
	SeverityCritical ProblemCaseSeverity = "critical"
	SeverityHigh     ProblemCaseSeverity = "high"
	SeverityMedium   ProblemCaseSeverity = "medium"
	SeverityLow      ProblemCaseSeverity = "low"
)

// ProblemCaseState is the lifecycle state of a ProblemCase.
// +kubebuilder:validation:Enum=open;investigating;resolved
type ProblemCaseState string

const (
	ProblemCaseStateOpen          ProblemCaseState = "open"
	ProblemCaseStateInvestigating ProblemCaseState = "investigating"
	ProblemCaseStateResolved      ProblemCaseState = "resolved"
)

// ResourceRef identifies a Kubernetes resource by namespace, kind, and name.
type ResourceRef struct {
	// Namespace of the resource. Empty for cluster-scoped resources.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Kind is the Kubernetes resource kind (e.g., Pod, Deployment, ScaledObject).
	Kind string `json:"kind"`
	// Name is the resource name.
	Name string `json:"name"`
	// APIGroup is the Kubernetes API group for this resource kind, e.g. "apps" or "keda.sh".
	// Empty means the core API group. Used by the dynamic client in diagnostics-worker
	// to resolve arbitrary CRDs requested by the user or suggested by the LLM.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`
	// EvidenceSlices controls which parts of this resource the diagnostics-worker collects
	// when it appears in RelatedResources. Empty means collect all slices applicable to the
	// kind (backward-compatible default).
	//
	// Valid values:
	//   spec        — resource spec field (with redaction)
	//   status      — full status object
	//   conditions  — status.conditions array, extracted separately for clarity
	//   events      — Kubernetes Events for this resource
	//   logs        — container logs (Deployment, StatefulSet, DaemonSet, Pod, Job only)
	//   metrics     — current CPU/memory from metrics-server (container-having kinds only)
	//   labels      — metadata.labels
	//   annotations — metadata.annotations (sensitive values redacted)
	//   ownerRefs   — metadata.ownerReferences chain resolved recursively up to the root
	// +optional
	EvidenceSlices []string `json:"evidenceSlices,omitempty"`
}

// ProblemCaseSpec defines the desired state of ProblemCase.
type ProblemCaseSpec struct {
	// AffectedResource is the primary Kubernetes resource that triggered this problem.
	AffectedResource ResourceRef `json:"affectedResource"`

	// Detector is the name of the detector that identified this problem.
	Detector string `json:"detector"`

	// Severity is the assessed severity of this problem.
	Severity ProblemCaseSeverity `json:"severity"`

	// Symptoms is a list of human-readable descriptions of the observed symptoms.
	// +optional
	Symptoms []string `json:"symptoms,omitempty"`

	// RelatedResources is a list of additional Kubernetes resources relevant to this problem.
	// +optional
	RelatedResources []ResourceRef `json:"relatedResources,omitempty"`
}

// ProblemCaseStatus defines the observed state of ProblemCase.
type ProblemCaseStatus struct {
	// State is the current lifecycle state of this problem.
	// +kubebuilder:default=open
	State ProblemCaseState `json:"state,omitempty"`

	// FirstSeen is when this problem was first detected.
	// +optional
	FirstSeen *metav1.Time `json:"firstSeen,omitempty"`

	// LastSeen is when symptoms were last observed for this problem.
	// +optional
	LastSeen *metav1.Time `json:"lastSeen,omitempty"`

	// ResolvedAt is when this problem was resolved.
	// +optional
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

	// LatestDiagnosticRunRef is the name of the most recently created DiagnosticRun CRD.
	// +optional
	LatestDiagnosticRunRef string `json:"latestDiagnosticRunRef,omitempty"`

	// LatestAnalysisResultRef is the SQLite analysis_results.id of the most recent analysis.
	// +optional
	LatestAnalysisResultRef string `json:"latestAnalysisResultRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Severity",type=string,JSONPath=".spec.severity"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Detector",type=string,JSONPath=".spec.detector"
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=".spec.affectedResource.name"
// +kubebuilder:printcolumn:name="First Seen",type=date,JSONPath=".status.firstSeen"

// ProblemCase is the Schema for the problemcases API.
// It represents a detected problem in the Kubernetes cluster.
type ProblemCase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProblemCaseSpec   `json:"spec,omitempty"`
	Status ProblemCaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProblemCaseList contains a list of ProblemCase.
type ProblemCaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProblemCase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProblemCase{}, &ProblemCaseList{})
}
