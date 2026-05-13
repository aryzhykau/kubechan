"""FastAPI router: health probes and /analyze endpoint."""
from __future__ import annotations

import logging
from typing import Any

from fastapi import APIRouter, HTTPException

from app.models import AnalyzeRequest, AnalyzeResponse, ExclusionRuleProposal, SuggestedResource
from app.parser import parse_llm_json
from app.prompt import build_prompt
from app.providers.base import make_provider

logger = logging.getLogger("llm-gateway")

router = APIRouter()


@router.get("/healthz")
def healthz() -> dict:
    return {"status": "ok"}


@router.get("/readyz")
def readyz() -> dict:
    return {"status": "ok"}


@router.post("/analyze", response_model=AnalyzeResponse)
async def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    logger.info(
        "analyzing evidence | evidenceId=%s incidentId=%s provider=%s",
        req.evidenceId,
        req.incidentId,
        req.provider,
    )

    prompt = build_prompt(
        req.payload,
        req.reanalysisCount,
        req.moodLevel,
        [p.model_dump() for p in req.priorDiagnoses],
        req.userMessage,
        req.incidentSource,
        req.personaEnabled,
    )

    logger.info("=== PROMPT TO MODEL ===\n%s\n=== END PROMPT ===", prompt)

    try:
        provider = make_provider(req.provider, req.credentials)
        raw, thinking_tokens = await provider.call(prompt)
    except Exception as exc:
        logger.exception("LLM provider call failed | provider=%s", req.provider)
        raise HTTPException(status_code=502, detail=f"LLM provider error: {exc}") from exc

    logger.info("=== RAW MODEL RESPONSE ===\n%s\n=== END RESPONSE ===", raw)

    try:
        parsed = parse_llm_json(raw)
    except Exception as exc:
        logger.error("failed to parse LLM JSON | raw=%s", raw[:500])
        raise HTTPException(
            status_code=502,
            detail=f"LLM response parse error: {exc}. Raw: {raw[:200]}",
        ) from exc

    def _str(val: Any, default: str = "") -> str:
        if isinstance(val, list):
            return "\n".join(str(item) for item in val)
        return str(val) if val is not None else default

    raw_suggested = parsed.get("suggestedResources") or []
    suggested = [
        SuggestedResource(kind=str(s["kind"]), apiGroup=str(s.get("apiGroup", "")), reason=str(s.get("reason", "")))
        for s in raw_suggested
        if isinstance(s, dict) and s.get("kind")
    ]

    needs_more_info = bool(parsed.get("needsMoreInfo", False)) and len(suggested) > 0

    # Parse suggestExclusionRule — only accept if it has required fields.
    suggest_exclusion_rule: ExclusionRuleProposal | None = None
    raw_exc = parsed.get("suggestExclusionRule")
    if isinstance(raw_exc, dict) and raw_exc.get("reason") and raw_exc.get("targetResources"):
        try:
            suggest_exclusion_rule = ExclusionRuleProposal(
                reason=str(raw_exc["reason"]),
                detectors=[str(d) for d in (raw_exc.get("detectors") or [])],
                targetResources=list(raw_exc.get("targetResources") or []),
                timeWindow=raw_exc.get("timeWindow"),
            )
        except Exception:
            suggest_exclusion_rule = None

    return AnalyzeResponse(
        evidenceId=req.evidenceId,
        model=provider.model_id(),
        openingRant=_str(parsed.get("openingRant")) if req.personaEnabled else "",
        likelyRootCause=_str(parsed.get("likelyRootCause"), "Unknown"),
        evidenceChain=_str(parsed.get("evidenceChain")),
        recommendation=_str(parsed.get("recommendation")),
        closingInsult=_str(parsed.get("closingInsult")) if req.personaEnabled else "",
        confidence=min(max(float(parsed.get("confidence", 0.5)), 0.0), 1.0),
        needsMoreInfo=needs_more_info,
        suggestedResources=suggested,
        suggestExclusionRule=suggest_exclusion_rule,
        thinkingBudgetUsed=thinking_tokens,
        rawResponse=raw,
        prompt=prompt,
    )
