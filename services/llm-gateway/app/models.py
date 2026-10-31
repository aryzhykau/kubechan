"""Pydantic request / response models for the llm-gateway API."""
from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, model_validator


class PriorDiagnosis(BaseModel):
    attempt: int
    likelyRootCause: str
    userRating: str = ""  # "up", "down", or ""


class AnalyzeRequest(BaseModel):
    evidenceId: str
    diagnosticRunId: str
    incidentId: str | None = None
    incidentSource: str = "auto"  # "auto" or "manual"
    reanalysisCount: int = 0
    moodLevel: int = 0
    priorDiagnoses: list[PriorDiagnosis] = []
    userMessage: str = ""
    provider: str = "bedrock"
    credentials: dict[str, Any] = {}
    personaEnabled: bool = True
    payload: dict[str, Any]


class SuggestedResource(BaseModel):
    kind: str
    apiGroup: str = ""
    reason: str


class ExclusionRuleProposal(BaseModel):
    reason: str
    detectors: list[str]
    targetResources: list[dict]  # [{namespace, kind, name, apiGroup?}]
    timeWindow: dict | None = None  # {timezone, periods:[{start,end,days}]}


class AssessmentCitation(BaseModel):
    sourceId: str
    quote: str = ""
    relevance: str = ""


class AssessmentClaim(BaseModel):
    # Models reliably provide IDs for primary claims, but often omit them for
    # alternative hypotheses. The identifier is a UI convenience, not part of
    # citation integrity, so accepting an empty value keeps the whole
    # evidence-backed assessment from being discarded.
    id: str = ""
    statement: str
    verdict: Literal["supported", "partially_supported", "unsupported"]
    citations: list[AssessmentCitation] = []


class RemediationStep(BaseModel):
    order: int
    title: str
    description: str = ""
    command: str = ""
    risk: Literal["low", "medium", "high"] = "low"
    verification: str = ""
    citations: list[AssessmentCitation] = []


class MissingEvidence(BaseModel):
    resource: dict[str, Any] | None = None
    description: str
    reason: str = ""


class CausalPathStep(BaseModel):
    order: int
    entityId: str
    edgeId: str = ""
    candidateId: str = ""
    claimId: str = ""
    role: Literal["root_cause", "failed_dependency", "supporting_evidence", "reported_impact", "context"] = "context"


class CausalPath(BaseModel):
    entryResourceId: str
    rootCauseEntityId: str
    steps: list[CausalPathStep] = []


class AssessmentV2(BaseModel):
    version: Literal[2] = 2
    summary: str
    confidence: float
    claims: list[AssessmentClaim] = []
    alternativeHypotheses: list[AssessmentClaim] = []
    missingEvidence: list[MissingEvidence] = []
    remediation: list[RemediationStep] = []
    causalPath: CausalPath | None = None

    @model_validator(mode="before")
    @classmethod
    def normalize_model_variants(cls, value: Any) -> Any:
        """Accept common semantically-correct shapes produced by LLMs.

        Citation IDs and verdicts remain strictly validated. Only descriptive
        missing-evidence strings are promoted to their canonical object shape.
        """
        if not isinstance(value, dict):
            return value
        normalized = dict(value)
        for field_name in ("claims", "alternativeHypotheses"):
            normalized_claims = []
            for item in normalized.get(field_name) or []:
                if not isinstance(item, dict):
                    continue
                claim = dict(item)
                claim.setdefault("id", "")
                claim.setdefault("statement", str(claim.get("hypothesis") or claim.get("description") or ""))
                if not claim.get("verdict"):
                    claim["verdict"] = "partially_supported" if claim.get("citations") else "unsupported"
                normalized_claims.append(claim)
            normalized[field_name] = normalized_claims
        missing = normalized.get("missingEvidence") or []
        normalized["missingEvidence"] = [
            {"description": item} if isinstance(item, str) else item
            for item in missing
        ]
        remediation = []
        for index, item in enumerate(normalized.get("remediation") or [], start=1):
            if not isinstance(item, dict):
                continue
            step = dict(item)
            step.setdefault("order", index)
            step.setdefault("title", str(step.get("action") or step.get("description") or "Action"))
            remediation.append(step)
        normalized["remediation"] = remediation
        return normalized


class CitationValidation(BaseModel):
    valid: bool = True
    total: int = 0
    invalid: int = 0
    invalidSourceIds: list[str] = []


class AnalyzeResponse(BaseModel):
    evidenceId: str
    model: str
    openingRant: str = ""
    likelyRootCause: str
    evidenceChain: str
    recommendation: str
    closingInsult: str = ""
    confidence: float
    needsMoreInfo: bool = False
    suggestedResources: list[SuggestedResource] = []
    suggestFalsePositive: bool = False
    suggestExclusionRule: ExclusionRuleProposal | None = None
    assessmentVersion: int = 1
    assessment: AssessmentV2 | None = None
    citationValidation: CitationValidation | None = None
    thinkingBudgetUsed: int = 0
    rawResponse: str | None = None
    prompt: str | None = None
