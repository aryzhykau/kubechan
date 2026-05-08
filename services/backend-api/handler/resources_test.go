// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResources_ListNamespaces_Empty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
	w := httptest.NewRecorder()
	h.ListNamespaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestResources_ListNamespaces_FiltersControlPlane(t *testing.T) {
	t.Parallel()
	nsList := []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "kube-public"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "my-app"}},
	}
	objs := make([]interface{ DeepCopyObject() interface{ runtime() } }, 0)
	_ = objs

	// Build with namespace objects.
	builder := fake.NewClientBuilder().WithScheme(newTestScheme())
	for i := range nsList {
		builder = builder.WithObjects(&nsList[i])
	}
	c := builder.Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
	w := httptest.NewRecorder()
	h.ListNamespaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// "default" and "my-app" should appear; control-plane ones filtered.
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if nameSet["kube-system"] {
		t.Error("kube-system should be filtered out")
	}
	if !nameSet["default"] {
		t.Error("default should be included")
	}
	if !nameSet["my-app"] {
		t.Error("my-app should be included")
	}
}
