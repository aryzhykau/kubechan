// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ExclusionRuleSpec defines the desired state of KubechanExclusionRule.
type ExclusionRuleSpec struct {
	// Description is a human-readable explanation of why this rule exists.
	// +optional
	Description string `json:"description,omitempty"`

	// Enabled controls whether this rule is actively enforced.
	// Defaults to true.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Namespace optionally scopes the entire rule to a single namespace.
	// When set, the rule only fires if the affected resource lives in this namespace.
	// Acts as a fast pre-filter before TargetResources / Selector are evaluated.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Detectors lists the detector names to suppress.
	// Empty slice means suppress ALL detectors for matched resources.
	// +optional
	Detectors []string `json:"detectors,omitempty"`

	// TargetResources is a list of exact resource references to match.
	// Can be used together with Selector — a resource matching EITHER is suppressed.
	// +optional
	TargetResources []ResourceRef `json:"targetResources,omitempty"`

	// Selector matches resources by label selector, optionally filtered by namespace and kinds.
	// Can be used together with TargetResources — a resource matching EITHER is suppressed.
	// +optional
	Selector *ExclusionSelector `json:"selector,omitempty"`

	// TimeWindow restricts the rule to specific recurring time periods.
	// Absent means the rule applies 24/7.
	// +optional
	TimeWindow *ExclusionTimeWindow `json:"timeWindow,omitempty"`
}

// ExclusionSelector matches resources by namespace, kind, and/or label selector.
type ExclusionSelector struct {
	// Namespace further narrows the selector to a specific namespace.
	// If ExclusionRuleSpec.Namespace is also set, both must agree.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Kinds restricts matching to specific resource kinds (e.g. ["Deployment", "Service"]).
	// Empty means any kind.
	// +optional
	Kinds []string `json:"kinds,omitempty"`

	// MatchLabels requires all listed key=value label pairs to be present on the resource.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// ExclusionTimeWindow defines a recurring schedule during which the rule is active.
type ExclusionTimeWindow struct {
	// Timezone is an IANA timezone name, e.g. "Europe/Berlin". Defaults to UTC.
	// +kubebuilder:default=UTC
	Timezone string `json:"timezone"`

	// Periods is the list of recurring time intervals during which the rule fires.
	Periods []ExclusionPeriod `json:"periods"`
}

// ExclusionPeriod is a single recurring time slot within a week.
type ExclusionPeriod struct {
	// Start is the period start time in "HH:MM" 24-hour format.
	Start string `json:"start"`
	// End is the period end time in "HH:MM" 24-hour format.
	// End < Start is valid and means the period spans midnight.
	End string `json:"end"`
	// Days lists the days of week this period applies to.
	// Valid values: Mon, Tue, Wed, Thu, Fri, Sat, Sun.
	Days []string `json:"days"`
}

// ExclusionRuleStatus defines the observed state of KubechanExclusionRule.
type ExclusionRuleStatus struct {
	// LastMatchedAt records when this rule last suppressed a ProblemCase creation.
	// +optional
	LastMatchedAt *metav1.Time `json:"lastMatchedAt,omitempty"`

	// SuppressedCount is the lifetime count of ProblemCase creations suppressed by this rule.
	// +optional
	SuppressedCount int `json:"suppressedCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=".spec.enabled"
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="Suppressed",type=integer,JSONPath=".status.suppressedCount"
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=".spec.description"

// KubechanExclusionRule suppresses specific KubeChan detectors for matched resources,
// optionally within a recurring time window.
type KubechanExclusionRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExclusionRuleSpec   `json:"spec,omitempty"`
	Status ExclusionRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubechanExclusionRuleList contains a list of KubechanExclusionRule.
type KubechanExclusionRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubechanExclusionRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubechanExclusionRule{}, &KubechanExclusionRuleList{})
}
