// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/org/kubechan/services/cluster-watcher/detector"
)

func TestPendingTooLongDetector(t *testing.T) {
	ctx := context.Background()
	reader := fake.NewClientBuilder().Build()
	threshold := 5 * time.Minute
	d := &detector.PendingTooLongDetector{Threshold: func() time.Duration { return threshold }}

	t.Run("fires when pending beyond threshold", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "stuck",
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Fatalf("expected 1 symptom, got %d", len(symptoms))
		}
		if symptoms[0].Severity != "medium" {
			t.Errorf("expected severity medium, got %q", symptoms[0].Severity)
		}
	})

	t.Run("no symptom when pending within threshold", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "new-pod",
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms, got %d", len(symptoms))
		}
	})

	t.Run("no symptom when pod is running", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "running-pod",
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for running pod, got %d", len(symptoms))
		}
	})

	t.Run("uses default threshold when Threshold is nil", func(t *testing.T) {
		dDefault := &detector.PendingTooLongDetector{} // nil threshold → default 5m
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "slow-pod",
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-6 * time.Minute)),
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		}
		symptoms, err := dDefault.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Errorf("expected 1 symptom with default threshold, got %d", len(symptoms))
		}
	})
}
