// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
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

// ── List ──────────────────────────────────────────────────────────────────────

func TestIncidents_List_Empty_WithDB(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	r = withAdminCtx(r, "admin-user-1")
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []incidentView
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 incidents, got %d", len(result))
	}
}

func TestIncidents_List_AutoIncidents_VisibleToAll(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now())
	inc1 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-auto-1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen, OpenedAt: &now},
	}
	inc2 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-auto-2", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc1, inc2).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	// viewer role — can still see auto incidents
	ctx := withAdminCtx(r, "viewer-user")
	ctx.Context()
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []incidentView
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(result))
	}
}

func TestIncidents_List_StateFilter(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now())
	inc1 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-open-1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen, OpenedAt: &now},
	}
	inc2 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-resolved-1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc1, inc2).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents?state=open", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []incidentView
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 open incident, got %d", len(result))
	}
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestIncidents_Get_Found(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-get-1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
	}
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/kubechan/inc-get-1", nil)
	r = withChiParam(r, "id", "kubechan/inc-get-1")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestIncidents_Get_NotFound_WithDB(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/kubechan/missing", nil)
	r = withChiParam(r, "id", "kubechan/missing")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIncidents_Get_ManualIncident_Admin_CanAccess(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-manual-get", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "manual"},
	}
	db := openDB(t)
	adminID := "admin-get-test"
	insertTestUser(t, db, adminID)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAdminCtx(r, adminID)
	r = withChiParam(r, "id", "kubechan/inc-manual-get")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ── checkManualIncidentAccess ─────────────────────────────────────────────────

func TestIncidents_checkManualIncidentAccess_Admin_ReturnsTrue(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAdminCtx(r, "admin-id")
	w := httptest.NewRecorder()
	if !h.checkManualIncidentAccess(w, r, "some-incident") {
		t.Error("expected admin to have access")
	}
}

func TestIncidents_checkManualIncidentAccess_NotManual_ReturnsTrue(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	// No manual_incident_owners row → auto incident → access granted
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// viewer role
	ctx := withViewerCtx(r, "viewer-id")
	w := httptest.NewRecorder()
	if !h.checkManualIncidentAccess(w, ctx, "auto-incident") {
		t.Error("expected viewer to access auto incident")
	}
}

func TestIncidents_checkManualIncidentAccess_ViewerCannotAccessOtherManual(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	ownerID := "owner-123"
	insertTestUser(t, db, ownerID)

	// Create manual_incident_owners row for a different owner
	_, _ = db.Exec(
		`INSERT INTO manual_incident_owners(incident_id, namespace, owner_id) VALUES ('manual-inc', 'kubechan', ?)`,
		ownerID,
	)

	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withViewerCtx(r, "different-viewer")
	w := httptest.NewRecorder()
	result := h.checkManualIncidentAccess(w, r, "manual-inc")
	if result {
		t.Error("expected viewer to be denied access to other's manual incident")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIncidents_List_ViewerOnlySeeOwnManualIncidents(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	ownerID := "inc-owner"
	viewerID := "inc-viewer"
	insertTestUser(t, db, ownerID)
	insertTestUser(t, db, viewerID)

	manualInc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-vis", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "manual"},
	}
	_, _ = db.Exec(
		`INSERT INTO manual_incident_owners(incident_id, namespace, owner_id) VALUES ('manual-vis', 'kubechan', ?)`,
		ownerID,
	)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(manualInc).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withViewerCtx(r, viewerID) // different viewer
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var incidents []incidentView
	if err := json.NewDecoder(w.Body).Decode(&incidents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Viewer should not see ownerID's manual incident
	if len(incidents) != 0 {
		t.Errorf("viewer should not see other's manual incidents, got %d", len(incidents))
	}
}

func TestIncidents_Resolve_ManualIncident_ViewerNoAccess(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	ownerID := "res-owner"
	viewerID := "res-viewer"
	insertTestUser(t, db, ownerID)
	insertTestUser(t, db, viewerID)

	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-res", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "manual"},
	}
	_, _ = db.Exec(
		`INSERT INTO manual_incident_owners(incident_id, namespace, owner_id) VALUES ('manual-res', 'kubechan', ?)`,
		ownerID,
	)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).WithStatusSubresource(&v1alpha1.Incident{}).Build()
	h := &Incidents{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "manual-res")
	r = withViewerCtx(r, viewerID)
	w := httptest.NewRecorder()
	h.Resolve(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
}
