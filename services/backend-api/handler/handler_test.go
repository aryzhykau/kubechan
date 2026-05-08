// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	backenddb "github.com/org/kubechan/services/backend-api/db"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// withChiParam injects a chi URL parameter into the request context.
func withChiParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── helpers.go ────────────────────────────────────────────────────────────────

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestWriteError(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "bad input")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["error"] != "bad input" {
		t.Errorf("error = %q, want %q", body["error"], "bad input")
	}
}

func TestNamespacedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id      string
		defNS   string
		wantNS  string
		wantName string
	}{
		{"ns/name", "default", "ns", "name"},
		{"just-name", "default", "default", "just-name"},
		{"a/b/c", "default", "a", "b/c"},
	}
	for _, tc := range tests {
		ns, name := namespacedName(tc.id, tc.defNS)
		if ns != tc.wantNS || name != tc.wantName {
			t.Errorf("namespacedName(%q, %q) = (%q, %q), want (%q, %q)",
				tc.id, tc.defNS, ns, name, tc.wantNS, tc.wantName)
		}
	}
}

// ── ProblemCases ──────────────────────────────────────────────────────────────

func TestProblemCases_List_Empty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ProblemCases{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/problemcases", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProblemCases_List_Filtered(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "kubechan"},
		Spec: v1alpha1.ProblemCaseSpec{
			Severity: v1alpha1.SeverityHigh,
		},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(pc).Build()
	h := &ProblemCases{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/problemcases?severity=high", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProblemCases_Get_Found(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(pc).Build()
	h := &ProblemCases{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/pc-1")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestProblemCases_Get_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ProblemCases{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/missing")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── ExclusionRules ────────────────────────────────────────────────────────────

func TestExclusionRules_List_Empty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/exclusion-rules", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp []exclusionRuleResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d items", len(resp))
	}
}

func TestExclusionRules_Create_Success(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(createExclusionRuleRequest{
		Name: "my-rule",
		Spec: v1alpha1.ExclusionRuleSpec{
			Description: "test rule",
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Name: "app"},
			},
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/exclusion-rules", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestExclusionRules_Create_MissingDescription(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(createExclusionRuleRequest{
		Name: "my-rule",
		Spec: v1alpha1.ExclusionRuleSpec{
			TargetResources: []v1alpha1.ResourceRef{{Kind: "Deployment", Name: "app"}},
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestExclusionRules_Create_MissingTargetAndSelector(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(createExclusionRuleRequest{
		Name: "my-rule",
		Spec: v1alpha1.ExclusionRuleSpec{Description: "ok"},
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestExclusionRules_Create_InvalidJSON(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	h.Create(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestExclusionRules_Delete_Success(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "del-rule", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(rule).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "name", "del-rule")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestExclusionRules_Delete_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "name", "missing-rule")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestExclusionRules_SetEnabled(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "toggle-rule", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Enabled: true},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(rule).Build()
	h := &ExclusionRules{K8s: c, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(setEnabledRequest{Enabled: false})
	r := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body))
	r = withChiParam(r, "name", "toggle-rule")
	w := httptest.NewRecorder()
	h.SetEnabled(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Verify the rule was patched.
	var updated v1alpha1.KubechanExclusionRule
	if err := c.Get(r.Context(), client.ObjectKey{Namespace: "kubechan", Name: "toggle-rule"}, &updated); err != nil {
		t.Fatalf("Get after patch: %v", err)
	}
	if updated.Spec.Enabled {
		t.Error("expected Enabled=false after patch")
	}
}

// ── Settings ──────────────────────────────────────────────────────────────────

func TestSettings_Get(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sqldb, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	h := &Settings{DB: sqldb}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected at least one setting from migration seeds")
	}
}

func TestSettings_Update_Success(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings2.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sqldb, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	h := &Settings{DB: sqldb}
	body, _ := json.Marshal(map[string]any{"evidence.retention_days": 14})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestSettings_Update_UnknownKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings3.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sqldb, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	h := &Settings{DB: sqldb}
	body, _ := json.Marshal(map[string]any{"unknown.key": "value"})
	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSettings_Update_InvalidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings4.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sqldb, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	h := &Settings{DB: sqldb}
	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSettings_IdleMessage(t *testing.T) {
	t.Parallel()
	h := &Settings{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.IdleMessage(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
}

// ── UserFromCtx ───────────────────────────────────────────────────────────────

func TestUserFromCtx_Empty(t *testing.T) {
	t.Parallel()
	userID, username, role := UserFromCtx(t.Context())
	if userID != "" || username != "" || role != "" {
		t.Errorf("expected empty context values, got (%q, %q, %q)", userID, username, role)
	}
}
