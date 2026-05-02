// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens the SQLite database at the given path, applies pending migrations,
// and returns the *sql.DB ready for use.
func Open(path string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db at %s: %w", path, err)
	}

	// SQLite single-writer; WAL mode for better read concurrency.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting pragmas: %w", err)
	}

	if err := runMigrations(db, logger); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// runMigrations applies all *.sql files under migrations/ in lexicographic order.
// Applied migrations are tracked in the schema_migrations table.
func runMigrations(db *sql.DB, logger *slog.Logger) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, filename := range files {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, filename).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", filename, err)
		}
		if count > 0 {
			continue // already applied
		}

		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", filename, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("applying migration %s: %w", filename, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(filename) VALUES (?)`, filename); err != nil {
			return fmt.Errorf("recording migration %s: %w", filename, err)
		}
		logger.Info("applied migration", "file", filename)
	}
	return nil
}

// Ping checks whether the database is reachable.
func Ping(db *sql.DB) error {
	return db.Ping()
}

// SettingGet reads a single setting value by key.
func SettingGet(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SettingSet updates a setting value by key.
func SettingSet(db *sql.DB, key, value string) error {
	_, err := db.Exec(
		`UPDATE settings SET value = ?, updated_at = datetime('now') WHERE key = ?`,
		value, key,
	)
	return err
}

// SettingsAll returns all settings as a map[key]value.
func SettingsAll(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}
