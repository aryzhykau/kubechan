// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── GetEvidence ───────────────────────────────────────────────────────────────

func TestDiagnosticRuns_GetEvidence_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "no-such-run")
	w := httptest.NewRecorder()
	h.GetEvidence(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── GetAnalysisResult ─────────────────────────────────────────────────────────

func TestDiagnosticRuns_GetAnalysisResult_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "no-such-run")
	w := httptest.NewRecorder()
	h.GetAnalysisResult(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDiagnosticRuns_Delete_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	// Delete a non-existent run is still a no-error 204 (DB deletes nothing but succeeds).
	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "id", "some-run-id")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

// ── BulkDelete ────────────────────────────────────────────────────────────────

func TestDiagnosticRuns_BulkDelete_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	h.BulkDelete(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDiagnosticRuns_BulkDelete_EmptyIDs(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(bulkDeleteRequest{IDs: []string{}})
	r := httptest.NewRequest(http.MethodDelete, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.BulkDelete(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDiagnosticRuns_BulkDelete_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(bulkDeleteRequest{IDs: []string{"run-1", "run-2"}})
	r := httptest.NewRequest(http.MethodDelete, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.BulkDelete(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]int
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["deleted"] != 2 {
		t.Errorf("deleted = %d, want 2", resp["deleted"])
	}
}

// ── List with incidentId filter ───────────────────────────────────────────────

func TestDiagnosticRuns_List_WithIncidentFilter(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/?incidentId=inc-xyz", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
