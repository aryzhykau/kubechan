# Phase 3B — backend-api analysis completion

## Prerequisites (from other services)
- Phase 2B fully complete (backend-api core live)
- `POST /internal/analysis-result` JSON schema agreed with llm-gateway (Phase 3 integration contract)
- `POST /analyze` schema agreed with llm-gateway
- llm-gateway Phase 3A live for end-to-end testing

---

## Tasks (ordered)

### [3B.1] POST /internal/analysis-result handler (~4h)
- File: `services/backend-api/handler/internal.go` (extend from Phase 2B.9)
- Full flow on receipt:
  1. **Validate** request body: required fields (id, problem_case_id, diagnostic_run_id, model, status, payload)
  2. **Upsert** `analysis_results` row in SQLite:
     ```sql
     INSERT OR REPLACE INTO analysis_results
       (id, problem_case_id, diagnostic_run_id, model, status,
        likely_root_cause, confidence, consistency_check_status,
        has_styled_message, thinking_budget_used, error_message, payload, created_at)
     VALUES (...)
     ```
  3. **Mark** `analysis_requests` row: `UPDATE analysis_requests SET status='completed', completed_at=now WHERE diagnostic_run_id=?`
  4. **Patch** ProblemCase CRD: `client.Status().Patch` → `status.latestAnalysisResultRef = analysisResultId`
  5. **Broadcast** WS event:
     - On `status=completed`: `AnalysisResult.Completed` event with `{id, problemCaseId, confidence}`
     - On `status=failed`: `AnalysisResult.Failed` event with `{id, problemCaseId, error}`
  6. Return `200 OK`

---

### [3B.2] llm-gateway dispatch from /internal/evidence (~2h)
- File: `services/backend-api/handler/internal.go` (extend POST /internal/evidence handler from Phase 2B.9)
- After storing evidence + updating DiagnosticRun CRD:
  - Check if `analysis_requests` row exists with matching `diagnostic_run_id` and `status='pending'`
  - If yes: fire-and-forget `POST http://llm-gateway.kubechan.svc.cluster.local/analyze` with:
    ```json
    {
      "problemCaseId": "...",
      "diagnosticRunId": "...",
      "evidencePayload": { ...full evidence... },
      "settings": {
        "personaEnabled": true/false,
        "bedrockModelId": "...",
        "bedrockRegion": "...",
        "thinkingBudget": 0,
        "evidenceTokenBudget": 120000
      }
    }
    ```
  - HTTP client timeout: 130s (Bedrock analysis can take up to 120s per NFR-P3)
  - Update `analysis_requests`: `status='dispatched'`, `dispatched_at=now`
  - On HTTP error from llm-gateway: log; do NOT fail the `/internal/evidence` response — diagnostics are complete regardless

---

### [3B.3] Startup recovery — dispatch pending requests (~2h)
- File: `services/backend-api/startup/recovery.go` (from Phase 2B.15 stub → now fully implemented)
- On startup, after DB migrations and before serving HTTP:
  1. `SELECT id, problem_case_id, diagnostic_run_id FROM analysis_requests WHERE status = 'pending'`
  2. For each: check if evidence exists in SQLite for `diagnostic_run_id`; if yes → dispatch to llm-gateway
  3. Update row: `status='dispatched'`, `dispatched_at=now`
- Handles backend-api crash between DiagnosticRun completion and llm-gateway dispatch

---

### [3B.4] GET /api/v1/persona/idle-message (~2h)
- File: `services/backend-api/handler/persona.go`
- Guard: return `404` if `persona.enabled=false` or `persona.idle_chatter=false` in settings
- Forward to `POST http://llm-gateway.kubechan.svc.cluster.local/idle-message`
  - Request: `{ "settings": { "personaEnabled": true } }`
- Return llm-gateway response body as-is
- Timeout: 30s (idle messages should be quick)

---

## Integration test entry point
Full pipeline verification:
1. cluster-watcher creates a ProblemCase
2. `POST /api/v1/problemcases/:id/analyze` → DiagnosticRun CRD created (pending)
3. diagnostics-worker collects evidence → `POST /internal/evidence` received by backend-api
4. backend-api dispatches to llm-gateway → Bedrock called → result returned
5. `POST /internal/analysis-result` received by backend-api
6. `kubectl get problemcase <id>` → `status.latestAnalysisResultRef` populated
7. `GET /api/v1/analysisresults/<id>` → full analysis payload returned
8. WebSocket client receives `AnalysisResult.Completed` event
