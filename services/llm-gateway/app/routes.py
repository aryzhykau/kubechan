"""FastAPI router: health probes and /analyze endpoint."""
from __future__ import annotations

import logging
from typing import Any

from fastapi import APIRouter, HTTPException

from app.models import (
    AnalyzeRequest,
    AnalyzeResponse,
    AssessmentV2,
    CitationValidation,
    ExclusionRuleProposal,
    SuggestedResource,
)
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
            rendered: list[str] = []
            for index, item in enumerate(val, start=1):
                if isinstance(item, dict):
                    title = str(item.get("title") or item.get("description") or "Action")
                    command = str(item.get("command") or "").strip()
                    rendered.append(f"{index}. {title}" + (f" — `{command}`" if command else ""))
                else:
                    rendered.append(str(item))
            return "\n".join(rendered)
        return str(val) if val is not None else default

    raw_suggested = parsed.get("suggestedResources") or []
    suggested = [
        SuggestedResource(kind=str(s["kind"]), apiGroup=str(s.get("apiGroup", "")), reason=str(s.get("reason", "")))
        for s in raw_suggested
        if isinstance(s, dict) and s.get("kind")
    ]

    needs_more_info = bool(parsed.get("needsMoreInfo", False)) and len(suggested) > 0

    # Validate all model citations against the immutable source catalog sent in
    # this request. Unknown IDs are removed and their claims become unsupported.
    assessment: AssessmentV2 | None = None
    citation_validation: CitationValidation | None = None
    raw_assessment = parsed.get("assessment")
    if isinstance(raw_assessment, dict) and req.payload.get("sources"):
        try:
            assessment = AssessmentV2.model_validate(raw_assessment)
            valid_ids = {str(s.get("id")) for s in req.payload.get("sources", []) if isinstance(s, dict) and s.get("id")}
            suffixes: dict[str, list[str]] = {}
            for source_id in valid_ids:
                suffixes.setdefault(source_id.rsplit(":", 1)[-1], []).append(source_id)
            total = 0
            invalid_ids: list[str] = []
            for claim in [*assessment.claims, *assessment.alternativeHypotheses]:
                total += len(claim.citations)
                accepted = []
                for citation in claim.citations:
                    if citation.sourceId in valid_ids:
                        accepted.append(citation)
                    elif len(suffixes.get(citation.sourceId, [])) == 1:
                        # Some models preserve the deterministic hash but omit
                        # the human-readable type prefix. Resolve only a unique
                        # suffix and store the canonical source ID.
                        citation.sourceId = suffixes[citation.sourceId][0]
                        accepted.append(citation)
                    else:
                        invalid_ids.append(citation.sourceId)
                claim.citations = accepted
                if not accepted and claim.verdict != "unsupported":
                    claim.verdict = "unsupported"
            for step in assessment.remediation:
                total += len(step.citations)
                accepted = []
                for citation in step.citations:
                    if citation.sourceId in valid_ids:
                        accepted.append(citation)
                    elif len(suffixes.get(citation.sourceId, [])) == 1:
                        citation.sourceId = suffixes[citation.sourceId][0]
                        accepted.append(citation)
                    else:
                        invalid_ids.append(citation.sourceId)
                step.citations = accepted
            citation_validation = CitationValidation(
                valid=not invalid_ids,
                total=total,
                invalid=len(invalid_ids),
                invalidSourceIds=sorted(set(invalid_ids)),
            )
            if invalid_ids:
                assessment.confidence = min(assessment.confidence, 0.69)
        except Exception:
            logger.exception("invalid assessment v2 payload")
            assessment = None

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
        assessmentVersion=2 if assessment else 1,
        assessment=assessment,
        citationValidation=citation_validation,
        thinkingBudgetUsed=thinking_tokens,
        rawResponse=raw,
        prompt=prompt,
    )
