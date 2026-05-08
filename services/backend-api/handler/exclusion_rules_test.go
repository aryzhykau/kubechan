// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

func TestExclusionRules_List_Empty_ER(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	w := httptest.NewRecorder()
	h.List(w, httptest.NewRequest(http.MethodGet, "/api/v1/exclusion-rules", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []exclusionRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestExclusionRules_List_WithRules(t *testing.T) {
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Enabled: true, Description: "test rule"},
	}
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(rule).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	w := httptest.NewRecorder()
	h.List(w, httptest.NewRequest(http.MethodGet, "/api/v1/exclusion-rules", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []exclusionRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 || result[0].Name != "rule1" {
		t.Errorf("unexpected list: %+v", result)
	}
}

func TestExclusionRules_Create_WithSelector_Success(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]any{
		"name": "my-rule",
		"spec": map[string]any{
			"description": "disable crashloop for old pods",
			"targetResources": []map[string]any{
				{"kind": "Pod", "name": "legacy-app"},
			},
		},
	})
	w := httptest.NewRecorder()
	h.Create(w, httptest.NewRequest(http.MethodPost, "/api/v1/exclusion-rules", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body)
	}
}

func TestExclusionRules_Create_MissingDescription_ER(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]any{
		"name": "my-rule",
		"spec": map[string]any{
			"targetResources": []map[string]any{{"kind": "Pod"}},
		},
	})
	w := httptest.NewRecorder()
	h.Create(w, httptest.NewRequest(http.MethodPost, "/api/v1/exclusion-rules", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestExclusionRules_Create_MissingName(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]any{
		"name": "",
		"spec": map[string]any{
			"description":     "some description",
			"targetResources": []map[string]any{{"kind": "Pod"}},
		},
	})
	w := httptest.NewRecorder()
	h.Create(w, httptest.NewRequest(http.MethodPost, "/api/v1/exclusion-rules", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestExclusionRules_Create_NoTargets(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]any{
		"name": "my-rule",
		"spec": map[string]any{"description": "desc"},
	})
	w := httptest.NewRecorder()
	h.Create(w, httptest.NewRequest(http.MethodPost, "/api/v1/exclusion-rules", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestExclusionRules_SetEnabled_NotFound(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]bool{"enabled": false})
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/exclusion-rules/no-such", bytes.NewReader(body))
	r = withChiParam(r, "name", "no-such")
	w := httptest.NewRecorder()
	h.SetEnabled(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExclusionRules_SetEnabled_Success(t *testing.T) {
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Enabled: true, Description: "rule"},
	}
	k8s := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(rule).
		Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(map[string]bool{"enabled": false})
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/exclusion-rules/rule1", bytes.NewReader(body))
	r = withChiParam(r, "name", "rule1")
	w := httptest.NewRecorder()
	h.SetEnabled(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body)
	}
}

func TestExclusionRules_Delete_NotFound_ER(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/exclusion-rules/no-such", nil)
	r = withChiParam(r, "name", "no-such")
	w := httptest.NewRecorder()
	h.Delete(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExclusionRules_Delete_Success_ER(t *testing.T) {
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Enabled: true},
	}
	k8s := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(rule).
		Build()
	h := &ExclusionRules{K8s: k8s, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/exclusion-rules/rule1", nil)
	r = withChiParam(r, "name", "rule1")
	w := httptest.NewRecorder()
	h.Delete(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
