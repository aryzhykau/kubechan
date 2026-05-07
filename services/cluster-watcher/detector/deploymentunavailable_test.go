// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/org/kubechan/services/cluster-watcher/detector"
)

func TestDeploymentUnavailableDetector(t *testing.T) {
	threshold := 5 * time.Minute
	d := &detector.DeploymentUnavailableDetector{Threshold: func() time.Duration { return threshold }}
	ctx := context.Background()
	reader := fake.NewClientBuilder().Build()

	replicas := func(n int32) *int32 { return &n }

	// oldCondition simulates a deployment that has been unavailable well past the threshold.
	oldCondition := []appsv1.DeploymentCondition{{
		Type:               appsv1.DeploymentAvailable,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
	}}
	// recentCondition simulates a deployment that just became unavailable (within threshold).
	recentCondition := []appsv1.DeploymentCondition{{
		Type:               appsv1.DeploymentAvailable,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-30 * time.Second)),
	}}

	t.Run("fires when all replicas unavailable past threshold", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(3)},
			Status: appsv1.DeploymentStatus{
				Replicas:            3,
				ReadyReplicas:       0,
				AvailableReplicas:   0,
				UnavailableReplicas: 3,
				Conditions:          oldCondition,
			},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Fatalf("expected 1 symptom, got %d", len(symptoms))
		}
		if symptoms[0].Severity != "high" {
			t.Errorf("expected severity high, got %q", symptoms[0].Severity)
		}
	})

	t.Run("fires when some replicas unavailable past threshold", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(3)},
			Status: appsv1.DeploymentStatus{
				Replicas:            3,
				ReadyReplicas:       2,
				AvailableReplicas:   2,
				UnavailableReplicas: 1,
				Conditions:          oldCondition,
			},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Fatalf("expected 1 symptom, got %d", len(symptoms))
		}
	})

	t.Run("no symptom when unavailable but within threshold", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "just-deployed", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(2)},
			Status: appsv1.DeploymentStatus{
				Replicas:            2,
				ReadyReplicas:       0,
				AvailableReplicas:   0,
				UnavailableReplicas: 2,
				Conditions:          recentCondition,
			},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms within threshold, got %d", len(symptoms))
		}
	})

	t.Run("no symptom when all replicas ready", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(3)},
			Status: appsv1.DeploymentStatus{
				Replicas:          3,
				ReadyReplicas:     3,
				AvailableReplicas: 3,
			},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms, got %d", len(symptoms))
		}
	})

	t.Run("no symptom when scaled to zero", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "scaled-down", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: replicas(0)},
			Status:     appsv1.DeploymentStatus{},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for scaled-to-zero deployment, got %d", len(symptoms))
		}
	})

	t.Run("no symptom for nil replicas spec (defaults to 1 — healthy)", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "nil-replicas", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: nil},
			Status: appsv1.DeploymentStatus{
				Replicas:          1,
				ReadyReplicas:     1,
				AvailableReplicas: 1,
			},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms, got %d", len(symptoms))
		}
	})

	t.Run("no symptom when status not yet populated (brand new deployment)", func(t *testing.T) {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "brand-new",
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-5 * time.Second)),
			},
			Spec:   appsv1.DeploymentSpec{Replicas: replicas(1)},
			Status: appsv1.DeploymentStatus{},
		}
		symptoms, err := d.Evaluate(ctx, deploy, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for brand-new deployment with no status, got %d", len(symptoms))
		}
	})
}
