// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

func TestDiagnosticRuns_List_NoFilter(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticRuns_GetEvidence_Found(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(
		`INSERT INTO evidence(id, diagnostic_run_id, problem_case_id, incident_id, collected_at, collector_version, payload, payload_bytes)
		 VALUES ('ev-1', 'run-ev-1', 'pc-1', NULL, '2024-01-01T00:00:00Z', 'v1', '{"log":"test"}', 13)`,
	)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "run-ev-1")
	w := httptest.NewRecorder()
	h.GetEvidence(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticRuns_GetAnalysisResult_Found(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(
		`INSERT INTO analysis_results(id, diagnostic_run_id, model, status, likely_root_cause, confidence, payload)
		 VALUES ('ar-1', 'run-ar-1', 'gpt-4', 'completed', 'CPU overload', 0.95, '{}')`,
	)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "run-ar-1")
	w := httptest.NewRecorder()
	h.GetAnalysisResult(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticRuns_Get_Found_WithEvidence(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-get-with-evidence", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(dr).
		Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/dr-get-with-evidence")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestDiagnosticRuns_List_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestDiagnosticRuns_List_WithRows(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, incident_id, requested_at, status) VALUES ('ar-list-1', 'run-list-1', 'inc-1', '2024-01-01T00:00:00Z', 'completed')`)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/?incidentId=inc-1", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}

func TestDiagnosticRuns_List_NoIncidentFilter(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, incident_id, requested_at, status) VALUES ('ar-list-2', 'run-list-2', 'inc-2', '2024-01-01T00:00:00Z', 'pending')`)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) == 0 {
		t.Errorf("expected at least 1 item")
	}
}

// ── Delete ─────────────────────────────────────────────────────────────────────

func TestDiagnosticRuns_Delete_WithData(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, incident_id, requested_at, status) VALUES ('ar-del-1', 'run-del-1', 'inc-del', '2024-01-01T00:00:00Z', 'pending')`)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "id", "run-del-1")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticRuns_Delete_NotFound_Returns204(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "id", "nonexistent-run")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	// Deleting non-existent is still 204 (idempotent)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

// ── BulkDelete ────────────────────────────────────────────────────────────────

func TestDiagnosticRuns_BulkDelete_EmptyBody(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.BulkDelete(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDiagnosticRuns_BulkDelete_WithData(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	_, _ = db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, incident_id, requested_at, status) VALUES ('ar-bulk-1', 'run-bulk-1', 'inc-b', '2024-01-01T00:00:00Z', 'pending')`)
	_, _ = db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, incident_id, requested_at, status) VALUES ('ar-bulk-2', 'run-bulk-2', 'inc-b', '2024-01-01T00:00:00Z', 'pending')`)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body := `{"ids":["run-bulk-1","run-bulk-2"]}`
	r := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.BulkDelete(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["deleted"] != 2 {
		t.Errorf("expected deleted=2, got %d", result["deleted"])
	}
}
