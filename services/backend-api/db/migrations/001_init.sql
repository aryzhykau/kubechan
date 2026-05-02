-- Migration 001: Initial schema

CREATE TABLE evidence (
    id                  TEXT PRIMARY KEY,
    diagnostic_run_id   TEXT NOT NULL,
    problem_case_id     TEXT NOT NULL,
    incident_id         TEXT,
    collected_at        DATETIME NOT NULL,
    collector_version   TEXT NOT NULL,
    log_truncated       INTEGER NOT NULL DEFAULT 0,
    payload             TEXT NOT NULL,
    payload_bytes       INTEGER NOT NULL,
    redaction_summary   TEXT,
    log_truncation_info TEXT,
    collection_errors   TEXT,
    created_at          DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_evidence_problem_case   ON evidence(problem_case_id);
CREATE INDEX idx_evidence_diagnostic_run ON evidence(diagnostic_run_id);
CREATE INDEX idx_evidence_incident       ON evidence(incident_id);
CREATE INDEX idx_evidence_created_at     ON evidence(created_at);

CREATE TABLE analysis_results (
    id                       TEXT PRIMARY KEY,
    problem_case_id          TEXT,
    incident_id              TEXT,
    diagnostic_run_id        TEXT NOT NULL,
    model                    TEXT NOT NULL,
    model_runtime            TEXT NOT NULL DEFAULT 'external',
    status                   TEXT NOT NULL,
    likely_root_cause        TEXT,
    confidence               REAL,
    consistency_check_status TEXT,
    has_styled_message       INTEGER NOT NULL DEFAULT 0,
    thinking_budget_used     INTEGER,
    error_message            TEXT,
    payload                  TEXT NOT NULL,
    created_at               DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_ar_problem_case ON analysis_results(problem_case_id);
CREATE INDEX idx_ar_incident     ON analysis_results(incident_id);
CREATE INDEX idx_ar_created_at   ON analysis_results(created_at);
CREATE INDEX idx_ar_status       ON analysis_results(status);

CREATE TABLE analysis_requests (
    id                TEXT PRIMARY KEY,
    problem_case_id   TEXT,
    incident_id       TEXT,
    diagnostic_run_id TEXT NOT NULL,
    requested_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    status            TEXT NOT NULL DEFAULT 'pending',
    dispatched_at     DATETIME,
    completed_at      DATETIME
);
CREATE INDEX idx_areq_status          ON analysis_requests(status);
CREATE INDEX idx_areq_problem_case    ON analysis_requests(problem_case_id);
CREATE INDEX idx_areq_incident        ON analysis_requests(incident_id);
CREATE INDEX idx_areq_diagnostic_run  ON analysis_requests(diagnostic_run_id);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO settings(key, value) VALUES
    ('persona.enabled',            'false'),
    ('persona.idle_chatter',       'false'),
    ('persona.idle_interval_secs', '300'),
    ('bedrock.model_id',           '"qwen3-32b"'),
    ('bedrock.region',             '"us-east-1"'),
    ('bedrock.thinking_budget',    '0'),
    ('evidence.retention_days',    '7'),
    ('analysis.retention_days',    '30');
