# External Integrations

## Core Sections (Required)

### 1) Integration Inventory

| System | Type | Purpose | Auth model | Criticality | Evidence |
|--------|------|---------|------------|-------------|----------|
| AWS Bedrock | External LLM API | Primary LLM provider — runs `qwen3-32b` or `qwen3-235b` models for K8s analysis | boto3 standard AWS auth chain (env vars, instance profile, IRSA) | High | `services/llm-gateway/app/providers/bedrock.py`, `pyproject.toml` `boto3>=1.38.0` |
| GitHub Copilot | External LLM API | Secondary LLM provider (feature-parity with Bedrock) | `github-copilot-sdk` credentials passed per-request from backend-api | Medium | `services/llm-gateway/app/providers/copilot.py`, `pyproject.toml` |
| Kubernetes API | Infrastructure API | Read cluster state (Pods, Deployments, Events, etc.); create/update CRDs; read Secrets metadata | In-cluster `ServiceAccount` token + RBAC (`ClusterRole`/`RoleBinding`) | Critical | `services/backend-api/k8s/client.go`, `services/cluster-watcher/main.go`, `helm/kubechan/templates/` |
| SQLite | Embedded database | Persist analysis requests, results, evidence, users, settings | File-based; no network auth | High | `services/backend-api/db/db.go` |
| WebSocket (frontend ↔ backend) | Real-time event stream | Push Incident, ProblemCase, DiagnosticRun, Analysis.Completed, KubeChanState events to browser | JWT token in `?token=` query parameter | High | `services/backend-api/ws/hub.go`, `services/frontend-ui/src/useWebSocket.ts` |
| Kubernetes Secret (`kubechan-admin-credentials`) | Secrets store | Admin password storage and auto-generation on first boot | In-cluster ServiceAccount with `get`/`create` on the specific Secret | High | `services/backend-api/startup/admin_bootstrap.go` |

---

### 2) Data Stores

| Store | Role | Access layer | Key risk | Evidence |
|-------|------|--------------|----------|----------|
| SQLite (`kubechan.db`) | Sole persistent store for backend-api: analysis requests/results, evidence payloads, users, settings, LLM settings | `services/backend-api/db/` (`*sql.DB` via `modernc/sqlite`) | Single-writer; no horizontal scale; large evidence payloads stored as `TEXT` JSON in `evidence.payload` | `services/backend-api/db/db.go`, `services/backend-api/db/migrations/001_init.sql` |
| Kubernetes CRDs | Inter-service event bus: `Incident`, `ProblemCase`, `DiagnosticRun`, `KubeChanState` — and `KubechanExclusionRule` (suppression rules, see below) | controller-runtime `client.Client` (all Go services) | CRD objects accumulate indefinitely; no TTL or garbage collection implemented | `api/v1alpha1/`, `helm/kubechan/crds/` |

**`KubechanExclusionRule` CRD** — lives in the control namespace (`kubechan`). Fields: `enabled`, `namespace` scope, `detectors` list, `targetResources` (exact refs), `selector` (label-based), `timeWindow` (IANA timezone + recurring periods). Created by users via `POST /api/v1/exclusion-rules` (manually or from an LLM-proposed `ExclusionRuleProposal`). Evaluated at two points: (1) `exclusion.IsExcluded()` inside every detector reconciler to suppress new ProblemCases; (2) `ExclusionRuleReconciler` on rule create/enable to auto-resolve existing Incidents that match.

**Database schema summary (9 migrations):**
- `001_init.sql` — `evidence`, `analysis_results`, `analysis_requests`, `settings`
- `002_user_rating.sql` — rating column on `analysis_results`
- `003_manual_incidents.sql` — `manual_incident_owners` table
- `004_prompt_storage.sql` — `prompt` column on `analysis_results`
- `005_needs_more_info.sql` — `needs_more_info`, `suggested_resources` columns
- `006_users.sql` — `users` table (id, username, password_hash, role)
- `007_user_attribution.sql` — user attribution to requests
- `008_user_llm_settings.sql` — per-user LLM settings
- `009_detector_settings.sql` — detector threshold settings

---

### 3) Secrets and Credentials Handling

- **JWT secret:** `JWT_SECRET` env var, required at startup; validated by `handler.ValidateJWTSecret()` before server starts. Never logged.
- **Admin password:** auto-generated with `crypto/rand` (32 bytes, base64) on first boot; stored in Kubernetes Secret `kubechan-admin-credentials`; bcrypt-hashed (cost 12) before DB insert.
- **AWS credentials:** standard boto3 credential chain (env vars `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, IRSA, instance profile). Not hardcoded. Region and profile [TODO — not verified from provider source].
- **GitHub Copilot credentials:** passed per-request as `credentials` dict in `AnalyzeRequest` from backend-api to llm-gateway. How backend-api obtains them: [TODO — not verified from `llm_settings.go` source].
- **No `.env.example`** file found in the repo — required env vars are not centrally documented.
- **Secrets never committed:** verified — no hardcoded credentials found in scan.

---

### 4) Reliability and Failure Behavior

- **LLM calls:** single attempt; HTTP 502 returned to backend-api on failure (`HTTPException(status_code=502, ...)` in `routes.py`). No retry or circuit-breaker on llm-gateway side.
- **backend-api → llm-gateway dispatch:** goroutine-based, fire-and-forget after evidence receipt. On crash before dispatch, `startup.RecoverPendingRequests` re-dispatches pending `analysis_requests` from DB at next start.
- **Kubernetes API calls:** controller-runtime handles reconnection and exponential backoff internally.
- **WebSocket clients:** slow-consumer protection in Hub — full send buffer drops the message silently; client reconnects after 3 seconds on close (`useWebSocket.ts`).
- **SQLite:** WAL mode enabled (`PRAGMA journal_mode=WAL`) for improved read concurrency; `PRAGMA foreign_keys=ON` enforced.
- **Timeouts:** [TODO — explicit HTTP client timeouts for backend-api→llm-gateway requests not verified]

---

### 5) Observability for Integrations

- **Logging around external calls:**
  - llm-gateway logs full prompt and raw model response at INFO level (visible in pod logs)
  - backend-api logs errors from LLM dispatch goroutine
- **Metrics:** Prometheus client is an indirect dependency (pulled in by controller-runtime), but no custom metrics are instrumented
- **Tracing:** none observed
- **Health probes:** all services expose `/healthz` and `/readyz` endpoints; Kubernetes liveness/readiness probes configured in Helm chart
- **Missing visibility gaps:** no distributed tracing across the full pipeline (cluster-watcher → diagnostics-worker → backend-api → llm-gateway); no alerting rules

---

### 6) Evidence

- `services/llm-gateway/app/providers/bedrock.py` — Bedrock provider
- `services/llm-gateway/app/providers/copilot.py` — Copilot provider
- `services/llm-gateway/app/providers/base.py` — provider factory
- `services/llm-gateway/app/routes.py` — HTTP 502 on LLM failure
- `services/backend-api/db/db.go` — SQLite open + WAL pragma
- `services/backend-api/db/migrations/001_init.sql` — schema
- `services/backend-api/startup/admin_bootstrap.go` — Kubernetes Secret + bcrypt
- `services/backend-api/ws/hub.go` — slow-consumer protection
- `services/frontend-ui/src/useWebSocket.ts` — 3-second reconnect
- `services/backend-api/handler/auth.go` — JWT secret validation
- `helm/kubechan/templates/` — RBAC and health probe definitions
