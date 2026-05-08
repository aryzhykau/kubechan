// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helpers for logPruneResult tests
var errFake = errors.New("fake error")

type fakeResult int64

func (f fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeResult) RowsAffected() (int64, error) { return int64(f), nil }

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
func TestReadSettingInt_ExistingKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rsi.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, _ = db.Exec(`INSERT INTO settings(key, value) VALUES ('test.days', '42')`)
	got := readSettingInt(db, "test.days", 7)
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestReadSettingInt_MissingKey_ReturnsFallback(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rsi2.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	got := readSettingInt(db, "no.such.key", 99)
	if got != 99 {
		t.Errorf("got %d, want 99 (fallback)", got)
	}
}

func TestReadSettingInt_InvalidValue_ReturnsFallback(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rsi3.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, _ = db.Exec(`INSERT INTO settings(key, value) VALUES ('bad.val', 'notanumber')`)
	got := readSettingInt(db, "bad.val", 5)
	if got != 5 {
		t.Errorf("got %d, want 5 (fallback for invalid)", got)
	}
}

func TestLogPruneResult_WithError(t *testing.T) {
	t.Parallel()
	// Should not panic when err is non-nil.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	logPruneResult(logger, "evidence", nil, errFake)
}

func TestLogPruneResult_Success_NoRows(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// nil res with no error — RowsAffected will return 0.
	logPruneResult(logger, "evidence", fakeResult(0), nil)
}

func TestLogPruneResult_Success_WithRows(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	logPruneResult(logger, "evidence", fakeResult(3), nil)
}

func TestStartPruner_CancelledContext_StopsGracefully(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sp.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	StartPruner(ctx, db, logger)
	time.Sleep(20 * time.Millisecond) // let it fire once
	cancel()                           // signal stop
	time.Sleep(20 * time.Millisecond) // goroutine should exit
}