// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultPendingThreshold = 5 * time.Minute

// PendingTooLongDetector fires when a Pod has been in Pending phase longer than Threshold.
type PendingTooLongDetector struct {
	// Threshold is the maximum time a Pod may remain Pending before a symptom is raised.
	// Defaults to 5 minutes if zero.
	Threshold time.Duration
}

func (d *PendingTooLongDetector) Name() string { return "PendingTooLong" }

func (d *PendingTooLongDetector) Evaluate(_ context.Context, obj client.Object, _ client.Reader) ([]Symptom, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, nil
	}
	if pod.Status.Phase != corev1.PodPending {
		return nil, nil
	}

	threshold := d.Threshold
	if threshold == 0 {
		threshold = defaultPendingThreshold
	}

	age := time.Since(pod.CreationTimestamp.Time)
	if age < threshold {
		return nil, nil
	}

	return []Symptom{{
		Message:  fmt.Sprintf("pod has been Pending for %s (threshold: %s)", age.Round(time.Second), threshold),
		Severity: "medium",
	}}, nil
}
