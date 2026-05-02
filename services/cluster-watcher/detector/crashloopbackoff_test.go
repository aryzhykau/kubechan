// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/org/kubechan/services/cluster-watcher/detector"
)

func TestCrashLoopBackOffDetector(t *testing.T) {
	d := &detector.CrashLoopBackOffDetector{}
	ctx := context.Background()
	reader := fake.NewClientBuilder().Build()

	t.Run("fires on CrashLoopBackOff container", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: "default"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "app",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
					RestartCount: 5,
				}},
			},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Fatalf("expected 1 symptom, got %d", len(symptoms))
		}
		if symptoms[0].Severity != "critical" {
			t.Errorf("expected severity critical, got %q", symptoms[0].Severity)
		}
	})

	t.Run("fires on CrashLoopBackOff init container", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "crash-pod-init", Namespace: "default"},
			Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{
					Name: "init",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				}},
			},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Fatalf("expected 1 symptom, got %d", len(symptoms))
		}
	})

	t.Run("no symptom for healthy pod", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "ok-pod", Namespace: "default"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "app",
					Ready: true,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				}},
			},
		}
		symptoms, err := d.Evaluate(ctx, pod, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms, got %d", len(symptoms))
		}
	})

	t.Run("no symptom for non-pod object", func(t *testing.T) {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc"}}
		symptoms, err := d.Evaluate(ctx, svc, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for non-pod, got %d", len(symptoms))
		}
	})
}
