// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

// Package graphengine builds a deterministic investigation graph from the
// exact evidence snapshot sent to the analysis model.
package graphengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/org/kubechan/services/diagnostics-worker/collector"
)

type builder struct {
	ev         *collector.Evidence
	nodes      map[string]collector.GraphNode
	edges      map[string]collector.GraphEdge
	candidates map[string]collector.GraphCandidate
	sources    map[string][]collector.EvidenceSource
}

// Build creates the same graph for the same evidence regardless of input order.
func Build(ev *collector.Evidence) *collector.InvestigationGraph {
	b := &builder{ev: ev, nodes: map[string]collector.GraphNode{}, edges: map[string]collector.GraphEdge{}, candidates: map[string]collector.GraphCandidate{}, sources: map[string][]collector.EvidenceSource{}}
	for _, source := range ev.Sources {
		key := resourceKey(source.Resource)
		b.sources[key] = append(b.sources[key], source)
	}

	entry := b.addResource(ev.RootResource, "entry", nil)
	for _, pod := range ev.WorkloadPodLogs {
		ref := collector.ResourceRef{Kind: "Pod", Namespace: ev.RootResource.Namespace, Name: pod.PodName}
		state := "healthy"
		if pod.Phase != "Running" && pod.Phase != "Succeeded" {
			state = "degraded"
		}
		podID := b.addResource(ref, "workload", map[string]any{"phase": pod.Phase, "labels": pod.Labels, "state": state})
		b.addEdge("owns", entry, podID, "resolved", nil, nil)
		if pod.Dependencies != nil {
			for _, cm := range pod.Dependencies.ConfigMaps {
				dep := collector.ResourceRef{Kind: "ConfigMap", Namespace: ref.Namespace, Name: cm.Name}
				target := b.addDependency(dep, cm.Missing)
				b.addEdge("configured_by", podID, target, edgeState(cm.Missing), map[string]any{"mountPaths": cm.MountPaths}, b.sourceIDs(dep, "/exists"))
			}
			for _, secret := range pod.Dependencies.Secrets {
				dep := collector.ResourceRef{Kind: "Secret", Namespace: ref.Namespace, Name: secret.Name}
				target := b.addDependency(dep, secret.Missing)
				b.addEdge("uses_secret", podID, target, edgeState(secret.Missing), nil, b.sourceIDs(dep, "/exists"))
			}
		}
		for _, pvcName := range pod.PVCNames {
			pvcRef := collector.ResourceRef{Kind: "PersistentVolumeClaim", Namespace: ref.Namespace, Name: pvcName}
			pvcID := nodeID("resource", pvcRef)
			b.addEdge("mounts", podID, pvcID, "resolved", nil, b.sourceIDs(pvcRef, ""))
		}
	}
	for _, pvc := range ev.PVCInfos {
		ref := collector.ResourceRef{Kind: "PersistentVolumeClaim", Namespace: ev.RootResource.Namespace, Name: pvc.Name}
		state := "healthy"
		if pvc.Phase != "Bound" {
			state = "degraded"
		}
		b.addResource(ref, "dependency", map[string]any{"phase": pvc.Phase, "storageClass": pvc.StorageClass, "state": state})
	}
	for _, related := range ev.RelatedResourceEvidence {
		b.addResource(related.Resource, "related", related.Spec)
	}

	b.connectServicesToPods()
	b.connectIngressesToServices()

	nodes := make([]collector.GraphNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	edges := make([]collector.GraphEdge, 0, len(b.edges))
	for _, edge := range b.edges {
		edges = append(edges, edge)
	}
	candidates := make([]collector.GraphCandidate, 0, len(b.candidates))
	for _, candidate := range b.candidates {
		candidates = append(candidates, candidate)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return &collector.InvestigationGraph{SchemaVersion: 1, EntryResourceID: entry, Nodes: nodes, Edges: edges, Candidates: candidates}
}

func (b *builder) addResource(ref collector.ResourceRef, role string, attributes map[string]any) string {
	id := nodeID("resource", ref)
	state := "present"
	if raw, ok := attributes["state"]; ok {
		state = fmt.Sprint(raw)
	}
	b.nodes[id] = collector.GraphNode{ID: id, Type: "resource", Resource: ref, State: state, Role: role, Attributes: withoutState(attributes), SourceIDs: b.sourceIDs(ref, "")}
	return id
}

func (b *builder) addDependency(ref collector.ResourceRef, missing bool) string {
	if !missing {
		return b.addResource(ref, "dependency", nil)
	}
	id := nodeID("reference", ref)
	b.nodes[id] = collector.GraphNode{ID: id, Type: "reference", Resource: ref, State: "missing", Role: "dependency", SourceIDs: b.sourceIDs(ref, "/exists")}
	return id
}

func (b *builder) addEdge(kind, from, to, state string, attributes map[string]any, sourceIDs []string) string {
	id := stableID("edge", kind, from, to)
	b.edges[id] = collector.GraphEdge{ID: id, Type: kind, From: from, To: to, State: state, Attributes: attributes, SourceIDs: uniqueSorted(sourceIDs)}
	return id
}

func (b *builder) connectServicesToPods() {
	for serviceID, node := range b.nodes {
		if node.Type != "resource" || node.Resource.Kind != "Service" {
			continue
		}
		selector := stringMap(node.Attributes["selector"])
		if len(selector) == 0 {
			continue
		}
		matched := 0
		for podID, pod := range b.nodes {
			if pod.Resource.Kind != "Pod" || pod.Resource.Namespace != node.Resource.Namespace {
				continue
			}
			if labelsMatch(selector, stringMap(pod.Attributes["labels"])) {
				b.addEdge("selects", serviceID, podID, "resolved", map[string]any{"selector": selector}, b.sourceIDs(node.Resource, "/selector"))
				matched++
			}
		}
		if matched == 0 {
			b.nodes[serviceID] = withState(node, "degraded")
		}
	}
}

func (b *builder) connectIngressesToServices() {
	for ingressID, node := range b.nodes {
		if node.Type != "resource" || node.Resource.Kind != "Ingress" {
			continue
		}
		for ruleIndex, rawRule := range anySlice(node.Attributes["rules"]) {
			rule, _ := rawRule.(map[string]any)
			for pathIndex, rawPath := range anySlice(rule["paths"]) {
				path, _ := rawPath.(map[string]any)
				name := fmt.Sprint(path["backendService"])
				if name == "" || name == "<nil>" {
					continue
				}
				targetRef := collector.ResourceRef{Kind: "Service", Namespace: node.Resource.Namespace, Name: name}
				targetID := nodeID("resource", targetRef)
				state := "resolved"
				if _, ok := b.nodes[targetID]; !ok {
					targetID = nodeID("reference", targetRef)
					state = "broken"
					b.nodes[targetID] = collector.GraphNode{ID: targetID, Type: "reference", Resource: targetRef, State: "missing", Role: "referenced"}
				}
				pathPointer := fmt.Sprintf("/rules/%d/paths/%d/backendService", ruleIndex, pathIndex)
				sourceIDs := b.sourceIDs(node.Resource, pathPointer)
				b.addEdge("routes_to", ingressID, targetID, state, map[string]any{"path": path["path"], "port": path["backendPort"], "fieldPath": pathPointer}, sourceIDs)
				if state == "broken" {
					b.rankServiceCandidates(targetID, path, sourceIDs)
				}
			}
		}
	}
}

func (b *builder) rankServiceCandidates(referenceID string, ingressPath map[string]any, referenceSources []string) {
	ref := b.nodes[referenceID].Resource
	for candidateID, node := range b.nodes {
		if node.Type != "resource" || node.Resource.Kind != "Service" || node.Resource.Namespace != ref.Namespace {
			continue
		}
		score := 0
		var reasons []collector.CandidateReason
		annotations := stringMap(node.Attributes["annotations"])
		if annotations["kubechan.io/renamed-from"] == ref.Name {
			score += 100
			reasons = append(reasons, collector.CandidateReason{Code: "renamed_from", Label: "Service was renamed from " + ref.Name, SourceIDs: b.sourceIDs(node.Resource, "/annotations")})
		}
		selector := stringMap(node.Attributes["selector"])
		if b.selectorMatchesWorkload(selector, node.Resource.Namespace) {
			score += 40
			reasons = append(reasons, collector.CandidateReason{Code: "selects_workload", Label: "Selector matches the reported workload", SourceIDs: b.sourceIDs(node.Resource, "/selector")})
		}
		if serviceHasPort(node.Attributes["ports"], ingressPath["backendPort"]) {
			score += 20
			reasons = append(reasons, collector.CandidateReason{Code: "matching_port", Label: "Service exposes the referenced port", SourceIDs: b.sourceIDs(node.Resource, "/ports")})
		}
		if score < 40 {
			continue
		}
		strength := "possible"
		if score >= 100 {
			strength = "strong"
		}
		id := stableID("candidate", referenceID, candidateID)
		b.candidates[id] = collector.GraphCandidate{ID: id, ReferenceNodeID: referenceID, CandidateNodeID: candidateID, Score: score, Strength: strength, Reasons: reasons}
		_ = referenceSources
	}
}

func (b *builder) selectorMatchesWorkload(selector map[string]string, namespace string) bool {
	if len(selector) == 0 {
		return false
	}
	for _, node := range b.nodes {
		if node.Resource.Kind == "Pod" && node.Resource.Namespace == namespace && labelsMatch(selector, stringMap(node.Attributes["labels"])) {
			return true
		}
	}
	return false
}

func (b *builder) sourceIDs(ref collector.ResourceRef, pathContains string) []string {
	var ids []string
	for _, source := range b.sources[resourceKey(ref)] {
		if pathContains == "" || strings.Contains(source.Path, pathContains) {
			ids = append(ids, source.ID)
		}
	}
	return uniqueSorted(ids)
}

func nodeID(kind string, ref collector.ResourceRef) string {
	return strings.Join([]string{kind, apiGroup(ref), ref.Kind, ref.Namespace, ref.Name}, ":")
}
func resourceKey(ref collector.ResourceRef) string {
	return strings.Join([]string{apiGroup(ref), strings.ToLower(ref.Kind), ref.Namespace, ref.Name}, "|")
}
func apiGroup(ref collector.ResourceRef) string {
	if ref.APIGroup != "" {
		return ref.APIGroup
	}
	switch ref.Kind {
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		return "apps"
	case "Ingress", "NetworkPolicy":
		return "networking.k8s.io"
	case "Job", "CronJob":
		return "batch"
	default:
		return "core"
	}
}
func edgeState(missing bool) string {
	if missing {
		return "broken"
	}
	return "resolved"
}
func stableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return prefix + ":" + hex.EncodeToString(sum[:8])
}
func withoutState(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range values {
		if k != "state" {
			out[k] = v
		}
	}
	return out
}
func withState(node collector.GraphNode, state string) collector.GraphNode {
	node.State = state
	return node
}
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func stringMap(value any) map[string]string {
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		for k, v := range typed {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}
func anySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = typed[i]
		}
		return out
	default:
		return nil
	}
}
func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
func serviceHasPort(value any, target any) bool {
	wanted := fmt.Sprint(target)
	for _, raw := range anySlice(value) {
		if port, ok := raw.(map[string]any); ok && (fmt.Sprint(port["port"]) == wanted || fmt.Sprint(port["name"]) == wanted) {
			return true
		}
	}
	if n, err := strconv.Atoi(wanted); err == nil {
		return n > 0 && len(anySlice(value)) == 0
	}
	return false
}
