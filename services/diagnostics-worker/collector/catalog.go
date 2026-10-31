// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BuildSourceCatalog normalizes the legacy evidence payload into atomic facts
// with deterministic IDs. IDs are stable for the lifetime of an evidence
// snapshot and are the only values an LLM may return as citations.
func BuildSourceCatalog(ev *Evidence) []EvidenceSource {
	var out []EvidenceSource
	root := ev.RootResource
	for _, event := range ev.RootResourceEvents {
		out = append(out, eventSource(root, event, ev.CollectedAt))
	}
	for _, pc := range ev.ProblemCases {
		ref := pc.AffectedResource
		for _, event := range pc.Events {
			out = append(out, eventSource(ref, event, ev.CollectedAt))
		}
		out = append(out, logSources(ref, "", pc.Logs, pc.LogsTruncated, ev.CollectedAt)...)
	}
	for _, pod := range ev.WorkloadPodLogs {
		ref := ResourceRef{Kind: "Pod", Name: pod.PodName, Namespace: root.Namespace}
		out = append(out, fieldSource(ref, "/status/phase", pod.Phase, ev.CollectedAt))
		for _, event := range pod.Events {
			out = append(out, eventSource(ref, event, ev.CollectedAt))
		}
		out = append(out, logSources(ref, "default", pod.Logs, pod.Truncated, ev.CollectedAt)...)
		if pod.Dependencies != nil {
			for _, cm := range pod.Dependencies.ConfigMaps {
				cmRef := ResourceRef{Kind: "ConfigMap", Name: cm.Name, Namespace: root.Namespace}
				out = append(out, fieldSource(cmRef, "/exists", !cm.Missing, ev.CollectedAt))
				keys := make([]string, 0, len(cm.Data))
				for key := range cm.Data {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					out = append(out, fieldSource(cmRef, "/data/"+escapeJSONPointer(key), cm.Data[key], ev.CollectedAt))
				}
			}
			for _, secret := range pod.Dependencies.Secrets {
				secretRef := ResourceRef{Kind: "Secret", Name: secret.Name, Namespace: root.Namespace}
				out = append(out, EvidenceSource{ID: sourceID("field", secretRef, "/exists", strconv.FormatBool(!secret.Missing)), Type: "resource_field", Resource: secretRef, CollectedAt: ev.CollectedAt, Path: "/exists", Value: !secret.Missing, Redacted: true})
			}
		}
	}
	for _, pvc := range ev.PVCInfos {
		ref := ResourceRef{Kind: "PersistentVolumeClaim", Name: pvc.Name, Namespace: root.Namespace}
		out = append(out, fieldSource(ref, "/status/phase", pvc.Phase, ev.CollectedAt))
		if pvc.StorageClass != "" {
			out = append(out, fieldSource(ref, "/spec/storageClassName", pvc.StorageClass, ev.CollectedAt))
		}
		if pvc.RequestedStorage != "" {
			out = append(out, fieldSource(ref, "/spec/resources/requests/storage", pvc.RequestedStorage, ev.CollectedAt))
		}
		for _, event := range pvc.Events {
			out = append(out, eventSource(ref, event, ev.CollectedAt))
		}
	}
	for _, related := range ev.RelatedResourceEvidence {
		flattenFields(&out, related.Resource, "", related.Spec, ev.CollectedAt)
		for _, event := range related.Events {
			out = append(out, eventSource(related.Resource, event, ev.CollectedAt))
		}
		out = append(out, logSources(related.Resource, "", related.Logs, false, ev.CollectedAt)...)
	}
	return dedupeSources(out)
}

func fieldSource(ref ResourceRef, path string, value any, at time.Time) EvidenceSource {
	return EvidenceSource{ID: sourceID("field", ref, path, fmt.Sprint(value)), Type: "resource_field", Resource: ref, CollectedAt: at, Path: path, Value: value}
}

func eventSource(ref ResourceRef, event K8sEvent, at time.Time) EvidenceSource {
	key := event.Reason + "|" + event.Message + "|" + event.FirstTime.UTC().Format(time.RFC3339)
	copy := event
	return EvidenceSource{ID: sourceID("event", ref, "/events", key), Type: "event", Resource: ref, CollectedAt: at, Event: &copy}
}

func logSources(ref ResourceRef, container, logs string, truncated bool, at time.Time) []EvidenceSource {
	if strings.TrimSpace(logs) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(logs, "\n"), "\n")
	out := make([]EvidenceSource, 0, len(lines))
	for i, text := range lines {
		line := EvidenceLogLine{Number: i + 1, Text: text}
		path := fmt.Sprintf("/logs/%s/current/%d", escapeJSONPointer(container), i+1)
		out = append(out, EvidenceSource{ID: sourceID("log", ref, path, text), Type: "log_line", Resource: ref, CollectedAt: at, Path: path, Line: &line, Truncated: truncated})
	}
	return out
}

func flattenFields(out *[]EvidenceSource, ref ResourceRef, path string, value any, at time.Time) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenFields(out, ref, path+"/"+escapeJSONPointer(key), typed[key], at)
		}
	case []any:
		for i, item := range typed {
			flattenFields(out, ref, path+"/"+strconv.Itoa(i), item, at)
		}
	case []map[string]any:
		// Kind-specific collectors naturally build typed slices of maps. They
		// are not assignable to []any in Go, so handle them explicitly to keep
		// every nested Kubernetes field addressable by an atomic JSON pointer.
		for i, item := range typed {
			flattenFields(out, ref, path+"/"+strconv.Itoa(i), item, at)
		}
	default:
		if path != "" {
			*out = append(*out, fieldSource(ref, path, typed, at))
		}
	}
}

func sourceID(kind string, ref ResourceRef, path, value string) string {
	raw := strings.Join([]string{kind, ref.APIGroup, ref.Kind, ref.Namespace, ref.Name, path, value}, "|")
	sum := sha256.Sum256([]byte(raw))
	return kind + ":" + hex.EncodeToString(sum[:8])
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func dedupeSources(sources []EvidenceSource) []EvidenceSource {
	seen := map[string]bool{}
	out := make([]EvidenceSource, 0, len(sources))
	for _, source := range sources {
		if seen[source.ID] {
			continue
		}
		seen[source.ID] = true
		out = append(out, source)
	}
	return out
}
