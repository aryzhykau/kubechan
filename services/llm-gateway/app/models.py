"""Pydantic request / response models for the llm-gateway API."""
from __future__ import annotations

from typing import Any

from pydantic import BaseModel


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


class AnalyzeResponse(BaseModel):
    evidenceId: str
    model: str
    openingRant: str
    likelyRootCause: str
    evidenceChain: str
    recommendation: str
    closingInsult: str
    confidence: float
    needsMoreInfo: bool = False
    suggestedResources: list[SuggestedResource] = []
    suggestFalsePositive: bool = False
    suggestExclusionRule: ExclusionRuleProposal | None = None
    suggestedResources: list[SuggestedResource] = []
    suggestExclusionRule: ExclusionRuleProposal | None = None
    thinkingBudgetUsed: int = 0
    rawResponse: str | None = None
    prompt: str | None = None
