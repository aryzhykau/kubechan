# Phase 3A — llm-gateway

## Prerequisites (from other services)
- `POST /analyze` request schema agreed with backend-api (integration contract for Phase 3)
- `POST /internal/analysis-result` schema agreed with backend-api
- backend-api `POST /internal/evidence` endpoint live (Phase 2B.9) — provides evidence payload format to design against
- AWS Bedrock credentials: IRSA role (EKS) or static creds Secret (local dev)

---

## Tasks (ordered)

### [3A.1] FastAPI scaffold (~2h)
- File: `services/llm-gateway/main.py`
- FastAPI app with lifespan context manager:
  ```python
  @asynccontextmanager
  async def lifespan(app: FastAPI):
      app.state.bedrock = boto3.client("bedrock-runtime", region_name=settings.BEDROCK_REGION)
      yield
  ```
- Routes registered from handler modules
- `POST /analyze` → `202 Accepted`; dispatches background task via `BackgroundTasks`
- `GET /healthz` → `200 {"status": "ok"}`
- Structured logging: `structlog.configure(...)` with JSON renderer

---

### [3A.2] Config (~1h)
- File: `services/llm-gateway/config.py`
```python
class Settings(BaseSettings):
    BEDROCK_REGION: str = "us-east-1"
    BEDROCK_MODEL_ID: str = "qwen3-32b"
    THINKING_BUDGET: int = 0           # 0 = /no_think; >0 = budget_tokens
    BACKEND_API_URL: str               # e.g. http://backend-api.kubechan.svc.cluster.local
    LOG_LEVEL: str = "INFO"

    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8")
```
- Credentials: IRSA injects `AWS_ROLE_ARN` + `AWS_WEB_IDENTITY_TOKEN_FILE` automatically; boto3 picks them up via default credential chain. Static env var fallback (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) works automatically too — no explicit handling needed.

---

### [3A.3] System prompt template (~4h)
- File: `services/llm-gateway/prompts/system_prompt.md` (Jinja2 template)
- Sections in order:
  1. **Role** — "You are a Kubernetes diagnostic assistant. You have been given collected evidence about a cluster problem. Your job is to identify the likely root cause and provide actionable remediation guidance."
  2. **Constraints** — evidence-only; no invented facts; no destructive commands as defaults; no claim of having performed actions; confidence must reflect evidence quality
  3. **Evidence glossary** — pod phases, container state reasons (CrashLoopBackOff, OOMKilled, Error, Completed), common event reasons (BackOff, FailedScheduling, Evicted, OOMKilling), condition types (Ready, MemoryPressure, DiskPressure)
  4. **Output contract** — "You MUST call the `submit_analysis` tool exactly once with a JSON object matching the schema. Do not produce any other output."
  5. **Confidence calibration** — calibration table: full evidence = up to 0.9; missing logs = cap 0.7; truncated evidence = cap 0.6; no events = cap 0.8; explicitly state reason in `confidenceRationale`
  6. **Persona block** (conditional):
     ```
     {% if persona_enabled %}
     Additionally, populate the `styledMessage` field. The message must convey the same technical meaning as `likelyRootCause` in a tsundere anime character voice. Rules: same facts, no new warnings, no new technical claims, keep it under 3 sentences.
     {% endif %}
     ```
- File: `services/llm-gateway/prompts/tool_schema.json` — Bedrock tool use definition for `submit_analysis`:
  ```json
  {
    "name": "submit_analysis",
    "description": "Submit structured diagnostic analysis result",
    "inputSchema": { "json": { ...pydantic-derived schema... } }
  }
  ```

---

### [3A.4] Pydantic output model (~2h)
- File: `services/llm-gateway/models.py`
```python
class EvidenceMapping(BaseModel):
    evidenceType: str
    observation: str
    relevance: str  # "high" | "medium" | "low"

class AnalysisOutput(BaseModel):
    likelyRootCause: str
    confidence: float = Field(ge=0.0, le=1.0)
    confidenceRationale: str
    evidenceMapping: list[EvidenceMapping]
    recommendedRunbook: str              # markdown
    kubectlCommands: list[str]           # copy-pasteable commands
    safetyNotes: list[str]              # warnings about destructive commands
    styledMessage: str | None = None    # persona layer; None when persona disabled
    consistencyCheckStatus: str = "passed"  # "passed" | "warning"
```
- Generate `tool_schema.json` from this model: `AnalysisOutput.model_json_schema()`

---

### [3A.5] Evidence priority-tier builder (~4h)
- File: `services/llm-gateway/prompt_builder.py`
```python
def build_evidence_block(evidence: dict, token_budget: int = 120_000) -> str
```
- Token counting: use `len(text.split()) * 1.3` as conservative estimate (no tokenizer dependency)
- Fill order until budget exhausted:
  - **Tier 1** (always include): detector result, pod phase, container states, exit codes, latest 10 events
  - **Tier 2**: current logs tail 300 lines, previous logs tail 100 lines
  - **Tier 3**: owner manifest spec + status (Deployment/StatefulSet/DaemonSet)
  - **Tier 4**: service selectors, endpoint state, node conditions, configmap/secret metadata
- After each tier: check remaining budget; if exhausted before next tier: append `NOTE: evidence truncated — tiers N+ omitted due to token budget`
- Return a single formatted string block ready for insertion into the user message

---

### [3A.6] Bedrock Converse adapter (~3h)
- File: `services/llm-gateway/bedrock_client.py`
```python
async def call_bedrock(
    system_prompt: str,
    evidence_block: str,
    tool_schema: dict,
    thinking_budget: int
) -> AnalysisOutput
```
- Converse API call:
  ```python
  bedrock.converse(
      modelId=settings.BEDROCK_MODEL_ID,
      system=[{"text": system_prompt}],
      messages=[{"role": "user", "content": [{"text": evidence_block}]}],
      toolConfig={
          "tools": [{"toolSpec": tool_schema}],
          "toolChoice": {"tool": {"name": "submit_analysis"}}
      },
      additionalModelRequestFields=thinking_fields  # see below
  )
  ```
- THINKING_BUDGET == 0: prepend `/no_think` to system prompt; no `additionalModelRequestFields`
- THINKING_BUDGET > 0: `{"thinking": {"type": "enabled", "budget_tokens": THINKING_BUDGET}}`
- Extract tool use result: `response["output"]["message"]["content"][0]["toolUse"]["input"]`
- Validate against `AnalysisOutput` Pydantic model; raises `ValidationError` on mismatch

---

### [3A.7] Retry + backoff logic (~3h)
- File: `services/llm-gateway/retry.py`
- Wrap `call_bedrock` with:
  - On `ThrottlingException` or `ServiceUnavailableException`: exponential backoff — `2^attempt * base_delay` (base 1s); max 3 retries
  - On `ValidationError` (Pydantic): retry once with stricter prompt addition: "IMPORTANT: Your previous response did not match the required JSON schema. You MUST call `submit_analysis` with valid JSON. Do not add extra fields."
  - After max retries: raise `AnalysisFailedError` with error message
- Track `attempt_count` for observability logging

---

### [3A.8] Consistency checker (~2h)
- File: `services/llm-gateway/consistency.py`
```python
def check_consistency(output: AnalysisOutput) -> AnalysisOutput
```
- Only runs when `styledMessage` is present (persona enabled)
- Scan `styledMessage` for deny-list phrases: `["actually", "also note that", "warning:", "critical:", "you must", "immediately", "urgent"]` (case-insensitive)
- Cross-check: any factual claim in `styledMessage` that doesn't appear in `likelyRootCause` + `recommendedRunbook` (simple substring match for MVP)
- On match: set `output.consistencyCheckStatus = "warning"`; log structured warning with matched phrases
- Never block delivery — always return the output

---

### [3A.9] Analysis handler + result write-back (~2h)
- File: `services/llm-gateway/handler.py`
- Background task function `run_analysis(request: AnalyzeRequest)`:
  1. Build system prompt from Jinja2 template (render with `persona_enabled`)
  2. Build evidence block from tier builder
  3. Call Bedrock via retry wrapper
  4. Run consistency checker
  5. POST to `backend-api/internal/analysis-result` with full `AnalysisOutput` + metadata (model, problemCaseId, diagnosticRunId, status="completed")
  6. On `AnalysisFailedError`: POST with `status="failed"`, `error_message=str(e)`

---

### [3A.10] Unit tests (~3h)
- `tests/test_prompt_builder.py`:
  - Tier-fill order with varying budget sizes
  - Truncation note appended at correct tier
- `tests/test_consistency.py`:
  - All deny-list phrases trigger `warning` status
  - Clean styledMessage passes with `passed` status
- `tests/test_models.py`:
  - Valid tool use JSON → `AnalysisOutput` model
  - Invalid JSON → `ValidationError`

---

## Integration test entry point
With backend-api Phase 2B live and evidence stored in SQLite:
1. `POST /analyze` with a real evidence payload fixture
2. Verify: `POST /internal/analysis-result` received by backend-api (check logs or mock)
3. Verify Bedrock was called: check structlog output for Bedrock request/response
4. Smoke-test harness: load recorded evidence fixture file, call real Bedrock, assert `AnalysisOutput` validates (de-risks R-3 before Phase 3B integration)
