// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// RecoverPendingRequests re-marks any analysis_requests that were left in
// 'pending' state (e.g. from a previous crash) as 'dispatched' and logs them
// so the llm-gateway stub can be wired in Phase 3B.
//
// This runs synchronously before the HTTP server starts, ensuring no concurrent
// dispatch race (SQLite single-writer; server not yet accepting traffic).
func RecoverPendingRequests(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, incident_id, diagnostic_run_id FROM analysis_requests WHERE status = 'pending'`,
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var ids []struct{ id, incidentID, diagnosticRunID string }
	for rows.Next() {
		var id string
		var incidentID, diagnosticRunID sql.NullString
		if err := rows.Scan(&id, &incidentID, &diagnosticRunID); err != nil {
			return err
		}
		ids = append(ids, struct{ id, incidentID, diagnosticRunID string }{
			id:              id,
			incidentID:      incidentID.String,
			diagnosticRunID: diagnosticRunID.String,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, req := range ids {
		logger.Info("recovering pending analysis_request",
			"id", req.id,
			"incidentId", req.incidentID,
			"diagnosticRunId", req.diagnosticRunID,
		)
		// Phase 3B: dispatch to llm-gateway here.
		// For now just mark as dispatched to avoid re-processing on next restart.
		_, err := db.ExecContext(ctx,
			`UPDATE analysis_requests SET status = 'dispatched', dispatched_at = ? WHERE id = ? AND status = 'pending'`,
			time.Now().UTC().Format(time.RFC3339), req.id,
		)
		if err != nil {
			logger.Error("failed to update recovered request", "id", req.id, "error", err)
		}
	}

	if len(ids) > 0 {
		logger.Info("startup recovery complete", "recovered", len(ids))
	}
	return nil
}
