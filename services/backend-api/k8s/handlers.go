// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"log/slog"

	"k8s.io/client-go/tools/cache"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	kubews "github.com/org/kubechan/services/backend-api/ws"
)

// --- ProblemCase handler ---

type problemCaseHandler struct {
	hub    *kubews.Hub
	logger *slog.Logger
}

func newProblemCaseHandler(hub *kubews.Hub, logger *slog.Logger) cache.ResourceEventHandler {
	return &problemCaseHandler{hub: hub, logger: logger}
}

func (h *problemCaseHandler) OnAdd(obj any, _ bool) {
	pc, ok := obj.(*v1alpha1.ProblemCase)
	if !ok {
		return
	}
	h.hub.Broadcast(kubews.Marshal(kubews.ProblemCaseEvent{
		BaseEvent: kubews.BaseEvent{Type: kubews.EventProblemCaseCreated},
		Namespace: pc.Namespace,
		Name:      pc.Name,
		Severity:  string(pc.Spec.Severity),
		State:     string(pc.Status.State),
		Detector:  pc.Spec.Detector,
	}))
}

func (h *problemCaseHandler) OnUpdate(_, newObj any) {
	pc, ok := newObj.(*v1alpha1.ProblemCase)
	if !ok {
		return
	}
	evType := kubews.EventProblemCaseUpdated
	if pc.Status.State == v1alpha1.ProblemCaseStateResolved {
		evType = kubews.EventProblemCaseResolved
	}
	h.hub.Broadcast(kubews.Marshal(kubews.ProblemCaseEvent{
		BaseEvent: kubews.BaseEvent{Type: evType},
		Namespace: pc.Namespace,
		Name:      pc.Name,
		Severity:  string(pc.Spec.Severity),
		State:     string(pc.Status.State),
		Detector:  pc.Spec.Detector,
	}))
}

func (h *problemCaseHandler) OnDelete(obj any) {
	pc, ok := obj.(*v1alpha1.ProblemCase)
	if !ok {
		return
	}
	h.hub.Broadcast(kubews.Marshal(kubews.ProblemCaseEvent{
		BaseEvent: kubews.BaseEvent{Type: kubews.EventProblemCaseResolved},
		Namespace: pc.Namespace,
		Name:      pc.Name,
		Severity:  string(pc.Spec.Severity),
		State:     "resolved",
		Detector:  pc.Spec.Detector,
	}))
}

// --- Incident handler ---

type incidentHandler struct {
	hub        *kubews.Hub
	moodSyncer *MoodSyncer
	logger     *slog.Logger
}

func newIncidentHandler(hub *kubews.Hub, moodSyncer *MoodSyncer, logger *slog.Logger) cache.ResourceEventHandler {
	return &incidentHandler{hub: hub, moodSyncer: moodSyncer, logger: logger}
}

func (h *incidentHandler) OnAdd(obj any, _ bool) {
	inc, ok := obj.(*v1alpha1.Incident)
	if !ok {
		return
	}
	h.hub.Broadcast(kubews.Marshal(kubews.IncidentEvent{
		BaseEvent:          kubews.BaseEvent{Type: kubews.EventIncidentCreated},
		Namespace:          inc.Namespace,
		Name:               inc.Name,
		State:              string(inc.Status.State),
		RootResourceKind:   inc.Spec.RootResource.Kind,
		RootResourceName:   inc.Spec.RootResource.Name,
		ActiveProblemCases: inc.Status.ActiveProblemCases,
	}))
	if h.moodSyncer != nil {
		go h.moodSyncer.SyncFromIncidents(context.Background())
	}
}

func (h *incidentHandler) OnUpdate(_, newObj any) {
	inc, ok := newObj.(*v1alpha1.Incident)
	if !ok {
		return
	}
	evType := kubews.EventIncidentUpdated
	if inc.Status.State == v1alpha1.IncidentStateResolved {
		evType = kubews.EventIncidentResolved
	}
	h.hub.Broadcast(kubews.Marshal(kubews.IncidentEvent{
		BaseEvent:          kubews.BaseEvent{Type: evType},
		Namespace:          inc.Namespace,
		Name:               inc.Name,
		State:              string(inc.Status.State),
		RootResourceKind:   inc.Spec.RootResource.Kind,
		RootResourceName:   inc.Spec.RootResource.Name,
		ActiveProblemCases: inc.Status.ActiveProblemCases,
	}))
	if h.moodSyncer != nil {
		go h.moodSyncer.SyncFromIncidents(context.Background())
	}
}

func (h *incidentHandler) OnDelete(obj any) {
	inc, ok := obj.(*v1alpha1.Incident)
	if !ok {
		return
	}
	h.hub.Broadcast(kubews.Marshal(kubews.IncidentEvent{
		BaseEvent: kubews.BaseEvent{Type: kubews.EventIncidentResolved},
		Namespace: inc.Namespace,
		Name:      inc.Name,
		State:     "resolved",
	}))
	if h.moodSyncer != nil {
		go h.moodSyncer.SyncFromIncidents(context.Background())
	}
}

// --- DiagnosticRun handler ---

type diagnosticRunHandler struct {
	hub    *kubews.Hub
	logger *slog.Logger
}

func newDiagnosticRunHandler(hub *kubews.Hub, logger *slog.Logger) cache.ResourceEventHandler {
	return &diagnosticRunHandler{hub: hub, logger: logger}
}

func (h *diagnosticRunHandler) OnAdd(_ any, _ bool) {}

func (h *diagnosticRunHandler) OnUpdate(_, newObj any) {
	dr, ok := newObj.(*v1alpha1.DiagnosticRun)
	if !ok {
		return
	}
	h.hub.Broadcast(kubews.Marshal(kubews.DiagnosticRunEvent{
		BaseEvent:   kubews.BaseEvent{Type: kubews.EventDiagnosticRunStateChanged},
		Namespace:   dr.Namespace,
		Name:        dr.Name,
		State:       string(dr.Status.State),
		IncidentRef: dr.Spec.IncidentRef,
	}))
}

func (h *diagnosticRunHandler) OnDelete(_ any) {}
