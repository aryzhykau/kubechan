// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"testing"
	"time"
)

func TestBuildSourceCatalogDeterministicAndAtomic(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ev := &Evidence{
		CollectedAt:     now,
		RootResource:    ResourceRef{Kind: "Deployment", Namespace: "demo", Name: "api"},
		WorkloadPodLogs: []PodLogs{{PodName: "api-123", Phase: "CrashLoopBackOff", Logs: "first\nsecond", Truncated: true}},
		RelatedResourceEvidence: []RelatedResourceEvidence{{
			Resource: ResourceRef{Kind: "Ingress", Namespace: "demo", Name: "api"},
			Spec:     map[string]any{"backend": map[string]any{"service": "missing"}},
		}},
	}
	one := BuildSourceCatalog(ev)
	two := BuildSourceCatalog(ev)
	if len(one) != 4 {
		t.Fatalf("expected 4 atomic sources, got %d", len(one))
	}
	for i := range one {
		if one[i].ID != two[i].ID {
			t.Fatalf("source ID is not deterministic: %q != %q", one[i].ID, two[i].ID)
		}
	}
	if one[1].Line == nil || one[1].Line.Number != 1 || !one[1].Truncated {
		t.Fatalf("expected first truncated log line, got %#v", one[1])
	}
	if got := one[3].Path; got != "/backend/service" {
		t.Fatalf("unexpected JSON pointer %q", got)
	}
}

func TestBuildSourceCatalogIncludesWorkloadPodLabelsAndClaimReferences(t *testing.T) {
	ref := ResourceRef{Kind: "Pod", Namespace: "demo", Name: "gateway-1"}
	ev := &Evidence{
		RootResource: ResourceRef{Kind: "Service", Namespace: ref.Namespace, Name: "gateway-public"},
		WorkloadPodLogs: []PodLogs{{
			PodName: ref.Name, Phase: "Pending",
			Labels:   map[string]string{"track": "stable", "app.example.com/name": "gateway"},
			PVCNames: []string{"uploads"},
		}},
	}
	sources := BuildSourceCatalog(ev)
	want := []struct{ path, value string }{
		{"/status/phase", "Pending"},
		{"/labels/app.example.com~1name", "gateway"},
		{"/labels/track", "stable"},
		{"/pvcNames/0", "uploads"},
	}
	if len(sources) != len(want) {
		t.Fatalf("got %d sources, want %d", len(sources), len(want))
	}
	for i, expected := range want {
		source := sources[i]
		if source.Resource != ref || source.Type != "resource_field" || source.Path != expected.path || source.Value != expected.value {
			t.Errorf("source %d = %#v, want %s = %s on %v", i, source, expected.path, expected.value, ref)
		}
	}
}

func TestSecretSourceNeverContainsContent(t *testing.T) {
	ev := &Evidence{
		RootResource:    ResourceRef{Kind: "Deployment", Namespace: "demo", Name: "api"},
		WorkloadPodLogs: []PodLogs{{PodName: "api-1", Dependencies: &PodDependencies{Secrets: []SecretRef{{Name: "db-password"}}}}},
	}
	sources := BuildSourceCatalog(ev)
	if len(sources) < 2 {
		t.Fatalf("expected phase and secret sources, got %d", len(sources))
	}
	secret := sources[len(sources)-1]
	if !secret.Redacted || secret.Path != "/exists" {
		t.Fatalf("secret source must expose existence only: %#v", secret)
	}
}

func TestBuildSourceCatalogFlattensTypedMapSlices(t *testing.T) {
	ev := &Evidence{
		RootResource: ResourceRef{Kind: "Deployment", Namespace: "demo", Name: "api"},
		RelatedResourceEvidence: []RelatedResourceEvidence{{
			Resource: ResourceRef{Kind: "Ingress", Namespace: "demo", Name: "api"},
			Spec: map[string]any{"rules": []map[string]any{{
				"paths": []map[string]any{{"backendService": "missing-service"}},
			}}},
		}},
	}
	sources := BuildSourceCatalog(ev)
	if len(sources) != 1 {
		t.Fatalf("expected one atomic source, got %#v", sources)
	}
	if got := sources[0].Path; got != "/rules/0/paths/0/backendService" {
		t.Fatalf("unexpected JSON pointer %q", got)
	}
}

func TestBuildSourceCatalogIncludesProblemCaseObservations(t *testing.T) {
	ev := &Evidence{
		CollectedAt:  time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
		RootResource: ResourceRef{Kind: "Service", Namespace: "demo", Name: "gateway-public"},
		ProblemCases: []ProblemCaseEvidence{{
			Name:             "service-no-endpoints",
			Detector:         "ServiceNoEndpoints",
			Severity:         "high",
			Symptoms:         []string{"service \"gateway-public\" has no ready endpoints"},
			AffectedResource: ResourceRef{Kind: "Service", Namespace: "demo", Name: "gateway-public"},
		}},
	}

	sources := BuildSourceCatalog(ev)
	if len(sources) != 3 {
		t.Fatalf("expected detector, severity, and symptom sources, got %#v", sources)
	}
	if sources[2].Path != "/problemCases/service-no-endpoints/symptoms/0" {
		t.Fatalf("unexpected symptom path %q", sources[2].Path)
	}
	if sources[2].Value != "service \"gateway-public\" has no ready endpoints" {
		t.Fatalf("unexpected symptom value %#v", sources[2].Value)
	}
}
