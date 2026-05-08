// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── MarkFalsePositive ─────────────────────────────────────────────────────────

func TestIncidents_MarkFalsePositive_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/missing-inc")
	w := httptest.NewRecorder()
	h.MarkFalsePositive(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIncidents_MarkFalsePositive_AutoIncident(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-auto", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
	}
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/inc-auto")
	w := httptest.NewRecorder()
	h.MarkFalsePositive(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (only manual incidents allowed)", w.Code)
	}
}

func TestIncidents_MarkFalsePositive_ManualIncident_Success(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-manual", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "manual"},
		Status: v1alpha1.IncidentStatus{
			State:    v1alpha1.IncidentStateOpen,
			OpenedAt: &now,
		},
	}
	// DB is nil so checkManualIncidentAccess returns true (no DB check).
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/inc-manual")
	w := httptest.NewRecorder()
	h.MarkFalsePositive(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ── Resolve ───────────────────────────────────────────────────────────────────

func TestIncidents_Resolve_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/missing")
	w := httptest.NewRecorder()
	h.Resolve(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIncidents_Resolve_AutoIncident_Success(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-res", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status: v1alpha1.IncidentStatus{
			State:    v1alpha1.IncidentStateOpen,
			OpenedAt: &now,
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/inc-res")
	w := httptest.NewRecorder()
	h.Resolve(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestIncidents_Resolve_AlreadyResolved_Idempotent(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-resolved", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status: v1alpha1.IncidentStatus{
			State:      v1alpha1.IncidentStateResolved,
			OpenedAt:   &now,
			ResolvedAt: &now,
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(inc).
		Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/inc-resolved")
	w := httptest.NewRecorder()
	h.Resolve(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (idempotent)", w.Code)
	}
}

// ── checkManualIncidentAccess ─────────────────────────────────────────────────

func TestIncidents_checkManualIncidentAccess_NilDB_ReturnsTrue(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	if !h.checkManualIncidentAccess(w, r, "any-incident") {
		t.Error("expected checkManualIncidentAccess to return true when DB is nil")
	}
}
