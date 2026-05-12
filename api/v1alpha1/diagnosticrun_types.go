// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DiagnosticRunState is the lifecycle state of a DiagnosticRun.
// +kubebuilder:validation:Enum=pending;running;completed;failed
type DiagnosticRunState string

const (
	DiagnosticRunStatePending   DiagnosticRunState = "pending"
	DiagnosticRunStateRunning   DiagnosticRunState = "running"
	DiagnosticRunStateCompleted DiagnosticRunState = "completed"
	DiagnosticRunStateFailed    DiagnosticRunState = "failed"
)

// DiagnosticRunAnalysisState is the lifecycle state of the LLM analysis phase for a DiagnosticRun.
// +kubebuilder:validation:Enum=not_queued;in_progress;completed;failed
type DiagnosticRunAnalysisState string

const (
	DiagnosticRunAnalysisStateNotQueued  DiagnosticRunAnalysisState = "not_queued"
	DiagnosticRunAnalysisStateInProgress DiagnosticRunAnalysisState = "in_progress"
	DiagnosticRunAnalysisStateCompleted  DiagnosticRunAnalysisState = "completed"
	DiagnosticRunAnalysisStateFailed     DiagnosticRunAnalysisState = "failed"
)

// RedactionSummary contains a summary of the log/evidence redaction process.
type RedactionSummary struct {
	// PatternsApplied is the number of redaction pattern matches found and replaced.
	PatternsApplied int `json:"patternsApplied"`

	// RedactedFields is the list of JSON field paths that contained sensitive data.
	// +optional
	RedactedFields []string `json:"redactedFields,omitempty"`
}

// LogTruncationInfo contains information about log truncation during evidence collection.
type LogTruncationInfo struct {
	// Truncated indicates whether any logs were truncated due to size limits.
	Truncated bool `json:"truncated"`

	// OriginalBytes is the total byte size of all logs before truncation.
	OriginalBytes int64 `json:"originalBytes"`

	// TruncatedBytes is the total byte size of all logs after truncation.
	TruncatedBytes int64 `json:"truncatedBytes"`
}

// DiagnosticRunSpec defines the desired state of DiagnosticRun.
type DiagnosticRunSpec struct {
	// IncidentRef is the name of the Incident this diagnostic run was triggered for.
	// Either IncidentRef or ProblemCaseRef must be set.
	// +optional
	IncidentRef string `json:"incidentRef,omitempty"`

	// ProblemCaseRef is the name of the ProblemCase this diagnostic run belongs to.
	// Deprecated: prefer IncidentRef.
	// +optional
	ProblemCaseRef string `json:"problemCaseRef,omitempty"`

	// RequestedAt is when this diagnostic run was requested.
	RequestedAt *metav1.Time `json:"requestedAt"`
}

// DiagnosticRunStatus defines the observed state of DiagnosticRun.
type DiagnosticRunStatus struct {
	// State is the current lifecycle state of this diagnostic run.
	// +kubebuilder:default=pending
	State DiagnosticRunState `json:"state,omitempty"`

	// CollectedAt is when evidence collection completed.
	// +optional
	CollectedAt *metav1.Time `json:"collectedAt,omitempty"`

	// CollectorVersion is the version string of the diagnostics-worker that ran this collection.
	// +optional
	CollectorVersion string `json:"collectorVersion,omitempty"`

	// EvidenceRef is the SQLite evidence.id for the collected evidence payload.
	// +optional
	EvidenceRef string `json:"evidenceRef,omitempty"`

	// CollectionErrors contains non-fatal errors that occurred during collection.
	// A DiagnosticRun may complete successfully even with collection errors (partial evidence).
	// +optional
	CollectionErrors []string `json:"collectionErrors,omitempty"`

	// RedactionSummary contains a summary of the redaction process applied to the evidence.
	// +optional
	RedactionSummary *RedactionSummary `json:"redactionSummary,omitempty"`

	// LogTruncationInfo contains information about log truncation.
	// +optional
	LogTruncationInfo *LogTruncationInfo `json:"logTruncationInfo,omitempty"`

	// AnalysisState is the current state of the LLM analysis phase for this run.
	// +kubebuilder:default=not_queued
	// +optional
	AnalysisState DiagnosticRunAnalysisState `json:"analysisState,omitempty"`

	// AnalysisResultRef is the analysis_results.id for the completed LLM analysis.
	// +optional
	AnalysisResultRef string `json:"analysisResultRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Analysis",type=string,JSONPath=".status.analysisState"
// +kubebuilder:printcolumn:name="Problem Case",type=string,JSONPath=".spec.problemCaseRef"
// +kubebuilder:printcolumn:name="Requested",type=date,JSONPath=".spec.requestedAt"
// +kubebuilder:printcolumn:name="Collected",type=date,JSONPath=".status.collectedAt"

// DiagnosticRun is the Schema for the diagnosticruns API.
// It represents a single evidence collection run triggered for a ProblemCase.
type DiagnosticRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DiagnosticRunSpec   `json:"spec,omitempty"`
	Status DiagnosticRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DiagnosticRunList contains a list of DiagnosticRun.
type DiagnosticRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DiagnosticRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DiagnosticRun{}, &DiagnosticRunList{})
}
