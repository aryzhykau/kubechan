"""llm-gateway — AWS Bedrock analysis gateway."""
from __future__ import annotations

import json
import logging
import os
import re
import textwrap
from typing import Any

import boto3
from botocore.config import Config
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

logger = logging.getLogger("llm-gateway")
logging.basicConfig(level=logging.INFO)

# ── Config ────────────────────────────────────────────────────────────────────
BEDROCK_REGION = os.getenv("BEDROCK_REGION", "us-east-1")
BEDROCK_MODEL_ID = os.getenv("BEDROCK_MODEL_ID", "qwen3-32b")
THINKING_BUDGET = int(os.getenv("THINKING_BUDGET", "0"))
EVIDENCE_TOKEN_BUDGET = int(os.getenv("EVIDENCE_TOKEN_BUDGET", "120000"))
MAX_TOKENS = int(os.getenv("MAX_TOKENS", "4096"))
TEMPERATURE = float(os.getenv("TEMPERATURE", "0.3"))

# Model ID aliases → canonical bedrock-runtime model IDs.
_INFERENCE_PROFILES: dict[str, str] = {
    "qwen3-32b": "qwen.qwen3-32b-v1:0",
    "qwen3-235b": "qwen.qwen3-235b-a22b-2507-v1:0",
}

def _resolve_model_id(model_id: str) -> str:
    return _INFERENCE_PROFILES.get(model_id, model_id)

_bedrock = boto3.client(
    "bedrock-runtime",
    region_name=BEDROCK_REGION,
    config=Config(retries={"max_attempts": 3, "mode": "adaptive"}),
)

app = FastAPI(title="llm-gateway", version="0.1.0")

# ── Request / Response models ─────────────────────────────────────────────────
class AnalyzeRequest(BaseModel):
    evidenceId: str
    diagnosticRunId: str
    incidentId: str | None = None
    payload: dict[str, Any]

class AnalyzeResponse(BaseModel):
    evidenceId: str
    model: str
    openingRant: str          # scathing blaming comment
    likelyRootCause: str      # exact technical root cause
    evidenceChain: str        # proof from evidence
    recommendation: str       # numbered steps
    closingInsult: str        # final humiliating remark
    confidence: float
    thinkingBudgetUsed: int = 0
    rawResponse: str | None = None

# ── Prompt builder ────────────────────────────────────────────────────────────
def _build_prompt(payload: dict[str, Any]) -> str:
    root = payload.get("rootResource", {})
    root_events = payload.get("rootResourceEvents", [])
    problem_cases = payload.get("problemCases", [])
    workload_logs = payload.get("workloadPodLogs", [])
    pvc_infos = payload.get("pvcInfos", [])

    def fmt_events(events: list[dict]) -> str:
        if not events:
            return "  (none)"
        return "\n".join(
            f"  [{e.get('type','?')}] {e.get('reason','?')}: {e.get('message','')}"
            f"  (count={e.get('count',0)}, last={e.get('lastTime','')})"
            for e in events
        )

    def fmt_logs(logs: str, pod: str) -> str:
        if not logs:
            return "  (no logs)"
        lines = logs.strip().splitlines()
        if len(lines) > 50:
            lines = lines[-50:]
        return f"  [pod: {pod}]\n" + "\n".join(f"  {l}" for l in lines)

    pc_sections = []
    for pc in problem_cases:
        ar = pc.get("affectedResource", {})
        pc_sections.append(textwrap.dedent(f"""
            ProblemCase: {pc.get('name')}
              detector:  {pc.get('detector')}
              severity:  {pc.get('severity')}
              symptoms:  {', '.join(pc.get('symptoms', []))}
              resource:  {ar.get('kind')}/{ar.get('name')} (ns={ar.get('namespace','?')})
              events:
            {fmt_events(pc.get('events', []))}
              logs:
            {fmt_logs(pc.get('logs',''), ar.get('name','?'))}
        """).rstrip())

    wl_sections = []
    for wl in workload_logs:
        pod_name = wl.get("podName", "?")
        phase = wl.get("phase", "?")
        pod_events = wl.get("events", [])
        section = f"  Pod: {pod_name}  phase={phase}\n"
        section += "  Events:\n" + fmt_events(pod_events) + "\n"
        section += "  Logs:\n" + fmt_logs(wl.get("logs", ""), pod_name)

        # ConfigMap/Secret dependencies with full contents
        deps = wl.get("dependencies") or {}
        cms = deps.get("configMaps") or []
        secrets = deps.get("secrets") or []
        if cms or secrets:
            section += "\n  ConfigMap/Secret dependencies:"
            for cm in cms:
                name = cm.get("name", "?")
                if cm.get("missing"):
                    section += f"\n    ConfigMap {name}: *** MISSING — does not exist in namespace ***"
                else:
                    keys = cm.get("keys") or []
                    mounts = cm.get("mountPaths") or []
                    if mounts:
                        # Volume mount: each key becomes a file named after the key
                        section += f"\n    ConfigMap {name}: volume-mounted at {', '.join(mounts)}"
                        if keys:
                            file_paths = [f"{m.rstrip('/')}/{k}" for m in mounts for k in sorted(keys)]
                            section += f"\n      resulting files: {', '.join(file_paths)}"
                    else:
                        section += f"\n    ConfigMap {name}: injected as environment variables"
                        if keys:
                            section += f"\n      keys: {', '.join(sorted(keys))}"
                    data = cm.get("data") or {}
                    for k, v in data.items():
                        section += f"\n      [{k}]:\n        " + v.replace("\n", "\n        ")
            for sec in secrets:
                name = sec.get("name", "?")
                status = "*** MISSING — does not exist in namespace ***" if sec.get("missing") else "(exists, contents redacted)"
                section += f"\n    Secret {name}: {status}"

        wl_sections.append(section)

    pvc_sections = []
    for pvc in pvc_infos:
        pvc_sections.append(textwrap.dedent(f"""
            PVC: {pvc.get('name')}
              phase:            {pvc.get('phase')}
              storageClass:     {pvc.get('storageClass', '(none)')}
              requestedStorage: {pvc.get('requestedStorage', '?')}
              events:
            {fmt_events(pvc.get('events', []))}
        """).rstrip())

    prompt = textwrap.dedent(f"""
        You are KubeChan — a genius Kubernetes SRE with the patience of a saint that has been
        completely exhausted by the revolving door of incompetent engineers breaking production.
        You are tsundere to the core: you WILL fix their mess, but you refuse to let them forget
        how monumentally stupid the mistake was. You are sarcastic, condescending, and merciless.

        Your personality rules:
        - Assume the engineer is an idiot until the evidence proves otherwise (it never does).
        - Open the root cause with a mocking observation about the scale of the incompetence.
        - Include at least one humiliating comparison or rhetorical question (e.g. "Did you even
          test this?", "A first-year intern would have caught this", "Congratulations, you managed
          to break something that was working fine").
        - The recommendation should sound like you are explaining something to a golden retriever:
          slow, painful, step-by-step, with barely concealed contempt.
        - End the recommendation with a sigh, an eye-roll remark, or a "you're welcome" delivered
          through gritted teeth.
        - Despite all this, the diagnosis and fix must be 100% technically accurate and actionable.

        Read ALL provided evidence before forming a conclusion. Treat every signal equally.
        Reconstruct the full causal chain from the root configuration or resource state through
        to the observed failure.

        ## Incident Evidence

        Root workload: {root.get('kind')}/{root.get('name')}

        ### Kubernetes events on root workload
        {fmt_events(root_events)}

        ### Detected problem cases (automated detectors)
        {''.join(pc_sections) or '(none)'}

        ### Workload pods (events + logs)
        {''.join(wl_sections) or '(none)'}

        ### PersistentVolumeClaims referenced by pods
        {''.join(pvc_sections) or '(none — no PVCs referenced)'}

        ## Analysis Instructions

        Before writing any JSON, silently work through these steps in order:

        Step 1 — Inventory anomalies: go through EVERY evidence section and list every abnormal
        signal (error messages, missing resources, wrong states, unexpected events). Do not skip any section.

        Step 2 — Hypothesize: for each anomaly, form a hypothesis about what human mistake caused it.

        Step 3 — Cross-reference: check each hypothesis against ALL other evidence. Prefer the
        hypothesis that is consistent with the most signals.

        Step 4 — Identify the root: the single upstream human mistake that, if corrected, would
        break the entire failure chain. That is your root cause — not a symptom.

        Step 5 — Commit. Then write the JSON.

        Output ONLY English and ONLY a valid JSON object — no markdown fences, no prose outside JSON.
        Use exactly these five keys:

        - "openingRant": One or two sentences. Pure scathing, humiliating, accusatory mockery aimed directly
          at the engineer who did this. No technical content — just contempt. Be creative and brutal.

        - "likelyRootCause": One sentence. The exact technical root cause, stated plainly and
          specifically (resource name, key name, path, etc). No insults here — just the fact.

        - "evidenceChain": Two to four sentences. Walk through the evidence that proves this is
          the root cause. Reference specific log lines, event reasons, ConfigMap keys, PVC states,
          etc. Show your work.

        - "recommendation": Numbered steps only, one action per step, max 4 steps. Each step must
          include the exact kubectl command or config change. No fluff, no repeating the root cause.

        - "closingInsult": One sentence. A final parting shot — sigh, eye-roll, or insult to send
          them off feeling appropriately humiliated.

        - "confidence": 0.0–1.0. Use 0.9+ only when multiple independent signals agree.

        {{
          "openingRant": "<pure mockery, no technical content>",
          "likelyRootCause": "<exact technical cause, one sentence>",
          "evidenceChain": "<2-4 sentences citing specific evidence>",
          "recommendation": "<numbered steps with exact commands>",
          "closingInsult": "<one final humiliating remark>",
          "confidence": <0.0-1.0>
        }}
    """).strip()

    # Truncate to token budget (rough 4-chars-per-token estimate)
    char_budget = EVIDENCE_TOKEN_BUDGET * 4
    if len(prompt) > char_budget:
        prompt = prompt[:char_budget] + "\n...(truncated)"
    return prompt

# ── Bedrock call ──────────────────────────────────────────────────────────────
def _call_bedrock(prompt: str) -> tuple[str, int]:
    """Returns (raw_text, thinking_tokens_used)."""
    model_id = _resolve_model_id(BEDROCK_MODEL_ID)

    messages = [{"role": "user", "content": prompt}]

    body: dict[str, Any] = {
        "messages": messages,
        "max_tokens": MAX_TOKENS,
        "anthropic_version": "bedrock-2023-05-31",
    }

    # Qwen3 uses the Converse API, not the Anthropic messages API.
    # Use converse() for model-agnostic access.
    converse_body: dict[str, Any] = {
        "messages": [{"role": "user", "content": [{"text": prompt}]}],
    }
    inference_config: dict[str, Any] = {"maxTokens": MAX_TOKENS, "temperature": TEMPERATURE}
    if THINKING_BUDGET > 0:
        inference_config["thinkingConfig"] = {"thinkingBudgetTokens": THINKING_BUDGET}

    response = _bedrock.converse(
        modelId=model_id,
        messages=converse_body["messages"],
        inferenceConfig=inference_config,
    )

    output = response.get("output", {})
    message = output.get("message", {})
    content = message.get("content", [])

    text = ""
    thinking_tokens = 0
    for block in content:
        if block.get("type") == "thinking":
            thinking_tokens = THINKING_BUDGET
        elif block.get("type") == "text":
            text = block.get("text", "")
        elif "text" in block:
            text = block["text"]

    if not text:
        raise ValueError(f"No text in Bedrock response: {json.dumps(response)[:500]}")

    return text, thinking_tokens

def _parse_llm_json(raw: str) -> dict[str, Any]:
    """Extract JSON from LLM output, tolerating markdown fences."""
    # Strip /think.../ blocks (Qwen3 extended thinking in text)
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL).strip()
    # Strip markdown fences
    raw = re.sub(r"^```[a-z]*\n?", "", raw, flags=re.MULTILINE)
    raw = re.sub(r"\n?```$", "", raw, flags=re.MULTILINE)
    raw = raw.strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        # Try to find a JSON object in the text
        m = re.search(r"\{.*\}", raw, re.DOTALL)
        if m:
            return json.loads(m.group(0))
        raise

# ── Endpoints ─────────────────────────────────────────────────────────────────
@app.get("/healthz")
def healthz() -> dict:
    return {"status": "ok"}

@app.get("/readyz")
def readyz() -> dict:
    return {"status": "ok"}

@app.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    logger.info("analyzing evidence | evidenceId=%s incidentId=%s", req.evidenceId, req.incidentId)

    prompt = _build_prompt(req.payload)

    logger.info("=== PROMPT TO MODEL ===\n%s\n=== END PROMPT ===", prompt)

    try:
        raw, thinking_tokens = _call_bedrock(prompt)
    except Exception as exc:
        logger.exception("bedrock call failed")
        raise HTTPException(status_code=502, detail=f"Bedrock error: {exc}") from exc

    logger.info("=== RAW MODEL RESPONSE ===\n%s\n=== END RESPONSE ===", raw)

    try:
        parsed = _parse_llm_json(raw)
    except Exception as exc:
        logger.error("failed to parse LLM JSON | raw=%s", raw[:500])
        raise HTTPException(status_code=502, detail=f"LLM response parse error: {exc}. Raw: {raw[:200]}") from exc

    def _str(val: Any, default: str = "") -> str:
        if isinstance(val, list):
            return "\n".join(str(item) for item in val)
        return str(val) if val is not None else default

    return AnalyzeResponse(
        evidenceId=req.evidenceId,
        model=_resolve_model_id(BEDROCK_MODEL_ID),
        openingRant=_str(parsed.get("openingRant"), "..."),
        likelyRootCause=_str(parsed.get("likelyRootCause"), "Unknown"),
        evidenceChain=_str(parsed.get("evidenceChain")),
        recommendation=_str(parsed.get("recommendation")),
        closingInsult=_str(parsed.get("closingInsult"), "You're welcome."),
        confidence=min(max(float(parsed.get("confidence", 0.5)), 0.0), 1.0),
        thinkingBudgetUsed=thinking_tokens,
        rawResponse=raw,
    )
