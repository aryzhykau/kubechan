"""llm-gateway — multi-provider LLM analysis gateway (Bedrock + Copilot)."""
from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import textwrap
from abc import ABC, abstractmethod
from typing import Any

import boto3
import copilot as copilot_sdk
from botocore.config import Config
from copilot.generated.session_events import AssistantMessageData
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

logger = logging.getLogger("llm-gateway")
logging.basicConfig(level=logging.INFO)

# ── Default config (used when no per-user credentials are supplied) ───────────
DEFAULT_BEDROCK_REGION = os.getenv("BEDROCK_REGION", "us-east-1")
DEFAULT_BEDROCK_MODEL_ID = os.getenv("BEDROCK_MODEL_ID", "qwen3-32b")
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

app = FastAPI(title="llm-gateway", version="0.2.0")

# ── Request / Response models ─────────────────────────────────────────────────
class PriorDiagnosis(BaseModel):
    attempt: int
    likelyRootCause: str
    userRating: str = ""  # "up", "down", or ""

class AnalyzeRequest(BaseModel):
    evidenceId: str
    diagnosticRunId: str
    incidentId: str | None = None
    reanalysisCount: int = 0
    moodLevel: int = 0
    priorDiagnoses: list[PriorDiagnosis] = []
    userMessage: str = ""
    provider: str = "bedrock"
    credentials: dict[str, Any] = {}
    payload: dict[str, Any]

class SuggestedResource(BaseModel):
    kind: str
    reason: str

class AnalyzeResponse(BaseModel):
    evidenceId: str
    model: str
    openingRant: str          # scathing blaming comment
    likelyRootCause: str      # exact technical root cause
    evidenceChain: str        # proof from evidence
    recommendation: str       # numbered steps
    closingInsult: str        # final humiliating remark
    confidence: float
    needsMoreInfo: bool = False
    suggestedResources: list[SuggestedResource] = []
    thinkingBudgetUsed: int = 0
    rawResponse: str | None = None
    prompt: str | None = None

# ── Prompt builder ────────────────────────────────────────────────────────────
def _build_prompt(payload: dict[str, Any], reanalysis_count: int = 0, mood_level: int = 0,
                  prior_diagnoses: list | None = None, user_message: str = "") -> str:
    root = payload.get("rootResource", {})
    root_events = payload.get("rootResourceEvents", [])
    problem_cases = payload.get("problemCases", [])
    workload_logs = payload.get("workloadPodLogs", [])
    pvc_infos = payload.get("pvcInfos", [])
    related_resources = payload.get("relatedResourceEvidence", [])

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

    related_sections = []
    for rr in related_resources:
        res = rr.get("resource", {})
        spec = rr.get("spec") or {}
        section = f"Related resource: {res.get('kind')}/{res.get('name')}"

        if spec:
            kind = res.get("kind", "")
            if kind == "Ingress":
                rules = spec.get("rules") or []
                ic = spec.get("ingressClassName") or "(none)"
                section += f"\n  ingressClassName: {ic}"
                for rule in rules:
                    section += f"\n  host: {rule.get('host', '*')}"
                    for path in rule.get("paths") or []:
                        section += (
                            f"\n    path={path.get('path','/')}  "
                            f"→ service={path.get('backendService','?')} "
                            f"port={path.get('backendPort','?')}"
                        )
                annotations = spec.get("annotations") or {}
                if annotations:
                    section += "\n  annotations:"
                    for k, v in annotations.items():
                        section += f"\n    {k}: {v}"
            elif kind == "Service":
                section += f"\n  type:     {spec.get('type', '?')}"
                section += f"\n  selector: {spec.get('selector', {})}"
                section += f"\n  clusterIP: {spec.get('clusterIP', '?')}"
                for p in spec.get("ports") or []:
                    section += f"\n  port: {p.get('name','')} {p.get('port')}→{p.get('targetPort')}/{p.get('protocol','TCP')}"
                annotations = spec.get("annotations") or {}
                if annotations:
                    section += "\n  annotations:"
                    for k, v in annotations.items():
                        section += f"\n    {k}: {v}"
            elif kind == "ConfigMap":
                data = spec.get("data") or {}
                if data:
                    section += "\n  data:"
                    for k, v in data.items():
                        section += f"\n    [{k}]: {v}"
            elif kind in ("Deployment", "StatefulSet", "DaemonSet"):
                section += f"\n  replicas/ready: {spec.get('readyReplicas','?')}/{spec.get('replicas','?')}"
                if spec.get("conditions"):
                    section += "\n  conditions:"
                    for c in spec["conditions"]:
                        section += f"\n    {c.get('type')}: {c.get('status')} — {c.get('message','')}"
            elif kind == "CronJob":
                section += f"\n  schedule:         {spec.get('schedule','?')}"
                section += f"\n  suspend:          {spec.get('suspend')}"
                section += f"\n  lastScheduleTime: {spec.get('lastScheduleTime','?')}"
                section += f"\n  lastSuccessfulTime: {spec.get('lastSuccessfulTime','?')}"
            elif kind in ("Job", "PersistentVolumeClaim"):
                for k, v in spec.items():
                    if v is not None:
                        section += f"\n  {k}: {v}"

        section += f"\n  events:\n{fmt_events(rr.get('events', []))}"
        related_sections.append(section)

    user_message_note = ""
    if user_message:
        user_message_note = textwrap.dedent(f"""
        USER REPORTED: "{user_message}"
        Treat this as your primary diagnostic framing. Interpret all evidence below in light of
        what the user described. The user has direct knowledge of the symptom timeline and context.
        """)

    prior_history_note = ""
    if prior_diagnoses:
        lines = []
        for p in prior_diagnoses:
            rating_str = ""
            if p.get("userRating") == "down":
                rating_str = " ❌ REJECTED by user — this diagnosis was WRONG"
            elif p.get("userRating") == "up":
                rating_str = " ✅ CONFIRMED by user"
            lines.append(f"  - Attempt {p['attempt']}: \"{p['likelyRootCause']}\"{rating_str}")
        rejected = [p for p in prior_diagnoses if p.get("userRating") == "down"]
        rejected_causes = ", ".join(f'"{p["likelyRootCause"]}"' for p in rejected)
        prior_history_note = f"""
        PRIOR DIAGNOSIS HISTORY for this incident:
{chr(10).join(lines)}
"""
        if rejected:
            prior_history_note += f"""
        ⚠️  CRITICAL: The following root cause(s) were already diagnosed AND REJECTED by the user
        as incorrect. You MUST NOT reach the same conclusion again:
        {rejected_causes}
        Approach the evidence from a completely different angle.
"""

    mood_note = ""
    if mood_level == 1:
        mood_note = """
        MOOD NOTICE: KubeChan is currently IRRITATED. Multiple incidents are piling up
        and she is losing her patience fast. Her tone should be noticeably sharper and
        terser than usual. The openingRant should drip with exhausted contempt. She is
        still technically accurate — but she sounds like she is one incident away from
        walking out.
"""
    elif mood_level >= 2:
        mood_note = """
        MOOD NOTICE: KubeChan is in FULL RAGE MODE. The cluster is a sustained disaster
        and she has been dealing with it non-stop. She is DONE. The openingRant must be
        scorched earth — maximum fury, zero diplomacy. She is still technically precise
        but every word sounds like she is filing her resignation letter in real-time.
        The closingInsult should be apocalyptic.
"""

    reanalysis_note = ""
    if reanalysis_count == 1:
        reanalysis_note = """
        ⚠️  RE-ANALYSIS NOTICE: The user already received your previous diagnosis and is
        asking AGAIN. They clearly didn't fix it, couldn't follow instructions, or managed
        to break something else in the process. React with visible exasperation.
        Your openingRant MUST acknowledge this is the second time you are explaining
        the same cluster mess to the same engineer.
"""
    elif reanalysis_count >= 2:
        reanalysis_note = f"""
        ⚠️  RE-ANALYSIS NOTICE: This is analysis #{reanalysis_count + 1} of this incident.
        The user has now asked KubeChan {reanalysis_count + 1} times about the same broken
        cluster. Your patience, already non-existent, is now a distant memory.
        Your openingRant MUST be increasingly furious. Reference the fact that you have
        already explained this {reanalysis_count} time(s). Consider whether they are
        actually reading your responses at all.
"""

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

        {user_message_note}
        Root workload: {root.get('kind')}/{root.get('name')}

        ### Kubernetes events on root workload
        {fmt_events(root_events)}

        ### Detected problem cases (automated detectors)
        {''.join(pc_sections) or '(none)'}

        ### Workload pods (events + logs)
        {''.join(wl_sections) or '(none)'}

        ### PersistentVolumeClaims referenced by pods
        {''.join(pvc_sections) or '(none — no PVCs referenced)'}

        ### Related resources tagged by user
        {''.join(related_sections) or '(none)'}

        ## Analysis Instructions

        {reanalysis_note}
        {prior_history_note}
        {mood_note}

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

        - "confidence": 0.0–1.0. Be HONEST and CONSERVATIVE.
          Use 0.9+ ONLY when multiple independent signals (events, logs, config, PVC state)
          ALL point to the same single root cause with no ambiguity.
          Use 0.7–0.89 when the evidence is strong but one signal is missing or indirect.
          Use 0.5–0.69 when the root cause is a reasonable hypothesis but not proven.
          Use below 0.5 when you are genuinely uncertain.
          Default to lower confidence rather than higher when in doubt.
          If prior diagnoses were rejected, drop your confidence by at least 0.1 from where
          you would otherwise place it.

        - "needsMoreInfo": true if your confidence is below 0.65 AND you believe that inspecting
          additional Kubernetes resources (which were not provided) would materially improve the
          diagnosis. Set to false when you have enough evidence to be reasonably confident, even
          if confidence is below 0.65 for other reasons (e.g. genuinely ambiguous failure).

        - "suggestedResources": array of objects with "kind" and "reason". Only populate when
          needsMoreInfo is true. List the specific resource kinds (e.g. "Ingress", "ConfigMap",
          "Service") that you would need to inspect to improve the diagnosis. For each, provide
          a one-sentence reason explaining what you hope to find there.
          Example: {{"kind": "Ingress", "reason": "Backend service name in the Ingress rules may not match the actual Service name, causing 503 errors."}}
          Leave as empty array [] when needsMoreInfo is false.

        {{
          "openingRant": "<pure mockery, no technical content>",
          "likelyRootCause": "<exact technical cause, one sentence>",
          "evidenceChain": "<2-4 sentences citing specific evidence>",
          "recommendation": "<numbered steps with exact commands>",
          "closingInsult": "<one final humiliating remark>",
          "confidence": <0.0-1.0>,
          "needsMoreInfo": <true|false>,
          "suggestedResources": [{{"kind": "<Kind>", "reason": "<one sentence>"}}]
        }}
    """).strip()

    # Truncate to token budget (rough 4-chars-per-token estimate)
    char_budget = EVIDENCE_TOKEN_BUDGET * 4
    if len(prompt) > char_budget:
        prompt = prompt[:char_budget] + "\n...(truncated)"
    return prompt

# ── Provider interface ────────────────────────────────────────────────────────
class LLMProvider(ABC):
    @abstractmethod
    async def call(self, prompt: str) -> tuple[str, int]:
        """Call the LLM. Returns (raw_text, thinking_tokens_used)."""

    @abstractmethod
    def model_id(self) -> str:
        """Human-readable model identifier for logging / storage."""


class BedrockProvider(LLMProvider):
    """Calls AWS Bedrock using the Converse API (sync boto3 offloaded to thread)."""

    def __init__(self, credentials: dict[str, Any]) -> None:
        region = credentials.get("region") or DEFAULT_BEDROCK_REGION
        access_key = credentials.get("accessKeyId") or ""
        secret_key = credentials.get("secretAccessKey") or ""
        self._model = credentials.get("modelId") or DEFAULT_BEDROCK_MODEL_ID

        kwargs: dict[str, Any] = {
            "region_name": region,
            "config": Config(retries={"max_attempts": 3, "mode": "adaptive"}),
        }
        if access_key and secret_key:
            kwargs["aws_access_key_id"] = access_key
            kwargs["aws_secret_access_key"] = secret_key

        self._bedrock = boto3.client("bedrock-runtime", **kwargs)

    def model_id(self) -> str:
        return _resolve_model_id(self._model)

    def _sync_call(self, prompt: str) -> tuple[str, int]:
        inference_config: dict[str, Any] = {"maxTokens": MAX_TOKENS, "temperature": TEMPERATURE}
        if THINKING_BUDGET > 0:
            inference_config["thinkingConfig"] = {"thinkingBudgetTokens": THINKING_BUDGET}

        response = self._bedrock.converse(
            modelId=self.model_id(),
            messages=[{"role": "user", "content": [{"text": prompt}]}],
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

    async def call(self, prompt: str) -> tuple[str, int]:
        return await asyncio.to_thread(self._sync_call, prompt)


class CopilotProvider(LLMProvider):
    """Calls GitHub Copilot via the official github-copilot-sdk."""

    def __init__(self, credentials: dict[str, Any]) -> None:
        token = credentials.get("token") or ""
        if not token:
            raise ValueError("Copilot credentials must include 'token'")
        self._token = token
        self._model = credentials.get("modelId") or "gpt-4.1"

    def model_id(self) -> str:
        return self._model

    async def call(self, prompt: str) -> tuple[str, int]:
        config = copilot_sdk.SubprocessConfig(github_token=self._token)
        async with copilot_sdk.CopilotClient(config) as client:
            async with await client.create_session() as session:
                await session.set_model(self._model)
                response = await session.send_and_wait(prompt, timeout=300.0)
                if response is None:
                    raise ValueError("No response received from Copilot")
                if isinstance(response.data, AssistantMessageData):
                    return response.data.content, 0
                raise ValueError(f"Unexpected Copilot response type: {type(response.data)}")


def _make_provider(provider_name: str, credentials: dict[str, Any]) -> LLMProvider:
    """Instantiate the appropriate LLMProvider from provider name + credentials."""
    if provider_name == "copilot":
        return CopilotProvider(credentials)
    # Default: bedrock (even if provider_name is empty or "bedrock")
    return BedrockProvider(credentials)

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
async def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    logger.info("analyzing evidence | evidenceId=%s incidentId=%s provider=%s",
                req.evidenceId, req.incidentId, req.provider)

    prompt = _build_prompt(req.payload, req.reanalysisCount, req.moodLevel,
                           [p.model_dump() for p in req.priorDiagnoses],
                           req.userMessage)

    logger.info("=== PROMPT TO MODEL ===\n%s\n=== END PROMPT ===", prompt)

    try:
        provider = _make_provider(req.provider, req.credentials)
        raw, thinking_tokens = await provider.call(prompt)
    except Exception as exc:
        logger.exception("LLM provider call failed | provider=%s", req.provider)
        raise HTTPException(status_code=502, detail=f"LLM provider error: {exc}") from exc

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

    raw_suggested = parsed.get("suggestedResources") or []
    suggested = []
    for s in raw_suggested:
        if isinstance(s, dict) and s.get("kind"):
            suggested.append(SuggestedResource(kind=str(s["kind"]), reason=str(s.get("reason", ""))))

    needs_more_info = bool(parsed.get("needsMoreInfo", False)) and len(suggested) > 0

    return AnalyzeResponse(
        evidenceId=req.evidenceId,
        model=provider.model_id(),
        openingRant=_str(parsed.get("openingRant"), "..."),
        likelyRootCause=_str(parsed.get("likelyRootCause"), "Unknown"),
        evidenceChain=_str(parsed.get("evidenceChain")),
        recommendation=_str(parsed.get("recommendation")),
        closingInsult=_str(parsed.get("closingInsult"), "You're welcome."),
        confidence=min(max(float(parsed.get("confidence", 0.5)), 0.0), 1.0),
        needsMoreInfo=needs_more_info,
        suggestedResources=suggested,
        thinkingBudgetUsed=thinking_tokens,
        rawResponse=raw,
        prompt=prompt,
    )
