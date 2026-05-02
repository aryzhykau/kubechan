// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// IncidentState is the lifecycle state of an Incident.
// +kubebuilder:validation:Enum=open;resolved
type IncidentState string

const (
	IncidentStateOpen     IncidentState = "open"
	IncidentStateResolved IncidentState = "resolved"
)

// IncidentSpec defines the desired state of Incident.
type IncidentSpec struct {
	// RootResource is the workload root (Deployment, StatefulSet, DaemonSet, etc.)
	// that all member ProblemCases trace back to via owner references.
	RootResource ResourceRef `json:"rootResource"`

	// ProblemCases is the list of ProblemCase names (in the same namespace) grouped
	// into this incident.
	// +optional
	ProblemCases []string `json:"problemCases,omitempty"`
}

// IncidentStatus defines the observed state of Incident.
type IncidentStatus struct {
	// State is the current lifecycle state of this incident.
	// +kubebuilder:default=open
	State IncidentState `json:"state,omitempty"`

	// OpenedAt is when this incident was first opened.
	// +optional
	OpenedAt *metav1.Time `json:"openedAt,omitempty"`

	// ResolvedAt is when all member ProblemCases were resolved.
	// +optional
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

	// ActiveProblemCases is the count of non-resolved ProblemCases in this incident.
	// +optional
	ActiveProblemCases int `json:"activeProblemCases,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Root",type=string,JSONPath=".spec.rootResource.name"
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=".status.activeProblemCases"
// +kubebuilder:printcolumn:name="Opened",type=date,JSONPath=".status.openedAt"

// Incident is the Schema for the incidents API.
// It groups related ProblemCases that share a common workload root.
type Incident struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IncidentSpec   `json:"spec,omitempty"`
	Status IncidentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IncidentList contains a list of Incident.
type IncidentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Incident `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Incident{}, &IncidentList{})
}
