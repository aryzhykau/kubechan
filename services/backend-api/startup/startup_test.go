// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	backenddb "github.com/org/kubechan/services/backend-api/db"
)

func newStartupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	return s
}

func openTestDB(t *testing.T) interface {
	Close() error
} {
	t.Helper()
	path := filepath.Join(t.TempDir(), "startup_test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ── RecoverPendingRequests ────────────────────────────────────────────────────

func TestRecoverPendingRequests_NoPending(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "recover.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := RecoverPendingRequests(context.Background(), db, logger); err != nil {
		t.Errorf("RecoverPendingRequests() error = %v", err)
	}
}

func TestRecoverPendingRequests_WithPending(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "recover2.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert a pending analysis_request.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO analysis_requests (id, incident_id, diagnostic_run_id, requested_at, status)
		 VALUES ('req-1', 'inc-1', 'run-1', datetime('now'), 'pending')`,
	)
	if err != nil {
		t.Fatalf("insert pending request: %v", err)
	}

	if err := RecoverPendingRequests(context.Background(), db, logger); err != nil {
		t.Fatalf("RecoverPendingRequests() error = %v", err)
	}

	// Verify it was marked as dispatched.
	var status string
	if err := db.QueryRowContext(context.Background(),
		`SELECT status FROM analysis_requests WHERE id = 'req-1'`,
	).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "dispatched" {
		t.Errorf("status = %q, want dispatched", status)
	}
}

// ── EnsureAdminUser ───────────────────────────────────────────────────────────

func TestEnsureAdminUser_SkipsIfAdminExists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "admin1.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Pre-insert an admin user.
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO users (id, username, password_hash, role) VALUES ('uid-1', 'admin', '$2a$12$dummy', 'admin')`,
	)
	if err != nil {
		t.Fatalf("insert admin: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(newStartupScheme()).Build()
	if err := EnsureAdminUser(context.Background(), db, c, "kubechan", logger); err != nil {
		t.Errorf("EnsureAdminUser() error = %v", err)
	}
}

func TestEnsureAdminUser_UsesExistingSecret(t *testing.T) {
	// bcrypt is involved — skip parallel to keep test suite fast.
	path := filepath.Join(t.TempDir(), "admin2.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Create a pre-existing secret with a password.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminSecretName,
			Namespace: "kubechan",
		},
		Data: map[string][]byte{
			"password": []byte("secure-test-password-123"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(newStartupScheme()).WithObjects(secret).Build()

	if err := EnsureAdminUser(context.Background(), db, c, "kubechan", logger); err != nil {
		t.Fatalf("EnsureAdminUser() error = %v", err)
	}

	// Verify admin user was created.
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM users WHERE role = 'admin'`,
	).Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Errorf("admin count = %d, want 1", count)
	}
}

func TestEnsureAdminUser_CreatesSecretWhenMissing(t *testing.T) {
	// bcrypt is involved — skip parallel.
	path := filepath.Join(t.TempDir(), "admin3.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := backenddb.Open(path, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	c := fake.NewClientBuilder().WithScheme(newStartupScheme()).Build()

	if err := EnsureAdminUser(context.Background(), db, c, "kubechan", logger); err != nil {
		t.Fatalf("EnsureAdminUser() error = %v", err)
	}

	// Verify admin user and secret were created.
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM users WHERE role = 'admin'`,
	).Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Errorf("admin count = %d, want 1", count)
	}

	// Verify K8s secret was created.
	createdSecret := &corev1.Secret{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: "kubechan", Name: adminSecretName}, createdSecret); err != nil {
		t.Errorf("secret not created: %v", err)
	}
}
