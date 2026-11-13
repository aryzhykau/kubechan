"""PydanticAI runner with snapshot-only tools and an explicit evidence boundary."""
from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from datetime import datetime, timezone
import logging
import math
from typing import Any, Literal

import boto3
from botocore.config import Config
from botocore.session import Session
from botocore.tokens import FrozenAuthToken
from pydantic import ValidationError
from pydantic_ai import Agent, ModelRetry, RunContext, Tool
from pydantic_ai.capabilities import AbstractCapability
from pydantic_ai.exceptions import UnexpectedModelBehavior, UsageLimitExceeded
from pydantic_ai.models import Model, ModelRequestContext
from pydantic_ai.messages import ModelResponse, ToolCallPart
from pydantic_ai.models.bedrock import BedrockConverseModel, BedrockModelSettings
from pydantic_ai.providers.bedrock import BedrockProvider
from pydantic_ai.output import OutputContext
from pydantic_ai.tools import ToolDefinition
from pydantic_ai.usage import RunUsage, UsageLimits

from app.config import resolve_model_id
from app.models import AnalyzeRequest, AnalyzeResponse, AssessmentV2, CitationValidation, TokenUsage
from app.prompt import build_agent_instructions, build_agent_prompt, build_agent_turn_instructions
from app.snapshot import SnapshotIndex, encoded, resource_key

ProgressSink = Callable[[dict[str, Any]], Awaitable[None]]
logger = logging.getLogger("llm-gateway")
OUTPUT_RETRIES = 2
SOURCE_EXCERPT_CHARS = 512


def _source_texts(source: dict[str, Any]) -> list[str]:
    event = source.get("event") or {}
    values = [source.get("value"), (source.get("line") or {}).get("text"), event.get("message"), event.get("reason")]
    return [value if isinstance(value, str) else encoded(value) for value in values if value is not None]


def _validation_code(error: ValueError) -> str:
    message = str(error)
    if message.startswith("citation references"):
        return "unread_citation"
    if message.startswith("citation quote"):
        return "citation_quote_mismatch"
    if message.startswith("supported claims"):
        return "missing_citations"
    if message.startswith("confidence"):
        return "invalid_confidence"
    if message.startswith("claim IDs"):
        return "duplicate_claim_ids"
    if any(word in message for word in ("node insight", "node insights", "uninspected resources")):
        return "invalid_node_insight"
    if any(word in message for word in ("causal", "root cause")):
        return "invalid_causal_path"
    return "invalid_assessment"


def _schema_fields(error: ValidationError) -> list[dict[str, str]]:
    # Log only known schema field names and machine-readable error types.
    # Never log Pydantic's `input`, `msg`, context or raw model output.
    allowed = {"version", "summary", "confidence", "claims", "alternativeHypotheses", "missingEvidence", "remediation", "causalPath", "nodeInsights",
               "id", "statement", "verdict", "citations", "sourceId", "quote", "relevance", "resource", "description", "reason", "order", "title",
               "command", "risk", "verification", "entryResourceId", "rootCauseEntityId", "steps", "entityId", "edgeId", "candidateId", "claimId",
               "role", "nodeId", "comment", "claimIds", "sourceIds"}
    allowed.update({"claim", "hypothesis", "review", "step", "evidence", "gap_id", "node_id", "source_ids", "item_id", "kind", "path"})
    allowed.update({"memory_patch", "graph_patch", "read", "observations", "leads", "observationIds", "removeObservationIds",
                    "removeLeadIds", "text", "question", "status", "nextCheck", "edgeIds", "candidateIds", "cards",
                    "tool", "pathPrefix", "contains", "previous", "cursor", "limit",
                    "outcome", "runbook", "action", "priority", "branches", "resource_id", "branch_id", "edge_id", "finding_id"})
    fields = []
    for issue in error.errors(include_input=False, include_context=False, include_url=False)[:8]:
        path = ".".join(str(part) if isinstance(part, int) or part in allowed else "?" for part in issue["loc"])
        fields.append({"path": path or "$", "kind": issue["type"]})
    return fields


@dataclass(frozen=True)
class AgentBudget:
    requests: int = 100
    tool_calls: int = 50
    input_tokens: int = 400_000
    output_tokens: int = 75_000
    per_request_output: int = 8_000
    seconds: float = 600
    branch_seconds: float = 120
    branch_requests: int = 12
    branch_reads: int = 10
    branch_input_tokens: int = 80_000
    branch_output_tokens: int = 16_000


class _RequestTokenSession(Session):
    """Bedrock bearer authentication scoped to one request, without env writes."""
    def __init__(self, token: str):
        super().__init__()
        self.token = token

    def get_auth_token(self, **_kwargs: Any) -> FrozenAuthToken:
        return FrozenAuthToken(self.token)

    def get_credentials(self) -> None:  # type: ignore[override]
        return None


def make_agent_model(req: AnalyzeRequest) -> Model:
    if req.provider != "bedrock":
        raise ValueError("snapshot-agent-v1 currently supports the Bedrock provider")
    creds = req.credentials
    bearer = creds.get("bearerToken")
    session = boto3.Session(
        botocore_session=_RequestTokenSession(str(bearer)) if bearer else None,
        aws_access_key_id=creds.get("accessKeyId"),
        aws_secret_access_key=creds.get("secretAccessKey"),
        aws_session_token=creds.get("sessionToken"),
        region_name=creds.get("region") or "us-east-1",
    )
    config: dict[str, Any] = {
        "connect_timeout": 5, "read_timeout": 45,
        "retries": {"total_max_attempts": 1, "mode": "standard"},
    }
    if bearer:
        config["signature_version"] = "bearer"
    client = session.client("bedrock-runtime", config=Config(**config), endpoint_url=creds.get("baseUrl"))
    return BedrockConverseModel(resolve_model_id(str(creds.get("modelId") or "qwen3-32b")), provider=BedrockProvider(bedrock_client=client))


def validate_agent_assessment(assessment: AssessmentV2, index: SnapshotIndex) -> None:
    """Reject unknown/unseen citations and incoherent overlays. Never repair from hidden data."""
    seen = index.exposed
    if not math.isfinite(assessment.confidence) or not 0 <= assessment.confidence <= 1:
        raise ValueError("confidence must be between zero and one")
    ids = [claim.id for claim in assessment.claims if claim.id]
    if len(ids) != len(set(ids)):
        raise ValueError("claim IDs must be unique")
    claims = {claim.id: claim for claim in assessment.claims if claim.id}
    for claim in [*assessment.claims, *assessment.alternativeHypotheses]:
        if claim.verdict != "unsupported" and not claim.citations:
            raise ValueError("supported claims require delivered evidence")
    for item in [*assessment.claims, *assessment.alternativeHypotheses, *assessment.remediation]:
        for citation in item.citations:
            if citation.sourceId not in seen.sourceIds:
                raise ValueError("citation references unknown or unread evidence; read the exact source first")
            source = index.sources[citation.sourceId]
            if citation.quote and not any(citation.quote in text for text in _source_texts(source)):
                raise ValueError("citation quote does not match delivered source content")
    if len({insight.nodeId for insight in assessment.nodeInsights}) != len(assessment.nodeInsights):
        raise ValueError("node insights must be unique")
    for insight in assessment.nodeInsights:
        if insight.nodeId not in seen.nodeIds or not set(insight.sourceIds) <= seen.sourceIds or not set(insight.claimIds) <= claims.keys():
            raise ValueError("node insight references unknown or unread context")
        if insight.relevance == "relevant" and not insight.sourceIds and not any(claims[c].citations for c in insight.claimIds):
            raise ValueError("relevant node insights require evidence")
        if insight.relevance == "not_relevant":
            node = index.nodes[insight.nodeId]
            own_sources = {source["id"] for source in index.by_resource.get(resource_key(node.get("resource", {})), [])}
            own_sources.update(node.get("sourceIds") or [])
            if not own_sources & seen.sourceIds:
                raise ValueError("uninspected resources cannot be classified as not relevant")
    path = assessment.causalPath
    if not path:
        return
    if path.entryResourceId != index.entry or path.rootCauseEntityId not in seen.nodeIds or not path.steps:
        raise ValueError("causal path requires the snapshot entry, a revealed root and steps")
    orders = [step.order for step in path.steps]
    if orders != sorted(set(orders)) or any(order <= 0 for order in orders):
        raise ValueError("causal path orders must be unique positive increasing values")
    root_supported = False
    for i, step in enumerate(path.steps):
        if step.entityId not in seen.nodeIds or (step.claimId and step.claimId not in claims):
            raise ValueError("causal step references unread nodes or unknown claims")
        if step.edgeId:
            if step.edgeId not in seen.edgeIds:
                raise ValueError("causal step references an unread edge")
            edge = index.edges[step.edgeId]
            endpoints = {edge["from"], edge["to"]}
            adjacent = {path.steps[j].entityId for j in (i - 1, i + 1) if 0 <= j < len(path.steps)}
            if step.entityId not in endpoints or not (endpoints - {step.entityId}) & adjacent:
                raise ValueError("causal edge must connect adjacent path entities")
        if step.candidateId:
            if step.candidateId not in seen.candidateIds:
                raise ValueError("causal step references an unread candidate")
            candidate = index.candidates[step.candidateId]
            if step.entityId not in (candidate.get("sourceNodeId"), candidate.get("referenceNodeId"), candidate.get("candidateNodeId")):
                raise ValueError("candidate does not describe this causal step")
        if step.entityId == path.rootCauseEntityId and step.role == "root_cause" and step.claimId:
            claim = claims[step.claimId]
            node = index.nodes[step.entityId]
            related_sources = set(node.get("sourceIds") or [])
            related_sources.update(source["id"] for source in index.by_resource.get(resource_key(node.get("resource", {})), []))
            root_supported = claim.verdict == "supported" and any(c.sourceId in related_sources for c in claim.citations)
    if not root_supported:
        raise ValueError("root cause requires a supported claim citing delivered evidence for that entity")
    if index.entry not in {step.entityId for step in path.steps}:
        raise ValueError("causal path must reach the investigation entry")
    for left, right in zip(path.steps, path.steps[1:]):
        pair = {left.entityId, right.entityId}
        connected = any({index.edges[e]["from"], index.edges[e]["to"]} == pair for e in (left.edgeId, right.edgeId) if e)
        proposed = any({index.candidates[c]["sourceNodeId"], index.candidates[c]["candidateNodeId"]} == pair for c in (left.candidateId, right.candidateId) if c)
        if not connected and not proposed:
            raise ValueError("adjacent causal entities require a delivered relationship")


def project_agent_citations(assessment: AssessmentV2, index: SnapshotIndex) -> tuple[AssessmentV2, dict[str, Any]]:
    """Resolve display excerpts from delivered sources under an explicit audit policy.

    The model selects source IDs. Its quote strings are never treated as evidence.
    Validate every source/claim/graph reference before accessing source contents,
    then apply the same strict quote validation to the canonical result.
    """
    projected = assessment.model_copy(deep=True)
    citations = [citation for item in [*projected.claims, *projected.alternativeHypotheses, *projected.remediation] for citation in item.citations]
    ignored = sum(bool(citation.quote) for citation in citations)
    for citation in citations:
        citation.quote = ""
    validate_agent_assessment(projected, index)
    for citation in citations:
        text = next((value for value in _source_texts(index.sources[citation.sourceId]) if value), "")
        citation.quote = text[:SOURCE_EXCERPT_CHARS]
    validate_agent_assessment(projected, index)
    return projected, {"policy": "snapshot-excerpt-v1", "maxChars": SOURCE_EXCERPT_CHARS,
                       "count": len(citations), "ignoredModelQuotes": ignored}


@dataclass
class SnapshotSession:
    index: SnapshotIndex
    request: AnalyzeRequest
    budget: AgentBudget
    sink: ProgressSink
    calls: int = 0
    wrap_up: bool = False
    last_input_tokens: int = 0
    last_output_tokens: int = 0
    output_rejections: list[dict[str, Any]] = field(default_factory=list)
    finalizing_announced: bool = False
    citation_quotes: dict[str, Any] = field(default_factory=dict)

    @property
    def max_output_tokens(self) -> int:
        return min(self.budget.per_request_output, max(256, int(self.request.credentials.get("maxTokens") or self.budget.per_request_output)))

    async def emit(self, stage: str, phase: str = "started", **details: Any) -> None:
        await self.sink({"stage": stage, "phase": phase, "occurredAt": datetime.now(timezone.utc).isoformat(), **details})

    async def reject_output(self, code: str, fields: list[dict[str, str]] | None = None) -> None:
        issue = {"code": code, "fields": fields or []}
        self.output_rejections.append(issue)
        logger.warning("snapshot assessment rejected | runId=%s reasonCode=%s fields=%s",
                       self.request.diagnosticRunId, code, encoded(issue["fields"]))
        await self.emit("validating", "failed", reasonCode=code, validationFields=issue["fields"])

    def read_progress(self, name: str, result: dict) -> dict:
        return {}

    def read_started(self, name: str, node_id: str) -> dict:
        return {}

    async def invoke(self, name: str, read: Callable[[], dict], tool_call_id: str, node_id: str = "", previous_logs: bool = False) -> str:
        self.calls += 1
        if self.calls > self.budget.tool_calls:
            raise ValueError("tool-call budget exhausted")
        # Resolve display metadata from the frozen graph, never from model prose.
        details: dict[str, Any] = {"toolCallId": tool_call_id}
        if node_id in self.index.allowed_nodes:
            resource = self.index.nodes[node_id].get("resource", {})
            details["target"] = {"nodeId": node_id, "resource": {key: resource[key] for key in ("kind", "name", "namespace", "apiGroup") if key in resource}}
        if name == "get_logs":
            details["previousLogs"] = previous_logs
        await self.emit(name, **details, **self.read_started(name, node_id))
        try:
            result = read()
        except ValueError as exc:
            # Only deterministic tool-policy messages reach the model.
            await self.emit(name, "failed", reasonCode="snapshot_read_failed", **details)
            self.index.no_progress += 1
            return encoded({"error": str(exc), "finalizeIfBudgetExhausted": True})
        await self.emit(name, "completed", returnedCount=result.get("returnedCount", 0),
                        **({"availability": result["availability"]} if "availability" in result else {}), **details, **self.read_progress(name, result))
        return encoded(result)


class _OutputDiagnostics(AbstractCapability[SnapshotSession]):
    async def after_model_request(self, ctx: RunContext[SnapshotSession], *, request_context: ModelRequestContext,
                                  response: ModelResponse) -> ModelResponse:
        parameters = request_context.model_request_parameters
        available = {tool.name for tool in [*parameters.function_tools, *parameters.output_tools]}
        if any(isinstance(part, ToolCallPart) and part.tool_name not in available for part in response.parts):
            # Persist the protocol error without the untrusted tool name/arguments.
            await ctx.deps.reject_output("unavailable_tool")
        return response

    async def on_output_validate_error(self, ctx: RunContext[SnapshotSession], *, output_context: OutputContext,
                                       output: str | dict[str, Any], error: ValidationError | ModelRetry) -> Any:
        if isinstance(error, ValidationError):
            fields = _schema_fields(error)
            code = "output_json_invalid" if any(item["kind"] == "json_invalid" for item in fields) else "output_schema_invalid"
            await ctx.deps.reject_output(code, fields)
            raise ModelRetry(
                f"Invalid AssessmentV2 fields: {encoded(fields)}. Return a complete corrected assessment object, without an outer wrapper. "
                "Always include summary and numeric confidence. Every supported/partially_supported claim needs exact delivered source citations; "
                "otherwise mark it unsupported and record missingEvidence. Optional nodeInsights and causalPath may be omitted."
            ) from error
        raise error


def _budget_wrap_up(deps: SnapshotSession, usage: RunUsage, *, stop_on_stale_reads: bool = True) -> bool:
    # Another exploratory request also consumes the accumulated history. Reserve
    # room for finalization and bounded validation repairs before allowing it.
    # Input growth is a forecast; provider-reported hard limits still apply.
    input_forecast = (deps.last_input_tokens * 3 + 1) // 2
    output_forecast = min(deps.max_output_tokens, (deps.last_output_tokens * 3 + 1) // 2)
    request_reserve = min(OUTPUT_RETRIES + 1, max(1, deps.budget.requests - 1))
    token_pressure = usage.requests > 0 and (
        usage.input_tokens + (request_reserve + 1) * input_forecast > deps.budget.input_tokens
        or usage.output_tokens + output_forecast + 2 * deps.max_output_tokens > deps.budget.output_tokens
    )
    stalled = stop_on_stale_reads and deps.index.no_progress >= 2
    if deps.wrap_up or usage.requests >= deps.budget.requests - request_reserve or token_pressure or stalled or deps.calls >= deps.budget.tool_calls:
        deps.wrap_up = True
    return deps.wrap_up


def _should_wrap_up(ctx: RunContext[SnapshotSession]) -> bool:
    return _budget_wrap_up(ctx.deps, ctx.usage)


async def _prepare(ctx: RunContext[SnapshotSession], definition: ToolDefinition) -> ToolDefinition | None:
    return None if _should_wrap_up(ctx) else definition


def build_snapshot_agent(model: Model, persona_enabled: bool = False) -> Agent[SnapshotSession, AssessmentV2]:
    async def get_graph_neighbors(ctx: RunContext[SnapshotSession], node_id: str, direction: Literal["both", "in", "out"] = "both", relation_types: list[str] | None = None, cursor: str | None = None, limit: int = 10) -> str:
        """Reveal one-hop graph relationships and proposed candidates. Follow pagination; source IDs alone are not evidence values."""
        return await ctx.deps.invoke("get_graph_neighbors", lambda: ctx.deps.index.neighbors(node_id, direction, relation_types, cursor, limit), ctx.tool_call_id or "", node_id)

    async def get_resource(ctx: RunContext[SnapshotSession], node_id: str, path_prefix: str = "", cursor: str | None = None, limit: int = 25) -> str:
        """Read exact collected fields with optional path filtering and pagination. Missing references include original referring-resource records. This is not a live kubectl describe."""
        return await ctx.deps.invoke("get_resource", lambda: ctx.deps.index.observations(node_id, "resource_field", path_prefix=path_prefix, cursor=cursor, limit=limit), ctx.tool_call_id or "", node_id)

    async def get_events(ctx: RunContext[SnapshotSession], node_id: str, contains: str = "", cursor: str | None = None, limit: int = 10) -> str:
        """Read already collected events for a revealed resource, optionally using literal text search."""
        return await ctx.deps.invoke("get_events", lambda: ctx.deps.index.observations(node_id, "event", contains=contains, cursor=cursor, limit=limit), ctx.tool_call_id or "", node_id)

    async def get_logs(ctx: RunContext[SnapshotSession], node_id: str, previous: bool = False, contains: str = "", cursor: str | None = None, limit: int = 10) -> str:
        """Read bounded atomic log lines already in this snapshot. Missing previous logs cannot be fetched live."""
        return await ctx.deps.invoke("get_logs", lambda: ctx.deps.index.observations(node_id, "log_line", contains=contains, previous=previous, cursor=cursor, limit=limit), ctx.tool_call_id or "", node_id, previous)

    async def read_sources(ctx: RunContext[SnapshotSession], source_ids: list[str]) -> str:
        """Read 1..10 exact source records whose IDs were revealed by the graph or resource tools."""
        return await ctx.deps.invoke("read_sources", lambda: ctx.deps.index.read_sources(source_ids), ctx.tool_call_id or "")

    # The default also governs unknown/closed tools in PydanticAI. Allow a bounded
    # correction of that protocol mistake; registered snapshot tools retain zero
    # retries below, and closed tools are never executed.
    agent = Agent(model, deps_type=SnapshotSession, output_type=AssessmentV2, instructions=build_agent_instructions(persona_enabled), retries={"tools": OUTPUT_RETRIES, "output": OUTPUT_RETRIES}, tool_timeout=5,
                  capabilities=[_OutputDiagnostics()],
                  tools=[Tool(get_graph_neighbors, prepare=_prepare, sequential=True, max_retries=0),
                         Tool(get_resource, prepare=_prepare, sequential=True, max_retries=0),
                         Tool(get_events, prepare=_prepare, sequential=True, max_retries=0),
                         Tool(get_logs, prepare=_prepare, sequential=True, max_retries=0),
                         Tool(read_sources, prepare=_prepare, sequential=True, max_retries=0)])

    @agent.instructions
    async def turn_instructions(ctx: RunContext[SnapshotSession]) -> str:
        finalizing = _should_wrap_up(ctx)
        if finalizing and not ctx.deps.finalizing_announced:
            ctx.deps.finalizing_announced = True
            await ctx.deps.emit("finalizing")
        return build_agent_turn_instructions(max(0, ctx.deps.budget.requests - ctx.usage.requests), finalizing)

    @agent.output_validator
    async def validate(ctx: RunContext[SnapshotSession], assessment: AssessmentV2) -> AssessmentV2:
        await ctx.deps.emit("validating")
        try:
            assessment, quote_audit = project_agent_citations(assessment, ctx.deps.index)
        except ValueError as exc:
            code = _validation_code(exc)
            await ctx.deps.reject_output(code)
            feedback = str(exc) + ". Return a complete corrected AssessmentV2 within the remaining request budget. "
            if code == "missing_citations":
                paths = [f"{name}.{i}.citations" for name in ("claims", "alternativeHypotheses")
                         for i, claim in enumerate(getattr(assessment, name)) if claim.verdict != "unsupported" and not claim.citations]
                feedback += f"Missing citations at: {encoded(paths[:8])}. Add exact sourceId citations from delivered source records that support each claim. "
                feedback += "If no delivered source supports it, explicitly change its verdict to unsupported and describe missingEvidence, or remove it. Never invent a citation."
            elif code == "unread_citation":
                delivered = sorted(ctx.deps.index.exposed.sourceIds)
                unread = sorted({c.sourceId for item in [*assessment.claims, *assessment.alternativeHypotheses, *assessment.remediation]
                                 for c in item.citations if c.sourceId not in ctx.deps.index.exposed.sourceIds})
                feedback += f"Unread source IDs: {encoded(unread[:8])}. Delivered source IDs (up to 8): {encoded(delivered[:8])}. "
                feedback += "Cite only exact delivered IDs whose content supports the claim. If tools are unavailable, remove unsupported claims or mark them unsupported and record missingEvidence; do not invent citations."
            elif code == "invalid_causal_path":
                feedback += "Use only delivered, connected relationships. You may return causalPath=null when evidence does not support a complete causal chain; retain separately supported claims and describe the gap in missingEvidence."
            elif code == "invalid_node_insight":
                feedback += "Node insights are optional and sparse; omit entries you cannot support with inspected nodes and delivered sources."
            raise ModelRetry(feedback) from exc
        ctx.deps.citation_quotes = quote_audit
        await ctx.deps.emit("validating", "completed", reasonCode="snapshot_quotes")
        return assessment

    return agent


async def run_snapshot_agent(req: AnalyzeRequest, sink: ProgressSink, *, model: Model | None = None, budget: AgentBudget | None = None) -> AnalyzeResponse:
    budget = budget or AgentBudget()
    await sink({"stage": "indexing", "phase": "started"})
    index = SnapshotIndex(req.payload)
    bootstrap = index.bootstrap()
    prompt = build_agent_prompt(req.payload, bootstrap, req.userMessage)
    session = SnapshotSession(index, req, budget, sink)
    await session.emit("indexing", "completed", snapshotHash=index.snapshot_hash)
    model = model or make_agent_model(req)
    agent = build_snapshot_agent(model, req.personaEnabled)
    usage_limits = UsageLimits(request_limit=budget.requests, tool_calls_limit=budget.tool_calls, input_tokens_limit=budget.input_tokens, output_tokens_limit=budget.output_tokens)
    settings: BedrockModelSettings = {"max_tokens": session.max_output_tokens}
    if req.credentials.get("temperature") not in (None, ""):
        settings["temperature"] = float(req.credentials["temperature"])
    thinking_budget = int(req.credentials.get("thinkingBudget") or 0)
    if thinking_budget > 0:
        settings["bedrock_additional_model_requests_fields"] = {"thinking": {"type": "enabled", "budget_tokens": thinking_budget}}
    async with asyncio.timeout(budget.seconds):
        async with agent.iter(prompt, deps=session, usage_limits=usage_limits, model_settings=settings) as run:
            node = run.next_node
            while not Agent.is_end_node(node):
                is_request = Agent.is_model_request_node(node)
                if is_request and run.usage.requests < budget.requests:
                    await session.emit("model_request", turn=run.usage.requests + 1)
                previous_input, previous_output = run.usage.input_tokens, run.usage.output_tokens
                try:
                    node = await run.next(node)
                except Exception as exc:
                    if isinstance(exc, UsageLimitExceeded):
                        # SDK messages are used only to select a fixed limit key;
                        # their raw text is never forwarded to progress or logs.
                        for marker, kind, used, maximum in (
                            ("request_limit", "requests", run.usage.requests, budget.requests),
                            ("input_tokens_limit", "input_tokens", run.usage.input_tokens, budget.input_tokens),
                            ("output_tokens_limit", "output_tokens", run.usage.output_tokens, budget.output_tokens),
                            ("tool_calls_limit", "tool_calls", session.calls, budget.tool_calls),
                        ):
                            if marker in str(exc):
                                await session.emit("execution_limit", "failed", executionLimit={"kind": kind, "used": used, "maximum": maximum})
                                break
                    if isinstance(exc, UnexpectedModelBehavior) and not session.output_rejections:
                        await session.reject_output("unusable_model_response")
                    # Only numeric accounting, never raw provider exceptions or
                    # model/evidence content. A failed response may include usage.
                    logger.warning("snapshot agent stopped | requests=%d inputTokens=%d outputTokens=%d toolCalls=%d lastValidationCode=%s",
                                   run.usage.requests, run.usage.input_tokens, run.usage.output_tokens, session.calls,
                                   session.output_rejections[-1]["code"] if session.output_rejections else "none")
                    raise
                if is_request:
                    session.last_input_tokens = run.usage.input_tokens - previous_input
                    session.last_output_tokens = run.usage.output_tokens - previous_output
                    index.acknowledge_delivery()
            if run.result is None:
                raise ValueError("agent finished without a structured assessment")
            assessment = run.result.output
            validate_agent_assessment(assessment, index)
            usage = run.usage
    total_citations = sum(len(item.citations) for item in [*assessment.claims, *assessment.alternativeHypotheses, *assessment.remediation])
    state = "partial" if session.wrap_up else "completed"
    return AnalyzeResponse(
        evidenceId=req.evidenceId, model=f"bedrock/{model.model_name}",
        likelyRootCause=assessment.summary,
        evidenceChain="\n".join(claim.statement for claim in assessment.claims),
        recommendation="\n".join(f"{step.order}. {step.title}" + (f" — {step.command}" if step.command else "") for step in assessment.remediation),
        confidence=assessment.confidence, needsMoreInfo=bool(assessment.missingEvidence),
        assessmentVersion=2, assessment=assessment, citationValidation=CitationValidation(total=total_citations),
        analysisMode="snapshot-agent-v1", prompt=build_agent_instructions(req.personaEnabled) + "\n" + prompt,
        thinkingBudgetUsed=int(usage.details.get("reasoning_tokens", 0)),
        tokenUsage=TokenUsage(inputTokens=usage.input_tokens, outputTokens=usage.output_tokens, totalTokens=usage.input_tokens + usage.output_tokens, inputSource="provider", outputSource="provider"),
        agentExecution={"version": 1, "analysisRequestId": req.analysisRequestId, "attemptId": req.attemptId,
                        "snapshotHash": index.snapshot_hash, "status": state,
                        "stopReason": "budget_or_no_new_facts" if session.wrap_up else "final_answer",
                        "requests": usage.requests, "toolCalls": session.calls,
                        "contextBytes": index.context_bytes, "exposed": index.exposed.as_dict(), "contextJournal": index.journal,
                        "validationIssues": session.output_rejections, "citationQuotes": session.citation_quotes},
    )
