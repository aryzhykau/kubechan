// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// ── Open / migrations ─────────────────────────────────────────────────────────

func TestOpen_CreatesMigratedDB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify schema_migrations table was created and migrations applied.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query error: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one migration to be recorded")
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Open twice — second open should not re-apply migrations or error.
	db1, err := Open(path, logger)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	_ = db1.Close()

	db2, err := Open(path, logger)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer func() { _ = db2.Close() }()
}

// ── Ping ──────────────────────────────────────────────────────────────────────

func TestPing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ping.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := Ping(db); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

// ── SettingGet / SettingSet / SettingsAll ─────────────────────────────────────

func openSettingsDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSettingGet_ExistingKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sg.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// Seed a known key.
	if _, err := db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES (?, ?)`, "test.key", "hello"); err != nil {
		t.Fatalf("seed error: %v", err)
	}

	val, err := SettingGet(db, "test.key")
	if err != nil {
		t.Fatalf("SettingGet() error = %v", err)
	}
	if val != "hello" {
		t.Errorf("SettingGet() = %q, want %q", val, "hello")
	}
}

func TestSettingGet_MissingKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sg2.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = SettingGet(db, "nonexistent.key")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestSettingSet(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ss.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// Seed key first.
	if _, err := db.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES (?, ?)`, "update.me", "old"); err != nil {
		t.Fatalf("seed error: %v", err)
	}

	if err := SettingSet(db, "update.me", "new"); err != nil {
		t.Fatalf("SettingSet() error = %v", err)
	}

	val, err := SettingGet(db, "update.me")
	if err != nil {
		t.Fatalf("SettingGet() after set error = %v", err)
	}
	if val != "new" {
		t.Errorf("after SettingSet, value = %q, want %q", val, "new")
	}
}

func TestSettingsAll(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sa.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// Migrations seed default settings; just verify we get a non-empty map.
	all, err := SettingsAll(db)
	if err != nil {
		t.Fatalf("SettingsAll() error = %v", err)
	}
	if len(all) == 0 {
		t.Error("expected settings to be seeded by migrations")
	}
}

// ── Pruner ────────────────────────────────────────────────────────────────────

func TestPruner_RunsWithoutError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pruner.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	// prune on empty tables should be a no-op without error.
	prune(t.Context(), db, logger)
}
