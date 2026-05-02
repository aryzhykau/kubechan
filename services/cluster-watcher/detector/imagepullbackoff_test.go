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

func TestImagePullBackOffDetector(t *testing.T) {
	d := &detector.ImagePullBackOffDetector{}
	ctx := context.Background()
	reader := fake.NewClientBuilder().Build()

	cases := []struct {
		name           string
		reason         string
		expectSymptoms int
		severity       string
	}{
		{"ImagePullBackOff", "ImagePullBackOff", 1, "high"},
		{"ErrImagePull", "ErrImagePull", 1, "high"},
		{"Running (no symptom)", "", 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := corev1.ContainerStatus{Name: "app", Image: "bad:tag"}
			if tc.reason != "" {
				cs.State = corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: tc.reason},
				}
			} else {
				cs.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default"},
				Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{cs}},
			}
			symptoms, err := d.Evaluate(ctx, pod, reader)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(symptoms) != tc.expectSymptoms {
				t.Fatalf("expected %d symptoms, got %d", tc.expectSymptoms, len(symptoms))
			}
			if tc.expectSymptoms > 0 && symptoms[0].Severity != tc.severity {
				t.Errorf("expected severity %q, got %q", tc.severity, symptoms[0].Severity)
			}
		})
	}

	t.Run("fires on init container ErrImagePull", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-init", Namespace: "default"},
			Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{
					Name:  "init",
					Image: "missing:latest",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"},
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
}
