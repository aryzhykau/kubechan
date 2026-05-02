# DB Architecture Design

## Owner
backend-api team.

## Must complete before
Phase 2B.2 (SQLite setup + migrations).

## Output
`services/backend-api/db/migrations/001_init.sql` — reviewed, tested against an empty SQLite file, and checked into the repo.

---

## Tasks (ordered)

### [DB.1] Validate query patterns against schema indexes (~1h)

Cross-reference every planned REST endpoint query (Phase 2B) against the proposed schema indexes. For each query, confirm an index covers the `WHERE` column.

| Query | Table | WHERE column | Covered by index? |
|-------|-------|-------------|-------------------|
| `GET /evidence?problem_case_id=` | evidence | problem_case_id | `idx_evidence_problem_case` ✓ |
| `GET /evidence?diagnostic_run_id=` | evidence | diagnostic_run_id | `idx_evidence_diagnostic_run` ✓ |
| Retention pruning | evidence | created_at | `idx_evidence_created_at` ✓ |
| `GET /analysisresults/:id` | analysis_results | id (PK) | PK ✓ |
| List results by problem | analysis_results | problem_case_id | `idx_ar_problem_case` ✓ |
| Retention pruning | analysis_results | created_at | `idx_ar_created_at` ✓ |
| Startup recovery | analysis_requests | status | `idx_areq_status` ✓ |
| Mark completed | analysis_requests | diagnostic_run_id | needs index — add `idx_areq_diagnostic_run` |
| `GET /settings` | settings | key (PK) | PK ✓ |

**Action**: add `CREATE INDEX idx_areq_diagnostic_run ON analysis_requests(diagnostic_run_id)` to migration.

---

### [DB.2] Decide evidence payload strategy (~1h)

Two options for the `evidence` table:

**Option A — Raw JSON blob only** (recommended for MVP)
- `payload TEXT NOT NULL` stores the full evidence JSON
- Queryable fields extracted as real columns: `collector_version TEXT`, `log_truncated INTEGER` (boolean)
- Rationale: avoids schema coupling between diagnostics-worker and backend-api; evidence structure can evolve without migrations; extracted scalar columns handle the only query needs

**Option B — Fully normalized evidence columns**
- Each evidence field gets its own column
- Rejected: tightly couples diagnostics-worker evidence schema to DB; every new collector requires a migration

**Decision**: use Option A. Add `collector_version TEXT NOT NULL` and `log_truncated INTEGER NOT NULL DEFAULT 0` as real columns alongside `payload TEXT NOT NULL`.

---

### [DB.3] Decide migration tooling (~1h)

Two options:

**Option A — `golang-migrate/migrate` with embedded FS** (recommended)
- `//go:embed migrations/*.sql` embeds all migration files
- `migrate.New("iofs://...", "sqlite3://...")` applies pending migrations at startup
- Schema version tracked in `schema_migrations` table (auto-managed by library)
- Adding migration 002: create `002_add_something.sql` — no code change needed

**Option B — Raw SQL executed at startup**
- `db.Exec(migrationSQL)` in `main.go`
- Simpler for MVP with a single migration; brittle for future migrations (no version tracking)
- Rejected: will need version tracking as soon as a second migration is needed

**Decision**: use `golang-migrate/migrate`. Add to `go.mod`: `github.com/golang-migrate/migrate/v4`.

---

### [DB.4] Decide ID generation strategy (~30min)

All four tables use `id TEXT PRIMARY KEY`.

**Decision**: server-side UUIDs using `github.com/google/uuid`:
```go
id := uuid.New().String()  // e.g., "550e8400-e29b-41d4-a716-446655440000"
```
- Consistent across all tables
- Deterministic in tests (mock `uuid.New` or pass ID as parameter)
- No dependency on SQLite `randomblob` or `hex` functions
- Add `github.com/google/uuid` to `go.mod`

---

### [DB.5] Confirm analysis_requests concurrency safety (~30min)

backend-api runs as a single replica (NFR from full-plan.md §11: "Single replica (MVP) — SQLite single-writer constraint").

SQLite serializes all writes through a single WAL writer. Therefore:

- `UPDATE analysis_requests SET status='dispatched' WHERE id=? AND status='pending'` is safe — no race condition between concurrent goroutines or restarts
- No `SELECT FOR UPDATE` equivalent needed (SQLite does not support it; WAL mode handles writer serialization)
- The startup recovery path (`status='pending'` query) runs before the HTTP server starts — no concurrent dispatch possible

**Decision**: single `UPDATE ... WHERE status='pending'` is sufficient. Document this assumption in code comment.

---

### [DB.6] Write 001_init.sql migration (~1h)

File: `services/backend-api/db/migrations/001_init.sql`

```sql
-- Migration 001: Initial schema

CREATE TABLE evidence (
    id                  TEXT PRIMARY KEY,
    diagnostic_run_id   TEXT NOT NULL,
    problem_case_id     TEXT NOT NULL,
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
CREATE INDEX idx_evidence_created_at     ON evidence(created_at);

CREATE TABLE analysis_results (
    id                       TEXT PRIMARY KEY,
    problem_case_id          TEXT NOT NULL,
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
CREATE INDEX idx_ar_created_at   ON analysis_results(created_at);
CREATE INDEX idx_ar_status       ON analysis_results(status);

CREATE TABLE analysis_requests (
    id                TEXT PRIMARY KEY,
    problem_case_id   TEXT NOT NULL,
    diagnostic_run_id TEXT NOT NULL,
    requested_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    status            TEXT NOT NULL DEFAULT 'pending',
    dispatched_at     DATETIME,
    completed_at      DATETIME
);
CREATE INDEX idx_areq_status          ON analysis_requests(status);
CREATE INDEX idx_areq_problem_case    ON analysis_requests(problem_case_id);
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
```

**Verify**: run `sqlite3 /tmp/test.db < 001_init.sql` — must complete with no errors.
Run `.schema` in sqlite3 shell — confirm all tables and indexes present.

---

### [DB.7] Document deviations from full-plan.md §10 (~30min)

Deviations to add to §11 Locked Decisions (or note in `001_init.sql` header comment):

| Deviation | Reason |
|-----------|--------|
| Added `collector_version TEXT NOT NULL` and `log_truncated INTEGER NOT NULL DEFAULT 0` as real columns on `evidence` | Avoids JSON parsing for the most common queries |
| Added `idx_areq_diagnostic_run` index on `analysis_requests.diagnostic_run_id` | Required by `POST /internal/analysis-result` dispatch path |
| Migration tooling: `golang-migrate/migrate` | Version-tracked migrations for future schema evolution |
| ID generation: `github.com/google/uuid` | Consistent across all tables; testable |

---

## Integration test entry point
Before Phase 2B begins:
1. `sqlite3 /tmp/kubechan_test.db < services/backend-api/db/migrations/001_init.sql` — exits 0
2. `sqlite3 /tmp/kubechan_test.db .schema` — all 4 tables and all 8 indexes present
3. `SELECT * FROM settings` — 8 default rows present
4. Run `go test ./services/backend-api/db/...` — migration applied via `golang-migrate` in test, schema verified
