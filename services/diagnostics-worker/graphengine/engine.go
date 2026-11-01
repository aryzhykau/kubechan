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
	policy     collector.GraphPolicy
	nodes      map[string]collector.GraphNode
	edges      map[string]collector.GraphEdge
	candidates map[string]collector.GraphCandidate
	sources    map[string][]collector.EvidenceSource
}

// DefaultPolicy bounds graph expansion while allowing one explainable inferred
// hop to bridge a broken selector or renamed reference.
func DefaultPolicy() collector.GraphPolicy {
	return collector.GraphPolicy{
		MaxObservedDepth:    4,
		MaxInferredHops:     1,
		MaxNodes:            50,
		MaxEdges:            150,
		MaxCandidatesPerGap: 5,
		StrongThreshold:     100,
		PossibleThreshold:   60,
	}
}

// Build creates the same graph for the same evidence regardless of input order.
func Build(ev *collector.Evidence) *collector.InvestigationGraph {
	return BuildWithPolicy(ev, DefaultPolicy())
}

// BuildWithPolicy creates the same bounded graph for the same evidence and
// policy regardless of input order.
func BuildWithPolicy(ev *collector.Evidence, policy collector.GraphPolicy) *collector.InvestigationGraph {
	b := &builder{ev: ev, policy: policy, nodes: map[string]collector.GraphNode{}, edges: map[string]collector.GraphEdge{}, candidates: map[string]collector.GraphCandidate{}, sources: map[string][]collector.EvidenceSource{}}
	for _, source := range ev.Sources {
		key := resourceKey(source.Resource)
		b.sources[key] = append(b.sources[key], source)
	}

	entry := b.addResource(ev.RootResource, "entry", nil)
	for _, related := range ev.RelatedResourceEvidence {
		role := "related"
		if related.DiscoveryOrigin == "automatic" {
			role = "discovered"
		}
		if sameResource(related.Resource, ev.RootResource) {
			role = "entry"
		}
		b.addResource(related.Resource, role, related.Spec)
	}
	for _, pod := range ev.WorkloadPodLogs {
		ref := collector.ResourceRef{Kind: "Pod", Namespace: ev.RootResource.Namespace, Name: pod.PodName}
		state := "healthy"
		if pod.Phase != "Running" && pod.Phase != "Succeeded" {
			state = "degraded"
		}
		podID := b.addResource(ref, "workload", map[string]any{"phase": pod.Phase, "labels": pod.Labels, "state": state})
		for _, owner := range pod.Owners {
			ownerID := nodeID("resource", owner)
			if _, ok := b.nodes[ownerID]; ok {
				b.addEdge("owns", ownerID, podID, "resolved", nil, b.sourceIDs(ref, "/owners"))
			}
		}
		if len(pod.Owners) == 0 && isWorkloadKind(ev.RootResource.Kind) {
			b.addEdge("owns", entry, podID, "resolved", nil, nil)
		}
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
	b.connectServicesToPods()
	b.connectWorkloadsToPods()
	b.connectIngressesToServices()
	b.connectWorkloadDependencies()
	b.connectAutoscalers()
	b.connectEndpointSlices()
	b.connectPVCStorageClasses()

	nodes, edges, truncated := b.bounded(entry)
	candidates := make([]collector.GraphCandidate, 0, len(b.candidates))
	allowedNodes := map[string]bool{}
	for _, node := range nodes {
		allowedNodes[node.ID] = true
	}
	for _, candidate := range b.candidates {
		if allowedNodes[candidate.SourceNodeID] && allowedNodes[candidate.CandidateNodeID] {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return &collector.InvestigationGraph{SchemaVersion: 2, EntryResourceID: entry, Policy: policy, Truncated: truncated, Nodes: nodes, Edges: edges, Candidates: candidates}
}

func (b *builder) connectWorkloadsToPods() {
	for workloadID, workload := range b.nodes {
		if !isWorkloadKind(workload.Resource.Kind) {
			continue
		}
		labels := stringMap(workload.Attributes["templateLabels"])
		if len(labels) == 0 {
			continue
		}
		for podID, pod := range b.nodes {
			if pod.Resource.Kind != "Pod" || pod.Resource.Namespace != workload.Resource.Namespace {
				continue
			}
			if labelsMatch(labels, stringMap(pod.Attributes["labels"])) {
				b.addEdge("manages", workloadID, podID, "resolved", map[string]any{"templateLabels": labels}, b.sourceIDs(workload.Resource, "/templateLabels"))
			}
		}
	}
}

func (b *builder) allSorted() ([]collector.GraphNode, []collector.GraphEdge) {
	nodes := make([]collector.GraphNode, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	edges := make([]collector.GraphEdge, 0, len(b.edges))
	for _, edge := range b.edges {
		edges = append(edges, edge)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return nodes, edges
}

func (b *builder) addResource(ref collector.ResourceRef, role string, attributes map[string]any) string {
	id := nodeID("resource", ref)
	state := "present"
	merged := map[string]any{}
	if current, ok := b.nodes[id]; ok {
		for key, value := range current.Attributes {
			merged[key] = value
		}
		if current.State != "" {
			state = current.State
		}
		if role == "context" || role == "dependency" {
			role = current.Role
		}
	}
	for key, value := range withoutState(attributes) {
		merged[key] = value
	}
	if raw, ok := attributes["state"]; ok {
		state = fmt.Sprint(raw)
	}
	b.nodes[id] = collector.GraphNode{ID: id, Type: "resource", Resource: ref, State: state, Role: role, Attributes: merged, SourceIDs: b.sourceIDs(ref, "")}
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
	b.edges[id] = collector.GraphEdge{ID: id, Type: kind, From: from, To: to, State: state, Origin: "observed", Attributes: attributes, SourceIDs: uniqueSorted(sourceIDs)}
	return id
}

func (b *builder) addInferredEdge(kind, from, to string, score int, reasons []collector.CandidateReason, attributes map[string]any, sourceIDs []string) string {
	strength := "possible"
	if score >= b.policy.StrongThreshold {
		strength = "strong"
	}
	id := stableID("edge", "inferred", kind, from, to)
	b.edges[id] = collector.GraphEdge{ID: id, Type: kind, From: from, To: to, State: "proposed", Origin: "inferred", Strength: strength, Score: score, Reasons: reasons, Attributes: attributes, SourceIDs: uniqueSorted(sourceIDs)}
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
			b.inferServiceWorkloads(serviceID, node, selector)
		}
	}
}

func (b *builder) inferServiceWorkloads(serviceID string, service collector.GraphNode, selector map[string]string) {
	type ranked struct {
		id      string
		score   int
		reasons []collector.CandidateReason
		attrs   map[string]any
		sources []string
	}
	var candidates []ranked
	for workloadID, workload := range b.nodes {
		if workload.Type != "resource" || workload.Resource.Namespace != service.Resource.Namespace || !isWorkloadKind(workload.Resource.Kind) {
			continue
		}
		labels := stringMap(workload.Attributes["templateLabels"])
		if len(labels) == 0 {
			labels = stringMap(workload.Attributes["labels"])
		}
		score, matched, conflicts := SelectorInferenceScore(selector, labels)
		if score < b.policy.PossibleThreshold || len(matched) == 0 {
			continue
		}
		serviceSystem := systemIdentity(service.Attributes)
		workloadSystem := systemIdentity(workload.Attributes)
		if serviceSystem != "" && workloadSystem != "" && serviceSystem != workloadSystem {
			continue
		}
		var reasons []collector.CandidateReason
		selectorSources := b.sourceIDs(service.Resource, "/selector")
		workloadSources := b.sourceIDs(workload.Resource, "/templateLabels")
		reasons = append(reasons, collector.CandidateReason{Code: "selector_partial_match", Label: fmt.Sprintf("selector matches %d label(s) with %d conflict(s)", len(matched), len(conflicts)), SourceIDs: uniqueSorted(append(selectorSources, workloadSources...))})
		annotations := stringMap(service.Attributes["annotations"])
		if annotations["kubechan.io/renamed-from"] == workload.Resource.Name {
			score += 100
			reasons = append(reasons, collector.CandidateReason{Code: "renamed_from", Label: "Service was renamed from workload name " + workload.Resource.Name, SourceIDs: b.sourceIDs(service.Resource, "/annotations")})
		}
		if serviceSystem != "" && serviceSystem == workloadSystem {
			score += 80
			reasons = append(reasons, collector.CandidateReason{Code: "same_system", Label: "resources share application identity", SourceIDs: uniqueSorted(append(b.sourceIDs(service.Resource, "/labels"), b.sourceIDs(workload.Resource, "/labels")...))})
		}
		attrs := map[string]any{"matchedLabels": matched, "conflictingLabels": conflicts, "selector": selector}
		candidates = append(candidates, ranked{id: workloadID, score: score, reasons: reasons, attrs: attrs, sources: uniqueSorted(append(selectorSources, workloadSources...))})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) > b.policy.MaxCandidatesPerGap {
		candidates = candidates[:b.policy.MaxCandidatesPerGap]
	}
	for _, candidate := range candidates {
		b.addInferredEdge("possible_selector_target", serviceID, candidate.id, candidate.score, candidate.reasons, candidate.attrs, candidate.sources)
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
					b.rankServiceCandidates(ingressID, targetID, path)
				}
			}
		}
	}
}

func (b *builder) connectWorkloadDependencies() {
	for workloadID, node := range b.nodes {
		if !isWorkloadKind(node.Resource.Kind) {
			continue
		}
		for index, raw := range anySlice(node.Attributes["dependencies"]) {
			dep, _ := raw.(map[string]any)
			kind, name := fmt.Sprint(dep["kind"]), fmt.Sprint(dep["name"])
			if kind == "" || name == "" || name == "<nil>" {
				continue
			}
			ref := collector.ResourceRef{Kind: kind, Namespace: node.Resource.Namespace, Name: name}
			targetID := nodeID("resource", ref)
			state := "resolved"
			if exists, ok := dep["exists"].(bool); ok && !exists {
				targetID = nodeID("reference", ref)
				state = "broken"
				b.nodes[targetID] = collector.GraphNode{ID: targetID, Type: "reference", Resource: ref, State: "missing", Role: "dependency", SourceIDs: b.sourceIDs(node.Resource, fmt.Sprintf("/dependencies/%d", index))}
			} else if _, ok := b.nodes[targetID]; !ok {
				b.nodes[targetID] = collector.GraphNode{ID: targetID, Type: "resource", Resource: ref, State: "present", Role: "dependency"}
			}
			edgeType := map[string]string{"ConfigMap": "configured_by", "Secret": "uses_secret", "PersistentVolumeClaim": "mounts"}[kind]
			if edgeType == "" {
				edgeType = "references"
			}
			b.addEdge(edgeType, workloadID, targetID, state, map[string]any{"fieldPath": dep["fieldPath"]}, b.sourceIDs(node.Resource, fmt.Sprintf("/dependencies/%d", index)))
		}
	}
}

func (b *builder) connectAutoscalers() {
	for scalerID, node := range b.nodes {
		if node.Resource.Kind != "HorizontalPodAutoscaler" && node.Resource.Kind != "ScaledObject" {
			continue
		}
		target := mapValue(node.Attributes["scaleTargetRef"])
		if len(target) == 0 {
			if spec := mapValue(node.Attributes["spec"]); len(spec) > 0 {
				target = mapValue(spec["scaleTargetRef"])
			}
		}
		kind, name := fmt.Sprint(target["kind"]), fmt.Sprint(target["name"])
		if kind == "" || kind == "<nil>" || name == "" || name == "<nil>" {
			continue
		}
		ref := collector.ResourceRef{APIGroup: "apps", Kind: kind, Namespace: node.Resource.Namespace, Name: name}
		targetID := nodeID("resource", ref)
		state := "resolved"
		if _, ok := b.nodes[targetID]; !ok {
			targetID = nodeID("reference", ref)
			state = "broken"
			b.nodes[targetID] = collector.GraphNode{ID: targetID, Type: "reference", Resource: ref, State: "missing", Role: "referenced"}
		}
		b.addEdge("scales", scalerID, targetID, state, nil, b.sourceIDs(node.Resource, "scaleTargetRef"))
	}
}

func (b *builder) connectEndpointSlices() {
	for sliceID, node := range b.nodes {
		if node.Resource.Kind != "EndpointSlice" {
			continue
		}
		serviceName := fmt.Sprint(node.Attributes["serviceName"])
		if serviceName == "" || serviceName == "<nil>" {
			continue
		}
		ref := collector.ResourceRef{Kind: "Service", Namespace: node.Resource.Namespace, Name: serviceName}
		serviceID := nodeID("resource", ref)
		if _, ok := b.nodes[serviceID]; !ok {
			continue
		}
		state := "resolved"
		if intValue(node.Attributes["readyEndpoints"]) == 0 {
			state = "empty"
			b.nodes[serviceID] = withState(b.nodes[serviceID], "degraded")
		}
		b.addEdge("has_endpoints", serviceID, sliceID, state, map[string]any{"readyEndpoints": node.Attributes["readyEndpoints"], "notReadyEndpoints": node.Attributes["notReadyEndpoints"]}, b.sourceIDs(node.Resource, ""))
	}
}

func (b *builder) connectPVCStorageClasses() {
	for pvcID, node := range b.nodes {
		if node.Resource.Kind != "PersistentVolumeClaim" {
			continue
		}
		name := fmt.Sprint(node.Attributes["storageClass"])
		if name == "" || name == "<nil>" {
			continue
		}
		ref := collector.ResourceRef{Kind: "StorageClass", Name: name, APIGroup: "storage.k8s.io"}
		targetID := nodeID("resource", ref)
		state := "resolved"
		if exists, ok := node.Attributes["storageClassExists"].(bool); ok && !exists {
			targetID = nodeID("reference", ref)
			state = "broken"
			b.nodes[targetID] = collector.GraphNode{ID: targetID, Type: "reference", Resource: ref, State: "missing", Role: "dependency"}
		} else {
			b.addResource(ref, "dependency", nil)
		}
		b.addEdge("uses_storage_class", pvcID, targetID, state, nil, b.sourceIDs(node.Resource, "/storageClass"))
	}
}

type traversalState struct {
	id           string
	depth        int
	inferredHops int
}

func (b *builder) bounded(entry string) ([]collector.GraphNode, []collector.GraphEdge, bool) {
	allNodes, allEdges := b.allSorted()
	adj := map[string][]collector.GraphEdge{}
	for _, edge := range allEdges {
		adj[edge.From] = append(adj[edge.From], edge)
		adj[edge.To] = append(adj[edge.To], edge)
	}
	for id := range adj {
		sort.Slice(adj[id], func(i, j int) bool { return adj[id][i].ID < adj[id][j].ID })
	}
	seen := map[string]traversalState{entry: {id: entry}}
	queue := []traversalState{{id: entry}}
	for len(queue) > 0 && len(seen) < b.policy.MaxNodes {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range adj[current.id] {
			next := edge.To
			if next == current.id {
				next = edge.From
			}
			depth := current.depth + 1
			inferred := current.inferredHops
			if edge.Origin == "inferred" {
				inferred++
			}
			if depth > b.policy.MaxObservedDepth || inferred > b.policy.MaxInferredHops {
				continue
			}
			if _, ok := seen[next]; ok {
				continue
			}
			state := traversalState{id: next, depth: depth, inferredHops: inferred}
			seen[next] = state
			queue = append(queue, state)
			if len(seen) >= b.policy.MaxNodes {
				break
			}
		}
	}
	// Explicitly collected resources remain useful context even when they are
	// disconnected. Add them deterministically within the same hard node cap.
	for _, node := range allNodes {
		if len(seen) >= b.policy.MaxNodes {
			break
		}
		if node.Role == "related" {
			seen[node.ID] = traversalState{id: node.ID}
		}
	}
	// Preserve the immediate observed/inferred context of explicit resources,
	// including missing reference nodes, without opening another traversal wave.
	for _, edge := range allEdges {
		if len(seen) >= b.policy.MaxNodes {
			break
		}
		_, fromSeen := seen[edge.From]
		_, toSeen := seen[edge.To]
		if fromSeen == toSeen {
			continue
		}
		if fromSeen {
			seen[edge.To] = traversalState{id: edge.To}
		} else {
			seen[edge.From] = traversalState{id: edge.From}
		}
	}
	nodes := make([]collector.GraphNode, 0, len(seen))
	for _, node := range allNodes {
		if _, ok := seen[node.ID]; ok {
			nodes = append(nodes, node)
		}
	}
	edges := make([]collector.GraphEdge, 0)
	for _, edge := range allEdges {
		if len(edges) >= b.policy.MaxEdges {
			break
		}
		if _, fromOK := seen[edge.From]; fromOK {
			if _, toOK := seen[edge.To]; toOK {
				edges = append(edges, edge)
			}
		}
	}
	truncated := len(nodes) < len(allNodes) || len(edges) < len(allEdges)
	return nodes, edges, truncated
}

func (b *builder) rankServiceCandidates(sourceID, referenceID string, ingressPath map[string]any) {
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
		b.candidates[id] = collector.GraphCandidate{ID: id, SourceNodeID: sourceID, ReferenceNodeID: referenceID, CandidateNodeID: candidateID, Score: score, Strength: strength, Reasons: reasons}
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
	case "HorizontalPodAutoscaler":
		return "autoscaling"
	case "EndpointSlice":
		return "discovery.k8s.io"
	case "Ingress", "NetworkPolicy":
		return "networking.k8s.io"
	case "Job", "CronJob":
		return "batch"
	default:
		return "core"
	}
}

func sameResource(left, right collector.ResourceRef) bool {
	return strings.EqualFold(left.Kind, right.Kind) && left.Namespace == right.Namespace && left.Name == right.Name && apiGroup(left) == apiGroup(right)
}

func isWorkloadKind(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob":
		return true
	default:
		return false
	}
}

// SelectorInferenceScore returns a deterministic score and exact evidence of
// partial selector compatibility. Identity labels carry more weight than
// rollout labels so app=gateway,track=canary vs track=stable stays discoverable.
func SelectorInferenceScore(selector, labels map[string]string) (int, map[string]string, map[string]map[string]string) {
	matched := map[string]string{}
	conflicts := map[string]map[string]string{}
	matchedWeight, totalWeight := 0, 0
	for key, expected := range selector {
		weight := selectorKeyWeight(key)
		totalWeight += weight
		actual, exists := labels[key]
		if exists && actual == expected {
			matched[key] = actual
			matchedWeight += weight
		} else if exists {
			conflicts[key] = map[string]string{"expected": expected, "actual": actual}
		}
	}
	if totalWeight == 0 || matchedWeight == 0 {
		return 0, matched, conflicts
	}
	return matchedWeight * 100 / totalWeight, matched, conflicts
}

func selectorKeyWeight(key string) int {
	switch key {
	case "app.kubernetes.io/instance", "app.kubernetes.io/name":
		return 4
	case "app":
		return 3
	case "component", "app.kubernetes.io/component":
		return 2
	default:
		return 1
	}
}

func systemIdentity(attributes map[string]any) string {
	labels := stringMap(attributes["labels"])
	for _, key := range []string{"kubechan.io/system", "app.kubernetes.io/part-of", "app.kubernetes.io/instance"} {
		if labels[key] != "" {
			return key + "=" + labels[key]
		}
	}
	return ""
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

func mapValue(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		n, _ := strconv.Atoi(fmt.Sprint(value))
		return n
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
