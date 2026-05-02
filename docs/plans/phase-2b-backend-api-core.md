# Phase 2B — backend-api core

## Prerequisites (from other services)
- `api/v1alpha1` CRD types available (Phase 0.2)
- `POST /internal/evidence` JSON schema agreed with diagnostics-worker before task 2B.6
- DB schema finalized (see db-architecture-design.md) before task 2B.2
- cluster-watcher Phase 1 generating ProblemCase CRDs (needed for end-to-end testing)

---

## Tasks (ordered)

### [2B.1] HTTP server scaffold (~2h)
- File: `services/backend-api/main.go`
- `chi` router with middleware: `middleware.Logger` (structured log/slog), `middleware.Recoverer`, `middleware.RequestID`
- Graceful shutdown: `http.Server` with `Shutdown(ctx)` on `SIGTERM`/`SIGINT`
- Port: configurable via env var `PORT` (default `8080`)
- All routes registered from separate handler files

---

### [2B.2] SQLite setup + migrations (~2h)
- Driver: `modernc.org/sqlite` (CGO-free, pure Go — important for distroless image)
- File: `services/backend-api/db/db.go` — opens DB file, sets `PRAGMA journal_mode=WAL`, runs migrations
- Migration runner: embed SQL files with `//go:embed migrations/*.sql`; apply in filename order at startup; track applied migrations in `schema_migrations` table
- File: `services/backend-api/db/migrations/001_init.sql` — see db-architecture-design.md for exact SQL
- PVC mount path: `/data/kubechan.db` (configurable via `DB_PATH` env var)

---

### [2B.3] Settings seed (~30min)
- On startup after migrations: `INSERT OR IGNORE INTO settings(key, value) VALUES (...)` for all defaults
- Defaults: `persona.enabled=false`, `persona.idle_chatter=false`, `persona.idle_interval_secs=300`, `bedrock.model_id="qwen3-32b"`, `bedrock.region="us-east-1"`, `bedrock.thinking_budget=0`, `evidence.retention_days=7`, `analysis.retention_days=30`

---

### [2B.4] CRD client setup (~1h)
- File: `services/backend-api/k8s/client.go`
- controller-runtime `client.Client` using in-cluster config (or kubeconfig for dev)
- Register `v1alpha1` scheme
- Shared instance injected into handlers and informer setup

---

### [2B.5] REST endpoints — ProblemCase + DiagnosticRun (~3h)
- File: `services/backend-api/handler/problemcases.go`

`GET /api/v1/problemcases`
- Fetch from CRD API: `client.List(&v1alpha1.ProblemCaseList{}, listOpts...)`
- Filter options via query params: `?severity=`, `?status=`, `?namespace=`
- Pagination via `continue` token (pass through from CRD API list response)
- Response: JSON array of ProblemCase summaries

`GET /api/v1/problemcases/:id`
- `client.Get` by namespace+name parsed from `:id`
- Response: full ProblemCase object

`GET /api/v1/diagnosticruns/:id`
- `client.Get` DiagnosticRun CRD
- Response: full DiagnosticRun object

---

### [2B.6] REST endpoints — Analysis + Evidence (~3h)
- File: `services/backend-api/handler/analysis.go`

`GET /api/v1/problemcases/:id/evidence`
- SQLite query: `SELECT * FROM evidence WHERE problem_case_id = ? ORDER BY created_at DESC LIMIT 1`
- Response: evidence payload JSON

`GET /api/v1/analysisresults/:id`
- SQLite query: `SELECT * FROM analysis_results WHERE id = ?`
- Response: full analysis result payload

`POST /api/v1/problemcases/:id/analyze`
- Create `DiagnosticRun` CRD with `spec.problemCaseRef = id`, `spec.requestedAt = now`, `status.state = pending`
- Insert row into `analysis_requests`: `id=uuid`, `problem_case_id`, `diagnostic_run_id`, `status=pending`
- Response: `202 Accepted` with `{ "diagnosticRunId": "...", "analysisRequestId": "..." }`

---

### [2B.7] REST endpoints — Settings (~2h)
- File: `services/backend-api/handler/settings.go`

`GET /api/v1/settings`
- `SELECT key, value FROM settings` → build response object with all settings decoded from JSON values

`PUT /api/v1/settings`
- Accept partial update; iterate provided fields; `UPDATE settings SET value=?, updated_at=? WHERE key=?`
- Only allow known keys; return 400 for unknown keys

`GET /api/v1/persona/idle-message`
- Read persona settings; forward request to llm-gateway `POST /idle-message` (Phase 3B)
- Stub in Phase 2B: return `501 Not Implemented` until Phase 3B complete

---

### [2B.8] Health endpoints (~1h)
- `GET /healthz` — always returns `200 OK { "status": "ok" }` (liveness: process is alive)
- `GET /readyz` — returns `200` only when CRD client can list ProblemCases AND SQLite ping succeeds; else `503`

---

### [2B.9] POST /internal/evidence (~3h)
- File: `services/backend-api/handler/internal.go`
- Validate request body matches agreed schema
- Generate `id = uuid.New().String()`
- `INSERT INTO evidence (id, diagnostic_run_id, problem_case_id, collected_at, collector_version, payload, payload_bytes, redaction_summary, log_truncation_info, collection_errors, created_at) VALUES (...)`
- Update DiagnosticRun CRD: `client.Status().Patch` → set `status.evidenceRef = evidenceId`, `status.collectedAt`, `status.redactionSummary`, `status.logTruncationInfo`, `status.collectionErrors`
- If `analysis_requests` row exists with `diagnostic_run_id` and `status=pending`: dispatch to llm-gateway (Phase 3B.2); for now stub with a log line

---

### [2B.10] WebSocket Hub (~3h)
- File: `services/backend-api/ws/hub.go`
```go
type Hub struct {
    clients    map[*Client]bool
    register   chan *Client
    unregister chan *Client
    broadcast  chan []byte
}
func (h *Hub) Run()  // goroutine: select on channels
func (h *Hub) Broadcast(msg []byte)
```
- `Run()` is started in a goroutine from `main.go`

---

### [2B.11] WebSocket Client (~2h)
- File: `services/backend-api/ws/client.go`
```go
type Client struct {
    hub  *Hub
    conn *websocket.Conn
    send chan []byte
}
func (c *Client) readPump()   // reads from conn; on close: unregister
func (c *Client) writePump()  // reads from send chan; writes to conn; ping every 25s; write deadline 10s
```

---

### [2B.12] WebSocket endpoint + event types (~2h)
- File: `services/backend-api/ws/events.go` — typed event structs matching full-plan.md §4c schema
```go
type WSEvent struct {
    Type string `json:"type"`
    ID   string `json:"id"`
}
type ProblemCaseCreatedEvent struct { WSEvent; Namespace string; Name string; Kind string; Severity string; Detector string }
// ... all 6 event types
```
- `GET /ws` handler: upgrade HTTP → WebSocket, create Client, register with Hub

---

### [2B.13] CRD watch → WS broadcast (~3h)
- File: `services/backend-api/k8s/informer.go`
- controller-runtime `cache.Cache` started alongside the main HTTP server
- Watch `v1alpha1.ProblemCase`: `OnAdd` → `ProblemCase.Created` event; `OnUpdate` → `ProblemCase.Updated` or `ProblemCase.Resolved`; `OnDelete` → `ProblemCase.Resolved`
- Watch `v1alpha1.DiagnosticRun`: `OnUpdate` state changes → `DiagnosticRun.StatusChanged` event
- Each event: serialize to JSON → `hub.Broadcast()`

---

### [2B.14] Retention pruning goroutine (~2h)
- File: `services/backend-api/db/pruner.go`
- Started in goroutine from `main.go`; runs every 6 hours
- Reads `evidence.retention_days` and `analysis.retention_days` from settings table
- Executes three DELETE statements from full-plan.md §10

---

### [2B.15] Startup recovery (~2h)
- File: `services/backend-api/startup/recovery.go`
- On startup (after DB migrations, before serving HTTP): `SELECT id, problem_case_id, diagnostic_run_id FROM analysis_requests WHERE status = 'pending'`
- For each row: re-dispatch to llm-gateway (Phase 3B.2 stub for now); update `status = dispatched`, `dispatched_at = now`
- Prevents lost analysis intent across backend-api restarts (NFR-R4)

---

## Integration test entry point
With cluster-watcher Phase 1 running:
1. `kubectl get problemcases` — shows open ProblemCase
2. `GET /api/v1/problemcases` — returns same list
3. `GET /ws` (wscat) — WebSocket connects; `connectionStatus = connected`
4. cluster-watcher creates new ProblemCase → WS client receives `ProblemCase.Created` event within 2s
5. `POST /api/v1/problemcases/:id/analyze` → `202`, DiagnosticRun CRD created (state=pending)
6. diagnostics-worker (Phase 2A) processes it → `POST /internal/evidence` → SQLite row created
7. `GET /api/v1/problemcases/:id/evidence` → returns evidence payload
