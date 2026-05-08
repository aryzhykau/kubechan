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

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	backenddb "github.com/org/kubechan/services/backend-api/db"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── JWT / Auth ────────────────────────────────────────────────────────────────

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertTestUser inserts a minimal user row so FK constraints on users(id) are satisfied.
func insertTestUser(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO users(id, username, password_hash, role) VALUES (?, ?, '', 'admin')`,
		userID, userID,
	)
	if err != nil {
		t.Fatalf("insertTestUser: %v", err)
	}
}

func TestJWTSecret_Default(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	s := jwtSecret()
	if s == nil {
		t.Error("jwtSecret() returned nil")
	}
}

func TestJWTTTL_Default(t *testing.T) {
	t.Setenv("JWT_TTL_HOURS", "")
	ttl := jwtTTL()
	if ttl.Hours() != 24 {
		t.Errorf("default TTL = %v, want 24h", ttl)
	}
}

func TestJWTTTL_Custom(t *testing.T) {
	t.Setenv("JWT_TTL_HOURS", "48")
	ttl := jwtTTL()
	if ttl.Hours() != 48 {
		t.Errorf("TTL = %v, want 48h", ttl)
	}
}

func TestValidateJWTSecret_TooShort(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")
	if err := ValidateJWTSecret(); err == nil {
		t.Error("expected error for short JWT_SECRET")
	}
}

func TestValidateJWTSecret_Valid(t *testing.T) {
	t.Setenv("JWT_SECRET", "this-is-a-very-long-jwt-secret-12345678")
	if err := ValidateJWTSecret(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSignAndParseToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	t.Setenv("JWT_TTL_HOURS", "1")

	token, err := signToken("user-1", "alice", "admin")
	if err != nil {
		t.Fatalf("signToken() error = %v", err)
	}

	gotUserID, gotUsername, gotRole, err := parseToken(token)
	if err != nil {
		t.Fatalf("parseToken() error = %v", err)
	}
	if gotUserID != "user-1" || gotUsername != "alice" || gotRole != "admin" {
		t.Errorf("parsed = (%q, %q, %q), want (user-1, alice, admin)", gotUserID, gotUsername, gotRole)
	}
}

func TestParseToken_Invalid(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	_, _, _, err := parseToken("not.a.jwt")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestRequireAuth_MissingToken(t *testing.T) {
	t.Parallel()
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	t.Setenv("JWT_TTL_HOURS", "1")

	token, err := signToken("uid-1", "alice", "admin")
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	called := false
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		uid, uname, role := UserFromCtx(r.Context())
		if uid != "uid-1" || uname != "alice" || role != "admin" {
			t.Errorf("context: got (%q, %q, %q), want (uid-1, alice, admin)", uid, uname, role)
		}
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !called {
		t.Error("next handler was not called")
	}
}

func TestRequireAdmin_NonAdmin(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	t.Setenv("JWT_TTL_HOURS", "1")

	token, _ := signToken("uid-2", "bob", "viewer")

	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// ── Auth.Login ────────────────────────────────────────────────────────────────

func TestAuth_Login_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Auth{DB: db, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("bad"))
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAuth_Login_MissingFields(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Auth{DB: db, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	body, _ := json.Marshal(loginRequest{Username: "alice"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAuth_Login_UnknownUser(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Auth{DB: db, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	body, _ := json.Marshal(loginRequest{Username: "ghost", Password: "password123"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── Auth.Me ───────────────────────────────────────────────────────────────────

func TestAuth_Me(t *testing.T) {
	t.Parallel()
	h := &Auth{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.Me(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Users ─────────────────────────────────────────────────────────────────────

func TestUsers_List_Empty(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestUsers_Create_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("bad"))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUsers_Create_MissingUsername(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	body, _ := json.Marshal(createUserRequest{Password: "password123", Role: "viewer"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUsers_Create_ShortPassword(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	body, _ := json.Marshal(createUserRequest{Username: "alice", Password: "short", Role: "viewer"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUsers_Create_InvalidRole(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	body, _ := json.Marshal(createUserRequest{Username: "alice", Password: "password123", Role: "superuser"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUsers_Create_Success(t *testing.T) {
	// bcrypt is slow, so we skip t.Parallel() to avoid polluting test timing.
	db := openDB(t)
	h := &Users{DB: db}
	body, _ := json.Marshal(createUserRequest{Username: "newuser", Password: "password123", Role: "viewer"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestUsers_Create_Duplicate(t *testing.T) {
	db := openDB(t)
	h := &Users{DB: db}

	body, _ := json.Marshal(createUserRequest{Username: "dupe", Password: "password123", Role: "viewer"})
	makeReq := func() *http.Request {
		return httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	}

	w1 := httptest.NewRecorder()
	h.Create(w1, makeReq())
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", w1.Code)
	}

	body2, _ := json.Marshal(createUserRequest{Username: "dupe", Password: "password456", Role: "viewer"})
	w2 := httptest.NewRecorder()
	h.Create(w2, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body2)))
	if w2.Code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want 409", w2.Code)
	}
}

func TestUsers_Delete_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}
	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r = withChiParam(r, "id", "non-existent-uuid")
	w := httptest.NewRecorder()
	h.Delete(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Incidents ─────────────────────────────────────────────────────────────────

func TestIncidents_List_Empty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestIncidents_List_WithItems(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestIncidents_Get_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/missing")
	w := httptest.NewRecorder()
	h.Get(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIncidents_Get_AutoIncident(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-auto", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{Source: "auto"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(inc).Build()
	h := &Incidents{K8s: c, DB: nil, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/inc-auto")
	w := httptest.NewRecorder()
	h.Get(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ── DiagnosticRuns ────────────────────────────────────────────────────────────

func TestDiagnosticRuns_Get_NotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/missing-run")
	w := httptest.NewRecorder()
	h.Get(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDiagnosticRuns_Get_Found(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dr).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "kubechan/run-1")
	w := httptest.NewRecorder()
	h.Get(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticRuns_List_Empty(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &DiagnosticRuns{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/diagnosticruns", nil)
	w := httptest.NewRecorder()
	h.List(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Internal.ReceiveEvidence ──────────────────────────────────────────────────

func TestInternal_ReceiveEvidence_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("bad"))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternal_ReceiveEvidence_MissingRunID(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	body, _ := json.Marshal(evidenceRequest{})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestInternal_ReceiveEvidence_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		LLMGatewayURL:    "", // no async dispatch
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	body, _ := json.Marshal(evidenceRequest{
		DiagnosticRunID: "run-abc",
		Payload:         []byte(`{"test":"data"}`),
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

// ── nullStr / nullBytes ───────────────────────────────────────────────────────

func TestNullStr(t *testing.T) {
	t.Parallel()
	if nullStr("") != nil {
		t.Error("nullStr(\"\") should return nil")
	}
	if nullStr("hello") != "hello" {
		t.Errorf("nullStr(\"hello\") = %v, want \"hello\"", nullStr("hello"))
	}
}

func TestNullBytes(t *testing.T) {
	t.Parallel()
	if nullBytes(nil) != nil {
		t.Error("nullBytes(nil) should return nil")
	}
	if nullBytes([]byte("null")) != nil {
		t.Error("nullBytes(\"null\") should return nil")
	}
	if nullBytes([]byte("data")) != "data" {
		t.Errorf("nullBytes(\"data\") = %v, want \"data\"", nullBytes([]byte("data")))
	}
}

// ── ManualIncident ────────────────────────────────────────────────────────────

func TestManualIncident_Create_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("bad"))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestManualIncident_Create_MissingKind(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(manualIncidentRequest{ResourceName: "app", UserMessage: "something broke badly here"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestManualIncident_Create_MissingName(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(manualIncidentRequest{ResourceKind: "Deployment", UserMessage: "something broke badly here"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestManualIncident_Create_ShortMessage(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &ManualIncident{K8s: c, DB: db, DefaultNamespace: "kubechan"}
	body, _ := json.Marshal(manualIncidentRequest{ResourceKind: "Deployment", ResourceName: "app", UserMessage: "short"})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
