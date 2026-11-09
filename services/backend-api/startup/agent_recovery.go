package startup

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RecoverAgentAttempts settles lost executions. It never replays a paid request
// or mixes context from an interrupted attempt into another investigation.
func RecoverAgentAttempts(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id, analysis_request_id, diagnostic_run_id FROM analysis_attempts WHERE status = 'running'`)
	if err != nil {
		return err
	}
	type lostAttempt struct{ id, requestID, runID string }
	lost := []lostAttempt{}
	for rows.Next() {
		var attempt lostAttempt
		if err = rows.Scan(&attempt.id, &attempt.requestID, &attempt.runID); err != nil {
			_ = rows.Close()
			return err
		}
		lost = append(lost, attempt)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// A process can exit after evidence acceptance but before its goroutine has
	// created an attempt. Persist that interrupted preparation too.
	orphans, err := tx.QueryContext(ctx, `SELECT ar.id, ar.diagnostic_run_id, ev.id FROM analysis_requests ar JOIN evidence ev ON ev.id = (SELECT id FROM evidence WHERE diagnostic_run_id = ar.diagnostic_run_id ORDER BY created_at LIMIT 1) WHERE ar.analysis_mode = 'snapshot-agent-v1' AND ar.status = 'in_progress' AND NOT EXISTS (SELECT 1 FROM analysis_attempts a WHERE a.analysis_request_id = ar.id) AND NOT EXISTS (SELECT 1 FROM analysis_results res WHERE res.diagnostic_run_id = ar.diagnostic_run_id)`)
	if err != nil {
		return err
	}
	type preparation struct{ requestID, runID, evidenceID string }
	preparations := []preparation{}
	for orphans.Next() {
		var pending preparation
		if err = orphans.Scan(&pending.requestID, &pending.runID, &pending.evidenceID); err != nil {
			_ = orphans.Close()
			return err
		}
		preparations = append(preparations, pending)
	}
	if err = orphans.Err(); err != nil {
		_ = orphans.Close()
		return err
	}
	_ = orphans.Close()
	for _, pending := range preparations {
		id := uuid.NewString()
		if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_attempts(id, analysis_request_id, diagnostic_run_id, evidence_id, status, stage, created_at, updated_at) VALUES (?, ?, ?, ?, 'running', 'queued', ?, ?)`, id, pending.requestID, pending.runID, pending.evidenceID, now, now); err != nil {
			return err
		}
		lost = append(lost, lostAttempt{id, pending.requestID, pending.runID})
	}
	for _, attempt := range lost {
		if _, err = tx.ExecContext(ctx, `UPDATE analysis_attempts SET status = 'interrupted', stage = 'interrupted', reason_code = 'backend_restarted', updated_at = ? WHERE id = ? AND status = 'running'`, now, attempt.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE analysis_requests SET status = 'failed', completed_at = ? WHERE id = ?`, now, attempt.requestID); err != nil {
			return err
		}
		var seq int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM analysis_progress WHERE attempt_id = ?`, attempt.id).Scan(&seq); err != nil {
			return err
		}
		event, _ := json.Marshal(map[string]any{"version": 1, "type": "Analysis.Progress", "runId": attempt.runID, "diagnosticRunId": attempt.runID, "analysisRequestId": attempt.requestID, "attemptId": attempt.id, "seq": seq, "stage": "interrupted", "phase": "failed", "reasonCode": "backend_restarted", "occurredAt": now, "recordedAt": now})
		if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_progress(attempt_id, seq, event_id, payload) VALUES (?, ?, 'terminal', ?)`, attempt.id, seq, string(event)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
