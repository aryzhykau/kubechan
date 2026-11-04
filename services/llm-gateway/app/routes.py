"""FastAPI router: health probes and /analyze endpoint."""
from __future__ import annotations

import logging
from typing import Any, Literal

from fastapi import APIRouter, HTTPException

from app.models import (
    AnalyzeRequest,
    AnalyzeResponse,
    AssessmentCitation,
    AssessmentV2,
    CitationValidation,
    ExclusionRuleProposal,
    SuggestedResource,
    TokenUsage,
)
from app.parser import parse_llm_json
from app.prompt import build_prompt
from app.providers.base import make_provider
from app.tokenizer import estimate_token_usage

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
        token_usage = estimate_token_usage(prompt, raw, provider.model_id(), provider.token_usage)
    except Exception as exc:
        # Provider exceptions can contain request details. Keep credentials and
        # evidence out of API error bodies and gateway logs.
        logger.error("LLM provider call failed | provider=%s errorType=%s", req.provider, type(exc).__name__)
        raise HTTPException(status_code=502, detail=f"LLM provider error ({type(exc).__name__})") from exc

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
            original_verdicts: dict[int, Literal["supported", "partially_supported", "unsupported"]] = {
                id(claim): claim.verdict
                for claim in [*assessment.claims, *assessment.alternativeHypotheses]
            }
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

            # The model may annotate the graph, but it cannot add topology.
            # Drop any causal path that does not resolve completely against the
            # immutable graph included in this request.
            graph = req.payload.get("graph") or {}
            node_ids = {str(n.get("id")) for n in graph.get("nodes", []) if isinstance(n, dict) and n.get("id")}
            claim_ids = {claim.id for claim in assessment.claims if claim.id}
            validated_insights = []
            seen_insight_nodes: set[str] = set()
            for insight in assessment.nodeInsights:
                if insight.nodeId not in node_ids or insight.nodeId in seen_insight_nodes:
                    continue
                insight.sourceIds = sorted({source_id for source_id in insight.sourceIds if source_id in valid_ids})
                insight.claimIds = sorted({claim_id for claim_id in insight.claimIds if claim_id in claim_ids})
                if insight.relevance == "relevant" and not insight.sourceIds and not insight.claimIds:
                    insight.relevance = "context"
                seen_insight_nodes.add(insight.nodeId)
                validated_insights.append(insight)
            assessment.nodeInsights = validated_insights

            if assessment.causalPath and graph:
                edge_ids = {str(e.get("id")) for e in graph.get("edges", []) if isinstance(e, dict) and e.get("id")}
                candidate_ids = {str(c.get("id")) for c in graph.get("candidates", []) if isinstance(c, dict) and c.get("id")}
                path = assessment.causalPath
                valid_path = (
                    path.entryResourceId == graph.get("entryResourceId")
                    and path.rootCauseEntityId in node_ids
                    and bool(path.steps)
                )
                for graph_step in path.steps:
                    valid_path = valid_path and graph_step.entityId in node_ids
                    valid_path = valid_path and (not graph_step.edgeId or graph_step.edgeId in edge_ids)
                    valid_path = valid_path and (not graph_step.candidateId or graph_step.candidateId in candidate_ids)
                if not valid_path:
                    assessment.causalPath = None

            # A causal-path step is itself constrained to deterministic graph
            # entities. If the model mangled a citation ID, recover the exact
            # source IDs attached to that entity/relationship instead of
            # presenting a graph-backed root-cause claim as unsupported.
            # This never invents evidence: recovered IDs must also exist in the
            # immutable source catalog above.
            if assessment.causalPath:
                nodes_by_id = {
                    str(node.get("id")): node
                    for node in graph.get("nodes", [])
                    if isinstance(node, dict) and node.get("id")
                }
                edges_by_id = {
                    str(edge.get("id")): edge
                    for edge in graph.get("edges", [])
                    if isinstance(edge, dict) and edge.get("id")
                }
                candidates_by_id = {
                    str(candidate.get("id")): candidate
                    for candidate in graph.get("candidates", [])
                    if isinstance(candidate, dict) and candidate.get("id")
                }
                claims_by_id = {claim.id: claim for claim in assessment.claims if claim.id}
                for graph_step in assessment.causalPath.steps:
                    claim = claims_by_id.get(graph_step.claimId)
                    if not claim or claim.citations:
                        continue
                    recovered: list[str] = []
                    node = nodes_by_id.get(graph_step.entityId, {})
                    recovered.extend(node.get("sourceIds") or [])
                    if graph_step.edgeId:
                        recovered.extend(edges_by_id.get(graph_step.edgeId, {}).get("sourceIds") or [])
                    if graph_step.candidateId:
                        candidate = candidates_by_id.get(graph_step.candidateId, {})
                        for reason in candidate.get("reasons") or []:
                            if isinstance(reason, dict):
                                recovered.extend(reason.get("sourceIds") or [])
                    recovered = sorted({source_id for source_id in recovered if source_id in valid_ids})
                    if recovered:
                        claim.citations = [
                            AssessmentCitation(sourceId=source_id, relevance="Deterministic graph evidence")
                            for source_id in recovered
                        ]
                        claim.verdict = original_verdicts[id(claim)]
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
        confidence=assessment.confidence if assessment else min(max(float(parsed.get("confidence", 0.5)), 0.0), 1.0),
        needsMoreInfo=needs_more_info,
        suggestedResources=suggested,
        suggestExclusionRule=suggest_exclusion_rule,
        assessmentVersion=2 if assessment else 1,
        assessment=assessment,
        citationValidation=citation_validation,
        thinkingBudgetUsed=thinking_tokens,
        tokenUsage=token_usage,
        rawResponse=raw,
        prompt=prompt,
    )
