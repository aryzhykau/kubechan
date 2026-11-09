package startup

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	backenddb "github.com/org/kubechan/services/backend-api/db"
)

func TestRecoverAgentAttemptsPersistsInterruptedTerminalState(t *testing.T) {
	database, err := backenddb.Open(filepath.Join(t.TempDir(), "agent.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if _, err = database.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status) VALUES ('request', 'run', 'in_progress')`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO analysis_attempts(id, analysis_request_id, diagnostic_run_id, evidence_id, status, stage, created_at, updated_at) VALUES ('attempt', 'request', 'run', 'evidence', 'running', 'get_logs', '', '')`); err != nil {
		t.Fatal(err)
	}
	if err = RecoverAgentAttempts(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if err = RecoverAgentAttempts(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	var state, requestState, event string
	var count int
	_ = database.QueryRow(`SELECT status FROM analysis_attempts`).Scan(&state)
	_ = database.QueryRow(`SELECT status FROM analysis_requests`).Scan(&requestState)
	_ = database.QueryRow(`SELECT payload FROM analysis_progress`).Scan(&event)
	_ = database.QueryRow(`SELECT COUNT(*) FROM analysis_progress`).Scan(&count)
	if state != "interrupted" || requestState != "failed" || count != 1 || !strings.Contains(event, "backend_restarted") {
		t.Fatalf("state=%s request=%s events=%d event=%s", state, requestState, count, event)
	}
}

func TestRecoverAgentPreparationAfterEvidenceBeforeDispatch(t *testing.T) {
	database, err := backenddb.Open(filepath.Join(t.TempDir(), "preparation.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if _, err = database.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status, analysis_mode) VALUES ('request', 'run', 'in_progress', 'snapshot-agent-v1')`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO evidence(id, diagnostic_run_id, problem_case_id, collected_at, collector_version, log_truncated, payload, payload_bytes) VALUES ('evidence', 'run', '', '', '', 0, '{}', 2)`); err != nil {
		t.Fatal(err)
	}
	if err = RecoverAgentAttempts(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = database.QueryRow(`SELECT status FROM analysis_attempts WHERE analysis_request_id = 'request'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" {
		t.Fatalf("status=%s", status)
	}
}
