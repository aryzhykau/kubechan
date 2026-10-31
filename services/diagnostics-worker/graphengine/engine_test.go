// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package graphengine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/org/kubechan/services/diagnostics-worker/collector"
)

func TestBuildMissingIngressServiceAndCandidate(t *testing.T) {
	ev := fixtureEvidence()
	ev.Sources = collector.BuildSourceCatalog(ev)
	graph := Build(ev)
	if graph.EntryResourceID != "resource:apps:Deployment:demo:nginx" {
		t.Fatalf("entry = %q", graph.EntryResourceID)
	}
	var broken bool
	for _, edge := range graph.Edges {
		if edge.Type == "routes_to" && edge.State == "broken" {
			broken = true
		}
	}
	if !broken {
		t.Fatal("expected broken routes_to edge")
	}
	if len(graph.Candidates) != 1 {
		t.Fatalf("candidates = %d", len(graph.Candidates))
	}
	if graph.Candidates[0].Strength != "strong" || graph.Candidates[0].Score < 100 {
		t.Fatalf("candidate = %#v", graph.Candidates[0])
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	ev := fixtureEvidence()
	ev.Sources = collector.BuildSourceCatalog(ev)
	first, _ := json.Marshal(Build(ev))
	second, _ := json.Marshal(Build(ev))
	if string(first) != string(second) {
		t.Fatal("graph output is not deterministic")
	}
}

func fixtureEvidence() *collector.Evidence {
	return &collector.Evidence{
		CollectedAt:     time.Unix(1, 0).UTC(),
		RootResource:    collector.ResourceRef{APIGroup: "apps", Kind: "Deployment", Namespace: "demo", Name: "nginx"},
		WorkloadPodLogs: []collector.PodLogs{{PodName: "nginx-abc", Phase: "Running", Labels: map[string]string{"app": "nginx"}}},
		RelatedResourceEvidence: []collector.RelatedResourceEvidence{
			{Resource: collector.ResourceRef{Kind: "Ingress", Namespace: "demo", Name: "nginx"}, Spec: map[string]any{"rules": []any{map[string]any{"host": "demo.local", "paths": []any{map[string]any{"path": "/", "backendService": "nginx", "backendPort": 80}}}}}},
			{Resource: collector.ResourceRef{Kind: "Service", Namespace: "demo", Name: "nginx-svc"}, Spec: map[string]any{"selector": map[string]any{"app": "nginx"}, "ports": []any{map[string]any{"port": 80}}, "annotations": map[string]any{"kubechan.io/renamed-from": "nginx"}}},
		},
	}
}
