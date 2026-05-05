// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// KubeChanMoodLevel represents KubeChan's current emotional state.
// It is derived from cluster health signals and user interactions.
type KubeChanMoodLevel int32

const (
	// MoodCalm — no open incidents, no poke pressure.
	MoodCalm KubeChanMoodLevel = 0
	// MoodIrritated — 1–2 open incidents or mild poke pressure.
	MoodIrritated KubeChanMoodLevel = 1
	// MoodRage — 3+ open incidents or sustained poking.
	MoodRage KubeChanMoodLevel = 2
)

// KubeChanStateSpec is intentionally empty — KubeChanState is a singleton
// whose only meaningful content is its observed status (mood).
type KubeChanStateSpec struct{}

// KubeChanStateStatus reflects KubeChan's current mood and the signals that drive it.
type KubeChanStateStatus struct {
	// MoodLevel is the computed mood: 0=calm, 1=irritated, 2=rage.
	// +kubebuilder:default=0
	MoodLevel KubeChanMoodLevel `json:"moodLevel"`

	// OpenIncidentCount is the number of currently open incidents.
	OpenIncidentCount int `json:"openIncidentCount"`

	// PokeCount is the number of pokes accumulated in the current streak.
	PokeCount int `json:"pokeCount"`

	// PokeExpiresAt is when the current poke streak expires and resets.
	// +optional
	PokeExpiresAt *metav1.Time `json:"pokeExpiresAt,omitempty"`

	// UpdatedAt is when the mood was last recomputed.
	// +optional
	UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}

// KubeChanState is a singleton CRD that persists KubeChan's current mood.
// There is exactly one instance per control namespace, named "kubechan".
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Mood",type=integer,JSONPath=".status.moodLevel"
// +kubebuilder:printcolumn:name="Open Incidents",type=integer,JSONPath=".status.openIncidentCount"
// +kubebuilder:printcolumn:name="Pokes",type=integer,JSONPath=".status.pokeCount"
// +kubebuilder:printcolumn:name="Updated",type=date,JSONPath=".status.updatedAt"
type KubeChanState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeChanStateSpec   `json:"spec,omitempty"`
	Status KubeChanStateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type KubeChanStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeChanState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeChanState{}, &KubeChanStateList{})
}
