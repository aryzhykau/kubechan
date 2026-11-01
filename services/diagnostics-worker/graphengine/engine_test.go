// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package graphengine

import (
	"encoding/json"
	"fmt"
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
	if graph.Candidates[0].SourceNodeID != "resource:networking.k8s.io:Ingress:demo:nginx" {
		t.Fatalf("candidate source = %q", graph.Candidates[0].SourceNodeID)
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

func TestBuildIncident10MultiPointGraph(t *testing.T) {
	ev := &collector.Evidence{
		CollectedAt:  time.Unix(2, 0).UTC(),
		RootResource: collector.ResourceRef{Kind: "Service", Namespace: "kubechan-incident-10", Name: "gateway-public"},
		RelatedResourceEvidence: []collector.RelatedResourceEvidence{
			{DiscoveryOrigin: "root", Resource: collector.ResourceRef{Kind: "Service", Namespace: "kubechan-incident-10", Name: "gateway-public"}, Spec: map[string]any{
				"selector":    map[string]any{"app": "gateway", "track": "canary"},
				"annotations": map[string]any{"kubechan.io/renamed-from": "gateway"},
			}},
			{DiscoveryOrigin: "automatic", Resource: collector.ResourceRef{APIGroup: "networking.k8s.io", Kind: "Ingress", Namespace: "kubechan-incident-10", Name: "gateway"}, Spec: map[string]any{
				"rules": []any{map[string]any{"paths": []any{map[string]any{"path": "/", "backendService": "gateway-public", "backendPort": 80}}}},
			}},
			{DiscoveryOrigin: "automatic", Resource: collector.ResourceRef{APIGroup: "apps", Kind: "Deployment", Namespace: "kubechan-incident-10", Name: "gateway"}, Spec: map[string]any{
				"templateLabels": map[string]any{"app": "gateway", "track": "stable"},
				"dependencies": []any{
					map[string]any{"kind": "ConfigMap", "name": "gateway-config", "exists": false, "fieldPath": "/spec/template/spec/volumes/0/configMap"},
					map[string]any{"kind": "Secret", "name": "gateway-secrets", "exists": false, "fieldPath": "/spec/template/spec/containers/0/env/0/valueFrom/secretKeyRef"},
					map[string]any{"kind": "PersistentVolumeClaim", "name": "uploads", "exists": true, "fieldPath": "/spec/template/spec/volumes/1/persistentVolumeClaim"},
				},
			}},
			{DiscoveryOrigin: "automatic", Resource: collector.ResourceRef{Kind: "PersistentVolumeClaim", Namespace: "kubechan-incident-10", Name: "uploads"}, Spec: map[string]any{
				"phase": "Pending", "state": "degraded", "storageClass": "kubechan-nonexistent-storage-class", "storageClassExists": false,
			}},
			{DiscoveryOrigin: "automatic", Resource: collector.ResourceRef{APIGroup: "discovery.k8s.io", Kind: "EndpointSlice", Namespace: "kubechan-incident-10", Name: "gateway-public-empty"}, Spec: map[string]any{
				"serviceName": "gateway-public", "readyEndpoints": 0, "notReadyEndpoints": 0,
			}},
		},
	}
	ev.Sources = collector.BuildSourceCatalog(ev)
	graph := Build(ev)

	if graph.SchemaVersion != 2 || graph.Policy.MaxNodes != 50 {
		t.Fatalf("unexpected graph contract: %#v", graph)
	}
	wantEdges := map[string]string{
		"routes_to": "resolved", "has_endpoints": "empty", "possible_selector_target": "proposed",
		"configured_by": "broken", "uses_secret": "broken", "mounts": "resolved", "uses_storage_class": "broken",
	}
	for edgeType, state := range wantEdges {
		found := false
		for _, edge := range graph.Edges {
			if edge.Type == edgeType && edge.State == state {
				found = true
				if edgeType == "possible_selector_target" && (edge.Origin != "inferred" || edge.Strength != "strong") {
					t.Fatalf("selector inference = %#v", edge)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s/%s edge in %#v", edgeType, state, graph.Edges)
		}
	}
}

func TestSelectorInferenceScoreWeightsIdentityLabels(t *testing.T) {
	score, matched, conflicts := SelectorInferenceScore(
		map[string]string{"app": "gateway", "track": "canary"},
		map[string]string{"app": "gateway", "track": "stable"},
	)
	if score != 75 || matched["app"] != "gateway" || conflicts["track"]["actual"] != "stable" {
		t.Fatalf("score=%d matched=%#v conflicts=%#v", score, matched, conflicts)
	}
}

func TestBuildPolicyBoundsDisconnectedContext(t *testing.T) {
	ev := &collector.Evidence{RootResource: collector.ResourceRef{Kind: "Service", Namespace: "demo", Name: "seed"}}
	for i := 0; i < 20; i++ {
		ev.RelatedResourceEvidence = append(ev.RelatedResourceEvidence, collector.RelatedResourceEvidence{
			DiscoveryOrigin: "automatic",
			Resource:        collector.ResourceRef{APIGroup: "apps", Kind: "Deployment", Namespace: "demo", Name: fmt.Sprintf("unrelated-%02d", i)},
		})
	}
	policy := DefaultPolicy()
	policy.MaxNodes = 5
	graph := BuildWithPolicy(ev, policy)
	if len(graph.Nodes) != 1 || graph.Truncated != true {
		t.Fatalf("bounded graph = nodes:%d truncated:%v", len(graph.Nodes), graph.Truncated)
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
