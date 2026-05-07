// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ── Context keys ──────────────────────────────────────────────────────────────

type ctxKeyUserID struct{}
type ctxKeyUsername struct{}
type ctxKeyRole struct{}

// UserFromCtx extracts the authenticated user fields injected by RequireAuth.
func UserFromCtx(ctx context.Context) (userID, username, role string) {
	userID, _ = ctx.Value(ctxKeyUserID{}).(string)
	username, _ = ctx.Value(ctxKeyUsername{}).(string)
	role, _ = ctx.Value(ctxKeyRole{}).(string)
	return
}

// ── JWT helpers ───────────────────────────────────────────────────────────────

// jwtSecret returns the signing key from the JWT_SECRET env var.
// The server must already have validated this is non-empty at startup.
func jwtSecret() []byte {
	return []byte(os.Getenv("JWT_SECRET"))
}

func jwtTTL() time.Duration {
	h, err := strconv.Atoi(os.Getenv("JWT_TTL_HOURS"))
	if err != nil || h <= 0 {
		h = 24
	}
	return time.Duration(h) * time.Hour
}

func signToken(userID, username, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":      userID,
		"username": username,
		"role":     role,
		"exp":      time.Now().Add(jwtTTL()).Unix(),
		"iat":      time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

func parseToken(raw string) (userID, username, role string, err error) {
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret(), nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		return "", "", "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", "", errors.New("invalid token claims")
	}

	userID, _ = claims["sub"].(string)
	username, _ = claims["username"].(string)
	role, _ = claims["role"].(string)
	return
}

// ── Middleware ────────────────────────────────────────────────────────────────

// RequireAuth validates the Bearer JWT and injects user fields into context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "missing authentication token")
			return
		}

		userID, username, role, err := parseToken(raw)
		if err != nil || userID == "" {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)
		ctx = context.WithValue(ctx, ctxKeyUsername{}, username)
		ctx = context.WithValue(ctx, ctxKeyRole{}, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin validates JWT and additionally checks that role == "admin".
func RequireAdmin(next http.Handler) http.Handler {
	return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, role := UserFromCtx(r.Context())
		if role != "admin" {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// ValidateJWTSecret checks the JWT_SECRET env var at startup and returns an error
// if it is missing or too short.
func ValidateJWTSecret() error {
	s := os.Getenv("JWT_SECRET")
	if len(s) < 32 {
		return errors.New("JWT_SECRET must be at least 32 bytes; set it via the kubechan-jwt-secret K8s Secret")
	}
	return nil
}

// ── Auth handler ─────────────────────────────────────────────────────────────

// Auth holds dependencies for authentication endpoints.
type Auth struct {
	DB     *sql.DB
	Logger *slog.Logger
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string `json:"token"`
	Role     string `json:"role"`
	Username string `json:"username"`
}

// Login handles POST /api/v1/auth/login (public).
func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	var id, hash, role string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash, role FROM users WHERE username = ?`, req.Username,
	).Scan(&id, &hash, &role)

	// Constant-time path: always run bcrypt even if user not found to prevent
	// timing-based username enumeration.
	if err != nil {
		// Run a dummy compare so the timing is similar regardless.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.burn.time.for.timing.safety."), []byte(req.Password))
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := signToken(id, req.Username, role)
	if err != nil {
		h.Logger.Error("signing JWT", "error", err)
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token, Role: role, Username: req.Username})
}

// Me handles GET /api/v1/auth/me (requires auth).
func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	userID, username, role := UserFromCtx(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":   userID,
		"username": username,
		"role":     role,
	})
}

// ── Users handler ─────────────────────────────────────────────────────────────

// Users holds dependencies for user management endpoints (admin-only).
type Users struct {
	DB *sql.DB
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type userResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

// Create handles POST /api/v1/users (admin-only).
func (h *Users) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Role != "admin" && req.Role != "viewer" {
		writeError(w, http.StatusBadRequest, "role must be 'admin' or 'viewer'")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	id := uuid.New().String()
	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO users (id, username, password_hash, role) VALUES (?, ?, ?, ?)`,
		id, req.Username, string(hash), req.Role,
	)
	if err != nil {
		// SQLite UNIQUE constraint violation on username.
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "username already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, userResponse{ID: id, Username: req.Username, Role: req.Role})
}

// List handles GET /api/v1/users (admin-only).
func (h *Users) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, username, role, created_at FROM users ORDER BY created_at ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	var users []userResponse
	for rows.Next() {
		var u userResponse
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []userResponse{}
	}
	writeJSON(w, http.StatusOK, users)
}

// Delete handles DELETE /api/v1/users/{id} (admin-only).
func (h *Users) Delete(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, "user id is required")
		return
	}

	// Prevent deleting the last admin.
	var adminCount int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE role = 'admin'`,
	).Scan(&adminCount); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var targetRole string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT role FROM users WHERE id = ?`, targetID,
	).Scan(&targetRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if targetRole == "admin" && adminCount <= 1 {
		writeError(w, http.StatusConflict, "cannot delete the last admin user")
		return
	}

	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
