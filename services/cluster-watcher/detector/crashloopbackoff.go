// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CrashLoopBackOffDetector fires when any container (init or regular) is in CrashLoopBackOff.
type CrashLoopBackOffDetector struct{}

func (d *CrashLoopBackOffDetector) Name() string { return "CrashLoopBackOff" }

func (d *CrashLoopBackOffDetector) Evaluate(_ context.Context, obj client.Object, _ client.Reader) ([]Symptom, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, nil
	}

	var symptoms []Symptom
	check := func(cs corev1.ContainerStatus) {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			symptoms = append(symptoms, Symptom{
				Message:  fmt.Sprintf("container %q is in CrashLoopBackOff (restarts: %d)", cs.Name, cs.RestartCount),
				Severity: "critical",
			})
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		check(cs)
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		check(cs)
	}
	return symptoms, nil
}
