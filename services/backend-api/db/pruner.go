// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"time"
)

// StartPruner runs the retention pruner in a goroutine.
// It fires once at startup and then every 6 hours.
func StartPruner(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		prune(ctx, db, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				prune(ctx, db, logger)
			}
		}
	}()
}

func prune(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	evidenceDays := readSettingInt(db, "evidence.retention_days", 7)
	analysisDays := readSettingInt(db, "analysis.retention_days", 30)

	res, err := db.ExecContext(ctx,
		`DELETE FROM evidence WHERE created_at < datetime('now', ? || ' days')`,
		strconv.Itoa(-evidenceDays),
	)
	logPruneResult(logger, "evidence", res, err)

	res, err = db.ExecContext(ctx,
		`DELETE FROM analysis_results WHERE created_at < datetime('now', ? || ' days')`,
		strconv.Itoa(-analysisDays),
	)
	logPruneResult(logger, "analysis_results", res, err)

	res, err = db.ExecContext(ctx,
		`DELETE FROM analysis_requests WHERE completed_at IS NOT NULL
		  AND completed_at < datetime('now', ? || ' days')`,
		strconv.Itoa(-analysisDays),
	)
	logPruneResult(logger, "analysis_requests", res, err)
}

func readSettingInt(db *sql.DB, key string, fallback int) int {
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func logPruneResult(logger *slog.Logger, table string, res sql.Result, err error) {
	if err != nil {
		logger.Error("pruner delete failed", "table", table, "error", err)
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		logger.Info("pruner deleted rows", "table", table, "rows", n)
	}
}
