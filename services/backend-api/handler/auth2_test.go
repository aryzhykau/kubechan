// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// ── Auth.Login success path ───────────────────────────────────────────────────

func TestAuth_Login_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "supersecretkeythatisatleast32chars!!!")
	db := openDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := &Auth{DB: db, Logger: logger}

	// Create a user manually.
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), 4) // cost=4 for speed
	_, _ = db.Exec(
		`INSERT INTO users(id, username, password_hash, role) VALUES ('uid-login', 'loginuser', ?, 'admin')`,
		string(hash),
	)

	body, _ := json.Marshal(loginRequest{Username: "loginuser", Password: "password123"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Login(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp loginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.Role != "admin" {
		t.Errorf("role = %q, want admin", resp.Role)
	}
}

func TestAuth_Login_WrongPassword(t *testing.T) {
	t.Setenv("JWT_SECRET", "supersecretkeythatisatleast32chars!!!")
	db := openDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := &Auth{DB: db, Logger: logger}

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), 4)
	_, _ = db.Exec(
		`INSERT INTO users(id, username, password_hash, role) VALUES ('uid-wp', 'wpuser', ?, 'admin')`,
		string(hash),
	)

	body, _ := json.Marshal(loginRequest{Username: "wpuser", Password: "wrongpassword"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Login(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── Users.Delete ─────────────────────────────────────────────────────────────

func TestUsers_Delete_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	// Create two admins, then delete one.
	_, _ = db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES ('admin1','admin1','','admin')`)
	_, _ = db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES ('admin2','admin2','','admin')`)

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/users/admin1", nil)
	r = withChiParam(r, "id", "admin1")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestUsers_Delete_LastAdmin_Rejected(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	// Only one admin.
	_, _ = db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES ('only-admin','oadmin','','admin')`)

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/users/only-admin", nil)
	r = withChiParam(r, "id", "only-admin")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestUsers_Delete_MissingID(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/users/", nil)
	// Don't add chi param → empty ID.
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Users.List with data ──────────────────────────────────────────────────────

func TestUsers_List_WithData(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	_, _ = db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES ('u1','user1','','viewer')`)
	_, _ = db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES ('u2','user2','','admin')`)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var users []userResponse
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestUsers_List_EmptyReturnsArray(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	h.List(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var users []userResponse
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected empty array, got %d", len(users))
	}
}

func TestUsers_Delete_NotFound_Auth2(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Users{DB: db}

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/users/nonexistent", nil)
	r = withChiParam(r, "id", "nonexistent")
	w := httptest.NewRecorder()
	h.Delete(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
