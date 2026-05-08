// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// withAdminCtx injects an admin user into the request context (simulates RequireAuth).
func withAdminCtx(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)
	ctx = context.WithValue(ctx, ctxKeyUsername{}, "testadmin")
	ctx = context.WithValue(ctx, ctxKeyRole{}, "admin")
	return r.WithContext(ctx)
}

// withViewerCtx injects a viewer user into the request context.
func withViewerCtx(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)
	ctx = context.WithValue(ctx, ctxKeyUsername{}, "testviewer")
	ctx = context.WithValue(ctx, ctxKeyRole{}, "viewer")
	return r.WithContext(ctx)
}

// ── Create - success paths ────────────────────────────────────────────────────

func TestManualIncident_Create_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.DiagnosticRun{}).
		Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	// Create a user so FK constraint on analysis_requests.triggered_by is satisfied.
	adminID := "user-test-001"
	insertTestUser(t, db, adminID)

	body, _ := json.Marshal(manualIncidentRequest{
		ResourceKind: "Deployment",
		ResourceName: "myapp",
		Namespace:    "default",
		UserMessage:  "Application is experiencing serious issues",
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withAdminCtx(r, adminID)
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp manualIncidentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IncidentID == "" {
		t.Error("expected non-empty incidentId")
	}
	if resp.DiagnosticRunID == "" {
		t.Error("expected non-empty diagnosticRunId")
	}
}

func TestManualIncident_Create_WithRelatedResources(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.DiagnosticRun{}).
		Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	adminID := "user-test-002"
	insertTestUser(t, db, adminID)

	body, _ := json.Marshal(manualIncidentRequest{
		ResourceKind: "Deployment",
		ResourceName: "myapp",
		Namespace:    "default",
		UserMessage:  "Application is experiencing issues right now",
		RelatedResources: []relatedResourceIn{
			{Kind: "Service", Name: "myapp-svc"},
			{Kind: "", Name: "should-be-skipped"}, // missing kind → skipped
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withAdminCtx(r, adminID)
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestManualIncident_Create_MissingUserMessage(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(manualIncidentRequest{
		ResourceKind: "Deployment",
		ResourceName: "myapp",
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestManualIncident_Create_NoNamespace_UsesDefault(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.DiagnosticRun{}).
		Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	adminID := "user-test-003"
	insertTestUser(t, db, adminID)

	body, _ := json.Marshal(manualIncidentRequest{
		ResourceKind: "Deployment",
		ResourceName: "myapp",
		// Namespace deliberately empty
		UserMessage: "A longish user message for manual incident creation",
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withAdminCtx(r, adminID)
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}


