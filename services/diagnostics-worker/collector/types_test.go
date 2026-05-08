// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"testing"
	"time"
)

func TestResourceRef_Fields(t *testing.T) {
	t.Parallel()
	ref := ResourceRef{Kind: "Deployment", Name: "app", APIGroup: "apps"}
	if ref.Kind != "Deployment" || ref.Name != "app" || ref.APIGroup != "apps" {
		t.Errorf("unexpected ResourceRef: %+v", ref)
	}
}

func TestK8sEvent_Fields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	ev := K8sEvent{
		Type:      "Warning",
		Reason:    "BackOff",
		Message:   "Back-off restarting failed container",
		Count:     10,
		FirstTime: now,
		LastTime:  now,
	}
	if ev.Type != "Warning" || ev.Count != 10 {
		t.Errorf("unexpected K8sEvent: %+v", ev)
	}
}

func TestConfigMapRef_Fields(t *testing.T) {
	t.Parallel()
	cm := ConfigMapRef{
		Name:       "app-config",
		Missing:    false,
		Keys:       []string{"key1", "key2"},
		Data:       map[string]string{"key1": "val1"},
		MountPaths: []string{"/etc/config"},
	}
	if len(cm.Keys) != 2 || len(cm.MountPaths) != 1 {
		t.Errorf("unexpected ConfigMapRef: %+v", cm)
	}
}

func TestSecretRef_Missing(t *testing.T) {
	t.Parallel()
	s := SecretRef{Name: "db-creds", Missing: true}
	if !s.Missing {
		t.Error("expected Missing=true")
	}
}

func TestPodDependencies_Fields(t *testing.T) {
	t.Parallel()
	deps := PodDependencies{
		ConfigMaps: []ConfigMapRef{{Name: "cfg"}},
		Secrets:    []SecretRef{{Name: "sec"}},
	}
	if len(deps.ConfigMaps) != 1 || len(deps.Secrets) != 1 {
		t.Errorf("unexpected PodDependencies: %+v", deps)
	}
}

func TestPodLogs_Fields(t *testing.T) {
	t.Parallel()
	pl := PodLogs{
		PodName:   "app-abc",
		Phase:     "Running",
		Logs:      "log line",
		Truncated: false,
		Events:    []K8sEvent{{Type: "Normal", Reason: "Started"}},
	}
	if pl.PodName != "app-abc" || len(pl.Events) != 1 {
		t.Errorf("unexpected PodLogs: %+v", pl)
	}
}

func TestPVCInfo_Fields(t *testing.T) {
	t.Parallel()
	pvc := PVCInfo{
		Name:             "data-pvc",
		Phase:            "Bound",
		StorageClass:     "standard",
		RequestedStorage: "10Gi",
	}
	if pvc.Phase != "Bound" || pvc.RequestedStorage != "10Gi" {
		t.Errorf("unexpected PVCInfo: %+v", pvc)
	}
}

func TestProblemCaseEvidence_Fields(t *testing.T) {
	t.Parallel()
	pce := ProblemCaseEvidence{
		Name:             "pc-1",
		Detector:         "CrashLoop",
		Severity:         "high",
		Symptoms:         []string{"pod is crashing"},
		AffectedResource: ResourceRef{Kind: "Deployment", Name: "app"},
	}
	if pce.Detector != "CrashLoop" || len(pce.Symptoms) != 1 {
		t.Errorf("unexpected ProblemCaseEvidence: %+v", pce)
	}
}

func TestEvidence_Fields(t *testing.T) {
	t.Parallel()
	ev := Evidence{
		DiagnosticRunID: "run-1",
		IncidentID:      "inc-1",
		CollectedAt:     time.Now(),
		RootResource:    ResourceRef{Kind: "Deployment", Name: "app"},
		ProblemCases: []ProblemCaseEvidence{
			{Name: "pc-1", Detector: "CrashLoop"},
		},
		WorkloadPodLogs: []PodLogs{{PodName: "pod-1", Phase: "Running"}},
		PVCInfos:        []PVCInfo{{Name: "pvc-1", Phase: "Bound"}},
		UserMessage:     "manual incident",
	}
	if ev.DiagnosticRunID != "run-1" || len(ev.ProblemCases) != 1 {
		t.Errorf("unexpected Evidence: %+v", ev)
	}
}
