// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/org/kubechan/services/cluster-watcher/detector"
)

func serviceNoEndpointsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("adding clientgo scheme: %v", err)
	}
	return s
}

func boolPtr(b bool) *bool { return &b }

func TestServiceNoEndpointsDetector(t *testing.T) {
	d := &detector.ServiceNoEndpointsDetector{}
	ctx := context.Background()

	t.Run("fires when ClusterIP service has no ready endpoints", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.1"},
		}
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "empty-svc-abc",
				Namespace: "default",
				Labels:    map[string]string{"kubernetes.io/service-name": "empty-svc"},
			},
			Endpoints: []discoveryv1.Endpoint{{
				Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(false)},
			}},
		}

		reader := fake.NewClientBuilder().
			WithScheme(serviceNoEndpointsScheme(t)).
			WithObjects(es).
			Build()

		symptoms, err := d.Evaluate(ctx, svc, reader)
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

	t.Run("no symptom when service has a ready endpoint", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ok-svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.2"},
		}
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ok-svc-xyz",
				Namespace: "default",
				Labels:    map[string]string{"kubernetes.io/service-name": "ok-svc"},
			},
			Endpoints: []discoveryv1.Endpoint{{
				Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(true)},
			}},
		}

		reader := fake.NewClientBuilder().
			WithScheme(serviceNoEndpointsScheme(t)).
			WithObjects(es).
			Build()

		symptoms, err := d.Evaluate(ctx, svc, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms, got %d", len(symptoms))
		}
	})

	t.Run("fires when no EndpointSlice exists", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "no-slice", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.3"},
		}
		reader := fake.NewClientBuilder().
			WithScheme(serviceNoEndpointsScheme(t)).
			Build()

		symptoms, err := d.Evaluate(ctx, svc, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 1 {
			t.Errorf("expected 1 symptom when no endpoint slice, got %d", len(symptoms))
		}
	})

	t.Run("no symptom for headless service", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "headless", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "None"},
		}
		reader := fake.NewClientBuilder().
			WithScheme(serviceNoEndpointsScheme(t)).
			Build()

		symptoms, err := d.Evaluate(ctx, svc, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for headless service, got %d", len(symptoms))
		}
	})

	t.Run("no symptom for NodePort service", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "nodeport", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, ClusterIP: "10.0.0.4"},
		}
		reader := fake.NewClientBuilder().
			WithScheme(serviceNoEndpointsScheme(t)).
			Build()

		symptoms, err := d.Evaluate(ctx, svc, reader)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(symptoms) != 0 {
			t.Errorf("expected 0 symptoms for NodePort service, got %d", len(symptoms))
		}
	})
}
