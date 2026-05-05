// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package ws

import "encoding/json"

// Event type constants — matched by the frontend useWebSocket hook.
const (
	EventProblemCaseCreated        = "ProblemCase.Created"
	EventProblemCaseUpdated        = "ProblemCase.Updated"
	EventProblemCaseResolved       = "ProblemCase.Resolved"
	EventIncidentCreated           = "Incident.Created"
	EventIncidentUpdated           = "Incident.Updated"
	EventIncidentResolved          = "Incident.Resolved"
	EventDiagnosticRunStateChanged = "DiagnosticRun.StateChanged"
	EventAnalysisResultCompleted   = "AnalysisResult.Completed"
	EventAnalysisResultFailed      = "AnalysisResult.Failed"
	EventKubeChanStateUpdated      = "KubeChanState.Updated"
)

// BaseEvent is embedded in every WS event.
type BaseEvent struct {
	Type string `json:"type"`
}

// ProblemCaseEvent carries minimal data for problem case lifecycle events.
type ProblemCaseEvent struct {
	BaseEvent
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Severity  string `json:"severity"`
	State     string `json:"state"`
	Detector  string `json:"detector"`
}

// IncidentEvent carries minimal data for incident lifecycle events.
type IncidentEvent struct {
	BaseEvent
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	State              string `json:"state"`
	RootResourceKind   string `json:"rootResourceKind"`
	RootResourceName   string `json:"rootResourceName"`
	ActiveProblemCases int    `json:"activeProblemCases"`
}

// DiagnosticRunEvent carries state-change data.
type DiagnosticRunEvent struct {
	BaseEvent
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	State           string `json:"state"`
	IncidentRef     string `json:"incidentRef,omitempty"`
}

// AnalysisResultEvent carries analysis completion data.
type AnalysisResultEvent struct {
	BaseEvent
	ID          string `json:"id"`
	IncidentID  string `json:"incidentId"`
	Status      string `json:"status"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// Marshal serialises an event to JSON for broadcasting.
func Marshal(event any) []byte {
	b, _ := json.Marshal(event)
	return b
}

// KubeChanStateEvent is broadcast when KubeChan's mood changes.
type KubeChanStateEvent struct {
	BaseEvent
	MoodLevel         int `json:"moodLevel"`
	OpenIncidentCount int `json:"openIncidentCount"`
	PokeCount         int `json:"pokeCount"`
}
