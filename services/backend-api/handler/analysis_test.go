// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── Analysis.Analyze ──────────────────────────────────────────────────────────

func TestAnalysis_Analyze_IncidentNotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = withChiParam(r, "id", "kubechan/no-such-incident")
	w := httptest.NewRecorder()
	h.Analyze(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAnalysis_Analyze_AutoIncident_CreatesRun(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	t.Setenv("JWT_TTL_HOURS", "1")

	db := openDB(t)
	insertTestUser(t, db, "uid-1")
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-auto", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	token, _ := signToken("uid-1", "alice", "admin")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r = withChiParam(r, "id", "kubechan/inc-auto")
	// Inject user context manually via RequireAuth.
	var capturedW, capturedR = httptest.NewRecorder(), (*http.Request)(nil)
	RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedW = w.(*httptest.ResponseRecorder)
		capturedR = req
	})).ServeHTTP(capturedW, r)

	if capturedR == nil {
		t.Fatal("RequireAuth did not call next handler")
	}
	capturedR = withChiParam(capturedR, "id", "kubechan/inc-auto")

	w := httptest.NewRecorder()
	h.Analyze(w, capturedR)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202; body: %s", w.Code, w.Body.String())
	}

	var resp analyzeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AnalysisRequestID == "" {
		t.Error("expected non-empty analysisRequestId")
	}
}

// ── Analysis.GetAnalysisResult ────────────────────────────────────────────────

func TestAnalysis_GetAnalysisResult_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "non-existent-result-id")
	w := httptest.NewRecorder()
	h.GetAnalysisResult(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Analysis.GetEvidence ──────────────────────────────────────────────────────

func TestAnalysis_GetEvidence_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/no-evidence-here")
	w := httptest.NewRecorder()
	h.GetEvidence(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Analysis.RateAnalysisResult ───────────────────────────────────────────────

func TestAnalysis_RateAnalysisResult_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	r = withChiParam(r, "id", "some-id")
	w := httptest.NewRecorder()
	h.RateAnalysisResult(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnalysis_RateAnalysisResult_InvalidRating(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(map[string]string{"rating": "meh"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withChiParam(r, "id", "some-id")
	w := httptest.NewRecorder()
	h.RateAnalysisResult(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnalysis_RateAnalysisResult_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Analysis{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(map[string]string{"rating": "up"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withChiParam(r, "id", "non-existent-result-id")
	w := httptest.NewRecorder()
	h.RateAnalysisResult(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
