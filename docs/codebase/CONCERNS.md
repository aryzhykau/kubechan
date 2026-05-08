# Codebase Concerns

## Core Sections (Required)

### 1) Top Risks (Prioritized)

| Severity | Concern | Evidence | Impact | Suggested action |
|----------|---------|----------|--------|------------------|
| High | SQLite single-writer — backend-api cannot scale horizontally | `services/backend-api/db/db.go` WAL pragma; single `DB_PATH` volume | Any attempt to run multiple replicas of backend-api will cause write conflicts or data corruption | Replace SQLite with PostgreSQL or add a proxy if horizontal scaling is needed |
| High | `POST /internal/evidence` has no authentication | `services/backend-api/main.go` — `r.Route("/internal", ...)` registers no auth middleware | Any pod in the cluster can inject arbitrary evidence, potentially poisoning LLM analysis | Add network policy to restrict access; optionally add a shared secret or mTLS for service-to-service calls |
| High | JWT passed as WebSocket URL query parameter | `services/frontend-ui/src/useWebSocket.ts` — `?token=${encodeURIComponent(token)}` | Token appears in nginx/proxy access logs and browser history | Authenticate WebSocket via initial HTTP upgrade headers or a short-lived ticket |
| Medium | No `.env.example` file — required env vars undocumented | Scan: "No .env.example or .env.template found" | New operators must read source code to find all required env vars | Add `.env.example` listing all vars with descriptions |
| Medium | LLM prompt and raw response logged at INFO level | `services/llm-gateway/app/routes.py` — `logger.info("=== PROMPT TO MODEL ===\n%s\n=== END PROMPT ===", prompt)` | Evidence payloads (pod logs, ConfigMap contents) are written to pod logs; may leak sensitive runtime data | Redact or truncate logged payloads; move to DEBUG level |
| Medium | No retry on llm-gateway LLM calls | `services/llm-gateway/app/routes.py` — single `await provider.call(prompt)` with no retry | Transient Bedrock/Copilot errors result in failed DiagnosticRuns | Add exponential backoff retry (2–3 attempts) before returning 502 |
| Low | CRD objects accumulate — no garbage collection | `api/v1alpha1/` CRD types; no TTL field | Resolved Incidents, ProblemCases, DiagnosticRuns accumulate in etcd indefinitely; large clusters with many events will grow etcd | Add background pruner for resolved/old CRDs (parallel to existing DB pruner) |
| Low | Evidence payload stored as `TEXT` JSON in SQLite | `services/backend-api/db/migrations/001_init.sql` — `payload TEXT NOT NULL` | Large log payloads inflate DB size and slow down queries | Compress payloads or move to blob column with size cap |

---

### 2) Technical Debt

| Debt item | Why it exists | Where | Risk if ignored | Suggested fix |
|-----------|---------------|-------|-----------------|---------------|
| Coverage tests added to reach threshold rather than test behaviour | Phase-7 PR history shows multiple commits "fix tests to raise coverage" | `services/backend-api/handler/`, `services/backend-api/ws/ws_test.go` | Tests may pass while real regressions are missed | Refactor to test outcomes, not implementation; add envtest-based controller tests |
| No envtest integration tests for controllers | MVP time constraint; comment in `.github/go-coverage.yml`: "Target: raise this to 50% once backend-api and cluster-watcher controllers have envtest-based tests" | `services/cluster-watcher/controllers/`, `services/diagnostics-worker/controllers/` | Controller reconcile logic is exercised only in production cluster | Add envtest-based suite as a follow-up phase |
| Python test mock strategy unverified | [TODO from investigation] | `services/llm-gateway/tests/` | Unknown coverage of error paths in LLM provider calls | Read test files and verify provider mocking |
| No E2E tests | Not yet implemented per `full-plan.md` | `Makefile` `e2e` target (stub) | Full user flows (cluster-watcher → UI) are not regression-tested | Add e2e tests using a real k8s cluster (e.g., kind) |
| `handler/internal.go` is high-churn and large | Receives evidence, stores in DB, creates analysis request, dispatches LLM goroutine — multiple responsibilities | `services/backend-api/handler/internal.go` (7 churn events in 90 days) | Hard to test in isolation; changes here risk breaking the core pipeline | Extract LLM dispatch into a separate package |
| ConfigMap value redaction not verified | Documented in `project-description.md` as implemented; actual code not read during investigation | `services/diagnostics-worker/collector/` | If not implemented, sensitive ConfigMap values may reach LLM prompts | Verify and add a test for redaction logic |
| No formatter enforced for Python or TypeScript | Not configured | `services/llm-gateway/`, `services/frontend-ui/src/` | Style drift over time | Add `ruff` (Python) and `prettier` (TypeScript) with CI enforcement |

---

### 3) Security Concerns

| Risk | OWASP category | Evidence | Current mitigation | Gap |
|------|----------------|----------|--------------------|-----|
| Unauthenticated internal endpoint | A01 (Broken Access Control) | `services/backend-api/main.go` `/internal` route — no middleware | Network isolation (cluster-internal DNS only) | No explicit network policy in Helm chart; any cluster workload can call it |
| JWT in WebSocket URL | A02 (Cryptographic Failures / A07 Identification and Authentication Failures) | `services/frontend-ui/src/useWebSocket.ts` | HTTPS required for production (nginx configured) | Token may appear in access logs on proxy; browser history |
| Full evidence/prompt logged at INFO | A09 (Security Logging and Monitoring Failures) | `services/llm-gateway/app/routes.py` | Pod log access requires RBAC | Pod logs accessible to anyone with `kubectl logs` RBAC; sensitive data in CloudWatch/Loki if log forwarding is active |
| Log lines collected without redaction | A02 (Sensitive Data Exposure) | `docs/project-description.md`: "Log lines are collected as-is; redaction of secrets in logs is a post-MVP concern" | Secret values never collected (only metadata) | Secrets injected into logs by applications are not redacted |
| Admin password in Kubernetes Secret (plaintext value) | A02 | `services/backend-api/startup/admin_bootstrap.go` — Secret contains generated password | bcrypt-hashed in DB | The Kubernetes Secret itself stores the plaintext password; anyone with `get secret kubechan-admin-credentials` RBAC can read it |

---

### 4) Performance and Scaling Concerns

| Concern | Evidence | Current symptom | Scaling risk | Suggested improvement |
|---------|----------|-----------------|-------------|-----------------------|
| SQLite single-writer | `services/backend-api/db/db.go` WAL pragma | Acceptable at single-replica | Blocks horizontal scaling of backend-api entirely | Replace with PostgreSQL for multi-replica deployment |
| Large evidence payloads in SQLite `TEXT` column | `migrations/001_init.sql` `payload TEXT NOT NULL` | Unknown — depends on cluster size | Large pod logs + many ConfigMaps → multi-MB rows; SQLite `TEXT` unbounded | Add `EVIDENCE_TOKEN_BUDGET` (already in llm-gateway) at collection time; truncate before DB insert |
| In-memory WebSocket Hub | `services/backend-api/ws/hub.go` | Works for single replica | Multiple backend-api replicas would each have their own Hub — clients on different replicas miss events | Add a pub/sub layer (Redis or NATS) for multi-replica WS fan-out |
| All open incidents queried + filtered in-memory | `services/backend-api/handler/incidents.go` — `h.K8s.List(...)` then in-memory filter by state | Acceptable for small clusters | Large clusters with many incidents would return all and filter client-side | Add label selector for state filtering at the Kubernetes API level |

---

### 5) Fragile/High-Churn Areas

| Area | Why fragile | Churn signal | Safe change strategy |
|------|-------------|-------------|----------------------|
| `.github/workflows/ci.yml` | CI pipeline evolved across all phases; coverage thresholds, badge publishing, multi-language build | 17 commits in 90 days (highest in repo) | Test changes in a branch PR; keep job names stable for artifact upload |
| `services/backend-api/main.go` | Wire-up of all dependencies; router configuration; every new handler requires a change here | 8 commits in 90 days | Extract handler registration into a setup function; changes require full integration test |
| `services/backend-api/handler/internal.go` | Receives evidence, stores results, dispatches LLM goroutine — multiple responsibilities in one file | 7 commits in 90 days | Extract LLM dispatch into a dedicated package |
| `services/frontend-ui/src/IncidentList.tsx` | Primary UI component; new incident features land here | 7 commits in 90 days | Add snapshot or interaction tests |
| `services/frontend-ui/src/api.ts` | All API call logic; changes with every new endpoint | 7 commits in 90 days | Keep API client thin; avoid business logic here |
| `api/v1alpha1/zz_generated.deepcopy.go` | Generated; must stay in sync with type changes | 7 commits in 90 days | Always run `make generate` after editing `api/v1alpha1/`; CI enforces this |

---

### 6) `[ASK USER]` Questions

1. **[ASK USER]** Is horizontal scaling of `backend-api` a near-term requirement? If yes, migration from SQLite to PostgreSQL should be prioritized before the next phase.
2. **[ASK USER]** Should the `/internal/evidence` endpoint be secured with a network policy, a shared token, or mTLS? What is the current cluster network policy posture?
3. **[ASK USER]** Is the full prompt/response logging in `llm-gateway` intentional for debugging, or should it be gated behind a debug flag? Logs may contain sensitive workload data.
4. **[ASK USER]** What is the intended deployment topology? Single-replica only, or HA? This affects WebSocket Hub, SQLite, and LLM dispatch design.
5. **[ASK USER]** ConfigMap value redaction: has this been implemented in `diagnostics-worker/collector/`? The project description says it is implemented, but it was not verified in source code during this investigation.
6. **[ASK USER]** How are GitHub Copilot credentials obtained and stored for per-user LLM settings? The `llm_settings.go` handler was not read in detail.
7. **[ASK USER]** Are there plans to add ESLint/Prettier for TypeScript and ruff for Python, or is current style discipline team-enforced?
8. **[ASK USER]** The `make e2e` target exists but appears to be a stub. Is there a timeline for implementing E2E tests?

---

### 7) Evidence

- `docs/codebase/.codebase-scan.txt` — HIGH-CHURN FILES section (top 20 files, last 90 days)
- `services/backend-api/main.go` — `/internal` route without auth middleware
- `services/frontend-ui/src/useWebSocket.ts` — JWT in URL query param
- `services/llm-gateway/app/routes.py` — full prompt/response at INFO log level
- `services/backend-api/db/db.go` — WAL pragma, single-writer SQLite
- `services/backend-api/db/migrations/001_init.sql` — `payload TEXT NOT NULL` unbounded
- `services/backend-api/ws/hub.go` — in-memory Hub (no pub/sub)
- `services/backend-api/handler/incidents.go` — full list + in-memory filter
- `.github/go-coverage.yml` — coverage threshold comment about envtest gap
- `docs/project-description.md` — "Log redaction is post-MVP concern"
- `services/backend-api/startup/admin_bootstrap.go` — plaintext password in K8s Secret
