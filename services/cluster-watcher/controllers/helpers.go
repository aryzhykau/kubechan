// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"fmt"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/detector"
)

// severityOrder maps severity names to a numeric weight (higher = more severe).
var severityOrder = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
}

// highestSeverity returns the most severe ProblemCaseSeverity from a slice of symptoms.
// Falls back to "low" if the slice is empty or contains unknown values.
func highestSeverity(symptoms []detector.Symptom) v1alpha1.ProblemCaseSeverity {
	best := 0
	result := v1alpha1.ProblemCaseSeverity("low")
	for _, s := range symptoms {
		if w := severityOrder[s.Severity]; w > best {
			best = w
			result = v1alpha1.ProblemCaseSeverity(s.Severity)
		}
	}
	return result
}

// debounceKey builds a stable debounce key for a specific resource + detector pair.
func debounceKey(namespace, kind, name, detectorName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", namespace, kind, name, detectorName)
}
