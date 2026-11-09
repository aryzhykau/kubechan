-- Agent execution state is separate from immutable evidence and Assessment v2.
ALTER TABLE analysis_requests ADD COLUMN analysis_mode TEXT NOT NULL DEFAULT 'single-shot';

CREATE TABLE analysis_attempts (
    id TEXT PRIMARY KEY,
    analysis_request_id TEXT NOT NULL REFERENCES analysis_requests(id) ON DELETE CASCADE,
    diagnostic_run_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    snapshot_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    stage TEXT NOT NULL,
    reason_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_attempt_run ON analysis_attempts(diagnostic_run_id, created_at);
CREATE UNIQUE INDEX idx_attempt_active ON analysis_attempts(analysis_request_id) WHERE status = 'running';

CREATE TABLE analysis_progress (
    attempt_id TEXT NOT NULL REFERENCES analysis_attempts(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    event_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    PRIMARY KEY (attempt_id, seq),
    UNIQUE (attempt_id, event_id)
);
