"""Incremental assessment over a read-only, immutable snapshot."""
from __future__ import annotations

import asyncio
from copy import deepcopy
from dataclasses import dataclass, field, replace
from itertools import zip_longest
import json
from typing import Literal

from pydantic_ai import Agent, ModelRetry, RunContext, Tool
from pydantic_ai.messages import ModelRequest, ModelResponse, UserPromptPart
from pydantic_ai.models import Model
from pydantic_ai.usage import RunUsage, UsageLimits

from app import agent as legacy
from app.models import (AnalyzeRequest, AnalyzeResponse, AssessmentCitation, AssessmentClaim, AssessmentV2,
                        CitationValidation, MissingEvidence, NodeInsight, RemediationStep, TokenUsage)
from app.prompt import build_agent_instructions, build_agent_prompt
from app.snapshot import Exposure, SnapshotIndex, digest, encoded, resource_key

MAX_WRITES = 49  # The last of the backend's 50 draft revisions is reserved for finish.
MAX_DRAFT_BYTES = 48_000
CONCLUSION_RESERVE_BYTES = 1024
MAX_READS_PER_TURN = 3
MAX_REPEATS = 3
FAILED_STATES = {"degraded", "missing", "broken", "unhealthy", "failed"}
PREVIEW = {
    "fields": (14, 3500), "events": (4, 1700), "currentLogs": (2, 900),
    "previousLogs": (2, 900), "collectionErrors": (2, 700),
}
STAGES = {"fields": "get_resource", "events": "get_events", "currentLogs": "get_logs",
          "previousLogs": "get_logs", "collectionErrors": "get_resource"}
Verdict = Literal["supported", "partially_supported", "unsupported"]


class InvestigationDiagnostics(legacy._OutputDiagnostics):
    async def before_model_request(self, ctx, request_context):
        messages = request_context.messages
        if len(messages) >= 3 and isinstance(messages[-2], ModelResponse) and isinstance(messages[-1], ModelRequest):
            # Keep the matching assistant calls and tool results for Bedrock.
            initial = replace(messages[0], parts=[UserPromptPart(
                ctx.deps.startup_prompt + "\n\nInvestigation state (data):\n" + encoded(ctx.deps.working_state())
            )])
            request_context.messages = [initial, *messages[-2:]]
        return request_context

    async def before_tool_validate(self, ctx, *, call, tool_def, args):
        aliases = {"resourceId": "resource_id", "node_id": "resource_id", "nodeId": "resource_id"} if tool_def.name == "inspect" else (
            {"sourceIds": "source_ids", "findingId": "finding_id"} if tool_def.name == "record_finding" else {})
        if not aliases:
            return args
        try:
            data = json.loads(args) if isinstance(args, str) else args
        except ValueError:
            return args
        if not isinstance(data, dict):
            return args
        return {aliases.get(key, key): value for key, value in data.items() if key not in aliases or aliases[key] not in data}

    async def on_output_validate_error(self, ctx, *, output_context, output, error):
        await ctx.deps.reject_output("draft_not_finalized")
        raise error


@dataclass
class Draft:
    assessment: AssessmentV2 = field(default_factory=lambda: AssessmentV2(summary="", confidence=0))
    revision: int = 0
    has_conclusion: bool = False

    def view(self, index: SnapshotIndex) -> dict:
        nodes = {item.nodeId for item in self.assessment.nodeInsights}
        edges = [identity for identity in sorted(index.exposed.edgeIds)
                 if {index.edges[identity]["from"], index.edges[identity]["to"]} <= nodes]
        candidates = [identity for identity in sorted(index.exposed.candidateIds)
                      if {index.candidates[identity].get(key) for key in ("sourceNodeId", "referenceNodeId", "candidateNodeId")} <= nodes]
        return {"version": 1, "revision": self.revision, "hasConclusion": self.has_conclusion,
                "assessment": self.assessment.model_dump(),
                "graph": {"nodeIds": sorted(nodes), "edgeIds": edges, "candidateIds": candidates}}


@dataclass(frozen=True)
class SectionPage:
    resource_id: str
    section: str
    offset: str


@dataclass(frozen=True)
class VerifiedDraft:
    draft: Draft
    assessment: AssessmentV2
    citation_quotes: dict


@dataclass
class InvestigationSession(legacy.SnapshotSession):
    draft: Draft = field(default_factory=Draft)
    startup_prompt: str = ""
    reads_in_turn: int = 0
    repeated_reads: int = 0
    repeated_writes: int = 0
    writes: int = 0
    verified: VerifiedDraft | None = None
    finalized: AssessmentV2 | None = None
    review_until: int | None = None
    review_targets: set[str] = field(default_factory=set)
    shown_nodes: set[str] = field(default_factory=set)
    shown_sources: set[str] = field(default_factory=set)
    shown_edges: set[str] = field(default_factory=set)
    shown_candidates: set[str] = field(default_factory=set)
    cursors: dict[str, SectionPage] = field(default_factory=dict)
    receipts: dict[str, dict] = field(default_factory=dict)

    def can_read(self) -> bool:
        return self.verified is None and not self.wrap_up and (
            self.review_until is None or bool(self.review_targets - self.shown_nodes)
        )

    def can_record(self) -> bool:
        return self.verified is None and self.writes < MAX_WRITES and self.repeated_writes < MAX_REPEATS

    async def advance_phase(self, usage: RunUsage) -> None:
        if self.verified is not None:
            return
        if self.review_until is not None:
            if usage.requests >= self.review_until or not self.can_record():
                await self.verify(usage.requests)
            return
        self.wrap_up |= self.repeated_reads >= MAX_REPEATS or not self.can_record()
        if legacy._budget_wrap_up(self, usage, stop_on_stale_reads=False):
            await self.verify(usage.requests)

    def graph_scope(self) -> dict:
        return {"nodeIds": sorted(self.shown_nodes | {self.index.entry}), "edgeIds": sorted(self.shown_edges),
                "candidateIds": sorted(self.shown_candidates), "sourceIds": sorted(self.shown_sources)}

    def unread_receipt(self, receipt: dict) -> dict:
        sections = {}
        for name, page in receipt["sections"].items():
            cursor = page.get("cursor")
            while cursor in self.receipts:
                cursor = self.receipts[cursor]["sections"][name].get("cursor")
            sections[name] = {key: value for key, value in page.items() if key != "cursor"}
            if cursor:
                sections[name]["cursor"] = cursor
        return {**receipt, "sections": sections}

    def read_started(self, name: str, node_id: str) -> dict:
        if node_id and node_id in self.index.allowed_nodes:
            self.shown_nodes.add(node_id)
            return {"investigationGraph": self.graph_scope()}
        return {}

    def read_progress(self, name: str, result: dict) -> dict:
        if result.get("status") == "already_read":
            return {"reasonCode": "snapshot_read_repeated"}
        items = result.get("items", [])
        self.shown_sources.update(source["id"] for source in items)
        nodes = self.shown_nodes | {self.index.entry}
        unconfirmed: set[str] = set()
        for edge in self.index.edges.values():
            target = self.index.nodes[edge["to"]]
            if edge.get("origin") == "observed" and edge.get("state") == "broken" and target.get("type") == "reference" and target.get("state") == "missing":
                proof = set(edge.get("sourceIds") or []) | set(target.get("sourceIds") or [])
                if not self.shown_sources.intersection(proof):
                    unconfirmed.add(edge["id"])
                elif edge["from"] in nodes:
                    self.shown_nodes.add(edge["to"])
        nodes = self.shown_nodes | {self.index.entry}
        self.shown_edges.update(edge["id"] for edge in self.index.edges.values()
                                if edge["id"] not in unconfirmed and {edge["from"], edge["to"]} <= nodes)
        self.shown_candidates.update(candidate["id"] for candidate in self.index.candidates.values()
                                     if {candidate.get(key) for key in ("sourceNodeId", "referenceNodeId", "candidateNodeId")} <= nodes)
        return {"investigationGraph": self.graph_scope()}

    def working_state(self) -> dict:
        if self.verified is not None:
            return {"verifiedRevision": self.verified.draft.revision}
        groups: dict[tuple, dict] = {}
        seen: set[str] = set()
        for read in reversed(self.index.journal):
            for identity in read["sourceIds"]:
                if identity in seen:
                    continue
                seen.add(identity)
                source = self.index.sources[identity]
                resource = source.get("resource") or {}
                fact = {key: source[key] for key in ("id", "path", "value", "event", "line", "truncated", "redacted") if key in source}
                if len(encoded(fact).encode()) <= 1200:
                    group = groups.setdefault(resource_key(resource), {"resource": resource, "items": []})
                    group["items"].append(fact)
        facts = [{"resource": group["resource"], "items": []} for group in groups.values()]
        remaining = 9000 - len(encoded(facts).encode())
        # Preserve comparisons across resources even when one had a large page.
        for round_of_facts in zip_longest(*(group["items"] for group in groups.values())):
            for group, fact in zip(facts, round_of_facts):
                if fact is None:
                    continue
                size = len(encoded(fact).encode()) + 1
                if size <= remaining:
                    group["items"].append(fact)
                    remaining -= size
            if remaining < 100:
                break
        assessment = self.draft.assessment
        findings = [{"id": claim.id, "statement": claim.statement[:400], "verdict": claim.verdict,
                     "sourceIds": [citation.sourceId for citation in claim.citations]}
                    for claim in [*assessment.claims, *assessment.alternativeHypotheses]]
        checks = []
        remaining = 4500
        for receipt in reversed(self.receipts.values()):
            receipt = self.unread_receipt(receipt)
            size = len(encoded(receipt).encode()) + 1
            if size <= remaining:
                checks.append(receipt)
                remaining -= size
            if remaining < 100 or len(checks) == 12:
                break
        latest = checks[0]["resourceId"] if checks else self.index.entry
        unchecked = [item for item in self.related(latest) if item["resourceId"] not in self.shown_nodes]
        return {"findings": findings, "recentChecks": checks, "recentEvidence": [group for group in facts if group["items"]],
                "uninspectedNeighbors": unchecked[:4], "unrecordedRelationships": self.unrecorded_relationships()}

    def unrecorded_relationships(self) -> list[dict]:
        cited = [{citation.sourceId for citation in claim.citations} for claim in self.draft.assessment.claims]
        checks = []
        for identity in sorted(self.shown_edges):
            edge = self.index.edges[identity]
            if edge.get("origin") != "inferred" and edge.get("state") != "broken":
                continue
            source_ids = set(edge.get("sourceIds") or [])
            if not source_ids or not source_ids <= self.index.exposed.sourceIds or any(source_ids <= sources for sources in cited):
                continue
            evidence = []
            for source_id in sorted(source_ids)[:6]:
                source = self.index.sources[source_id]
                keys = ("id", "resource", "path", "value", "event")
                fact = {key: source[key] for key in keys if key in source}
                evidence.append(fact if len(encoded(fact).encode()) <= 450 else {"id": source_id, "path": source.get("path")})
            checks.append({"from": edge["from"], "to": edge["to"], "type": edge.get("type"), "state": edge.get("state"),
                           "origin": edge.get("origin"), "evidence": evidence})
            if len(checks) == 3:
                break
        return checks

    def unread_review_targets(self) -> list[str]:
        targets = []
        checked = self.shown_nodes | {self.index.entry}
        for node in [self.index.entry, *sorted(checked - {self.index.entry})]:
            for link in self.related(node):
                target = link["resourceId"]
                if target in checked or target in targets or target not in self.index.allowed_nodes:
                    continue
                if link["state"] == "broken" or link["origin"] == "inferred" or link["resourceState"] in FAILED_STATES:
                    targets.append(target)
                    if len(targets) == 2:
                        return targets
        return targets

    def related(self, resource_id: str) -> list[dict]:
        related = []
        for edge in self.index.edges.values():
            if resource_id not in (edge["from"], edge["to"]):
                continue
            outgoing = edge["from"] == resource_id
            neighbor = edge["to"] if outgoing else edge["from"]
            link = {"resourceId": neighbor, "resourceState": self.index.nodes[neighbor].get("state", "unknown"),
                    "type": edge.get("type", ""), "state": edge.get("state", ""), "origin": edge.get("origin", ""),
                    "direction": "out" if outgoing else "in"}
            if edge.get("basis"):
                link["basis"] = edge["basis"]
            related.append(link)
        return sorted(related, key=lambda item: (item["resourceId"] in self.shown_nodes,
                                                 item["resourceState"] not in FAILED_STATES,
                                                 item["state"] not in FAILED_STATES, item["resourceId"], item["type"]))

    def section_page(self, resource_id: str, section: str, records: list[dict], offset: str | None,
                     limit: int, byte_budget: int | None = None) -> tuple[list[dict], dict]:
        query = {"nodeId": resource_id, "section": section}
        items, next_offset, omitted = self.index._page("inspect_section", query, records, offset, limit, byte_budget=byte_budget)
        page = {"available": bool(records), "total": len(records), "sourceIds": [item["id"] for item in items]}
        if omitted:
            page["omittedSourceIds"] = omitted
        if next_offset:
            cursor = "page:" + digest([self.index.snapshot_hash, query, next_offset])[:16]
            self.cursors[cursor] = SectionPage(resource_id, section, next_offset)
            page["cursor"] = cursor
        if next_offset or omitted or any(item.get("truncated") for item in records):
            page["truncated"] = True
        return items, page

    def inspect(self, resource_id: str) -> dict:
        available = self.index.resource_sections(resource_id)
        items, sections = [], {}
        for section, (limit, byte_budget) in PREVIEW.items():
            selected, page = self.section_page(resource_id, section, available[section], None, limit, byte_budget)
            items.extend(selected)
            sections[section] = page
        related = self.related(resource_id)
        result = {"resourceId": resource_id, "resource": self.index.nodes[resource_id]["resource"],
                  "sections": sections, "items": items, "related": related[:4],
                  "moreRelationshipsInMap": len(related) > 4, "returnedCount": len(items),
                  "availability": "collected" if any(available.values()) else "not_collected"}
        return self.index._stage("inspect", result, Exposure(nodeIds={resource_id}, sourceIds={item["id"] for item in items}))

    def read_page(self, page: SectionPage) -> dict:
        records = self.index.resource_sections(page.resource_id)[page.section]
        items, details = self.section_page(page.resource_id, page.section, records, page.offset, 20)
        result = {"resourceId": page.resource_id, "sections": {page.section: details}, "items": items,
                  "returnedCount": len(items), "availability": "collected" if records else "not_collected"}
        return self.index._stage("read_more", result,
                                 Exposure(nodeIds={page.resource_id}, sourceIds={item["id"] for item in items}))

    async def read(self, key: str, stage: str, resource_id: str, action, tool_call_id: str, *, previous: bool = False) -> str:
        if not self.can_read() or self.calls >= self.budget.tool_calls:
            return encoded({"error": "Reads are closed. Continue the verification phase."})
        if self.reads_in_turn >= MAX_READS_PER_TURN:
            return encoded({"error": "Use at most three reads per turn."})
        self.reads_in_turn += 1

        def load():
            if key and key in self.receipts:
                self.repeated_reads += 1
                return {"status": "already_read", **self.unread_receipt(self.receipts[key]),
                        "nextAction": "Follow an unread section cursor or inspect a different resource."}
            result = action()
            self.repeated_reads = 0
            if any(item["id"] not in self.index.exposed.sourceIds for item in result.get("items", [])):
                self.repeated_writes = 0
            if key:
                self.receipts[key] = {field: result[field] for field in ("resourceId", "sections") if field in result}
            return result
        return await super().invoke(stage, load, tool_call_id, resource_id, previous)

    def cards(self, assessment: AssessmentV2) -> list[NodeInsight]:
        inspected = sorted(self.shown_nodes | {self.index.entry})
        nodes = {resource_key(self.index.nodes[identity]["resource"]): identity for identity in inspected}
        external_owners: dict[str, list[str]] = {}
        for identity in inspected:
            node = self.index.nodes[identity]
            if node.get("type") == "external":
                for source_id in node.get("sourceIds") or []:
                    external_owners.setdefault(source_id, []).append(identity)
        insights: dict[str, NodeInsight] = {}
        for claim in assessment.claims:
            sources: dict[str, list[str]] = {}
            for citation in claim.citations:
                node = nodes.get(resource_key(self.index.sources[citation.sourceId].get("resource") or {}))
                for owner in external_owners.get(citation.sourceId) or ([node] if node else []):
                    sources.setdefault(owner, []).append(citation.sourceId)
            for node, identities in sources.items():
                insight = insights.setdefault(node, NodeInsight(nodeId=node, relevance="relevant", comment=claim.statement[:800]))
                insight.claimIds.append(claim.id)
                insight.sourceIds = sorted(set(insight.sourceIds + identities))
        return list(insights.values())

    def prepare_draft(self, draft: Draft) -> None:
        draft.assessment.nodeInsights = self.cards(draft.assessment)
        draft.revision += 1
        byte_limit = MAX_DRAFT_BYTES if draft.has_conclusion else MAX_DRAFT_BYTES - CONCLUSION_RESERVE_BYTES
        if len(encoded(draft.view(self.index)).encode()) > byte_limit:
            raise ValueError("Draft is full. Shorten or retract an existing finding.")
        legacy.validate_agent_assessment(draft.assessment, self.index)

    async def publish_draft(self, draft: Draft, kind: str, identity: str) -> None:
        self.draft = draft
        await self.emit("draft_updated", "completed", draft=draft.view(self.index), change={"kind": kind, "id": identity})

    async def reject_write(self, error: str) -> dict:
        await self.emit("validating", "failed", reasonCode="draft_change_deferred")
        return {"accepted": False, "error": error}

    async def verify(self, requests: int, summary: str = "", gaps: list[str] | None = None,
                     remediation: list[str] | None = None) -> dict:
        if self.verified is not None:
            return {"verified": True, "revision": self.verified.draft.revision}
        await self.emit("validating", "started", reasonCode="verify_requested")
        remaining_targets = self.review_targets - self.shown_nodes
        if self.review_until is not None and remaining_targets and requests < self.review_until and self.can_record() and self.calls < self.budget.tool_calls:
            return {"verified": False, "inspectResources": sorted(remaining_targets),
                    "nextAction": "Inspect the listed resources before verifying again."}
        targets = self.unread_review_targets() if self.can_read() and self.calls < self.budget.tool_calls else []
        deadline = min(requests + (4 if targets else 2), self.budget.requests - 2)
        if self.review_until is None and requests < deadline and self.writes < MAX_WRITES:
            checks = self.unrecorded_relationships()
            last_read_has_new_sources = any(exposure.sourceIds - self.index.exposed.sourceIds for _, exposure in self.index.pending)
            if checks or last_read_has_new_sources or targets:
                self.review_until = deadline
                self.review_targets = set(targets)
                self.repeated_writes = 0
                await self.emit("validating", "completed", reasonCode="verify_review_required")
                return {"verified": False, "review": checks, "inspectResources": targets,
                        "nextAction": "Inspect any listed resources and review the checks. Save any separate blocker they establish, then call verify again."}

        gaps = [gap.strip() for gap in gaps or [] if gap.strip()]
        remediation = [step.strip() for step in remediation or [] if step.strip()]
        if len(summary) > 1600 or len(gaps) > 16 or len(remediation) > 12 or any(len(item) > 800 for item in [*gaps, *remediation]):
            return await self.reject_write("Provide a short summary and bounded lists of gaps and actions.")
        if remaining_targets:
            self.wrap_up = True
        draft = deepcopy(self.draft)
        assessment = draft.assessment
        supported = any(claim.verdict == "supported" for claim in assessment.claims)
        if assessment.claims:
            default_summary = " ".join(claim.statement for claim in assessment.claims)
            if len(encoded(default_summary).encode()) > CONCLUSION_RESERVE_BYTES - 200:
                default_summary = "See the verified findings for the causes and supporting evidence."
            assessment.summary = summary.strip() or default_summary
        else:
            assessment.summary = "The snapshot does not establish a cause."
            gaps = gaps or ["The investigation did not establish a source-backed explanation for the reported impact."]
        assessment.confidence = 0.8 if supported and not gaps and not self.wrap_up else 0.55 if assessment.claims else 0.3
        assessment.missingEvidence = [MissingEvidence(description=gap) for gap in gaps]
        assessment.remediation = [RemediationStep(order=i, title=step) for i, step in enumerate(remediation, 1)] if assessment.claims else []
        draft.has_conclusion = True
        try:
            self.prepare_draft(draft)
            result, audit = legacy.project_agent_citations(assessment, self.index)
        except ValueError as exc:
            return await self.reject_write(str(exc))
        self.verified = VerifiedDraft(draft, result, audit)
        await self.emit("validating", "completed", reasonCode="draft_verified")
        return {"verified": True, "revision": draft.revision, "nextAction": "Call finish to save the verified assessment."}


def build_investigation_agent(model: Model, persona_enabled: bool = False, *, pipeline: bool = False) -> Agent[InvestigationSession, str]:
    async def read_prepare(ctx, definition):
        return definition if ctx.deps.can_read() else None

    async def page_prepare(ctx, definition):
        return definition if ctx.deps.review_until is None and ctx.deps.can_read() else None

    async def record_prepare(ctx, definition):
        return definition if ctx.deps.can_record() else None

    async def verify_prepare(ctx, definition):
        return definition if ctx.deps.verified is None else None

    async def finish_prepare(ctx, definition):
        return definition if ctx.deps.verified is not None else None

    async def inspect(ctx: RunContext[InvestigationSession], resource_id: str) -> str:
        """Inspect one resource from the snapshot map. Returns fields, events, log excerpts, relationships and section cursors."""
        session = ctx.deps
        if session.review_until is not None and resource_id not in session.review_targets - session.shown_nodes:
            return encoded({"error": "Only unread verification targets can be inspected during review."})
        return await session.read("inspect:" + resource_id, "get_resource", resource_id,
                                  lambda: session.inspect(resource_id), ctx.tool_call_id or "")

    async def read_more(ctx: RunContext[InvestigationSession], cursor: str) -> str:
        """Read one returned section cursor, or reread a previously delivered source ID."""
        session = ctx.deps
        if session.review_until is not None:
            return encoded({"error": "Use inspect on the listed verification targets during review."})
        if page := session.cursors.get(cursor):
            return await session.read(cursor, STAGES[page.section], page.resource_id, lambda: session.read_page(page),
                                      ctx.tool_call_id or "", previous=page.section == "previousLogs")
        if cursor in session.index.exposed.sourceIds:
            session.index.allowed_sources.add(cursor)
            return await session.read("", "read_sources", "", lambda: session.index.read_sources([cursor]),
                                      ctx.tool_call_id or "")
        return encoded({"error": "Use a section cursor or source ID returned by an earlier read."})

    async def record_finding(ctx: RunContext[InvestigationSession], statement: str = "", source_ids: list[str] | None = None,
                             finding_id: str = "", verdict: Verdict = "supported") -> dict:
        """Save one finding with exact source IDs. Reuse its finding_id to revise it, or provide only the finding_id to retract it."""
        session = ctx.deps
        if not session.can_record():
            return {"accepted": False, "error": "Finding writes are closed. Use the available verification or finish tool."}
        session.writes += 1
        draft = deepcopy(session.draft)
        assessment = draft.assessment
        statement = statement.strip()
        identity = finding_id or ("finding:" + digest(statement)[:16] if statement else "")
        prior = {claim.id: claim for claim in [*assessment.claims, *assessment.alternativeHypotheses]}
        if not identity or len(identity) > 512 or len(statement) > 1600 or (not statement and identity not in prior):
            return await session.reject_write("Give a finding of 1–1600 characters, or the ID of an existing finding to retract.")
        sources = list(dict.fromkeys(source_ids or []))
        if len(sources) > 12 or not set(sources) <= session.index.exposed.sourceIds or (statement and verdict != "unsupported" and not sources):
            return await session.reject_write("Use up to 12 exact source IDs from returned records. Supported findings require evidence.")
        already_cited = {citation.sourceId for claim in prior.values() for citation in claim.citations}
        session.repeated_writes = 0 if set(sources) - already_cited or not statement else session.repeated_writes + 1
        claim = AssessmentClaim(id=identity, statement=statement, verdict=verdict,
                                citations=[AssessmentCitation(sourceId=source_id) for source_id in sources])
        if claim == prior.get(identity):
            return {"accepted": True, "findingId": identity, "revision": draft.revision, "nextAction": "This finding is already saved."}
        assessment.claims = [claim for claim in assessment.claims if claim.id != identity]
        assessment.alternativeHypotheses = [claim for claim in assessment.alternativeHypotheses if claim.id != identity]
        kind = "retracted"
        if statement:
            kind = "hypothesis" if verdict == "unsupported" else "claim"
            (assessment.alternativeHypotheses if kind == "hypothesis" else assessment.claims).append(claim)
        if len(assessment.claims) > 24 or len(assessment.alternativeHypotheses) > 12:
            return await session.reject_write("Finding limit reached. Revise or retract an existing finding.")
        try:
            session.prepare_draft(draft)
        except ValueError as exc:
            return await session.reject_write(str(exc))
        await session.publish_draft(draft, kind, identity)
        return {"accepted": True, "findingId": identity, "revision": draft.revision}

    async def verify(ctx: RunContext[InvestigationSession], summary: str = "", gaps: list[str] | None = None,
                     remediation: list[str] | None = None) -> dict:
        """Verify findings and optional final text.

        For each gap, give the open diagnostic question, the evidence needed, and
        how it could change the diagnosis. Omit gaps when no such question remains.
        Use remediation for prerequisites to a fix. A one-time review may follow.
        """
        return await ctx.deps.verify(ctx.usage.requests, summary, gaps, remediation)

    async def finish(ctx: RunContext[InvestigationSession]) -> dict:
        """Save the verified assessment exactly as prepared."""
        session = ctx.deps
        if session.finalized is not None:
            return {"finalized": True}
        verified = session.verified
        if verified is None:
            return {"finalized": False, "error": "Call verify before finish."}
        session.writes += 1
        await session.emit("finalizing", "started", reasonCode="finish_requested")
        await session.publish_draft(verified.draft, "conclusion", "conclusion")
        session.finalized, session.citation_quotes = verified.assessment, verified.citation_quotes
        await session.emit("finalizing", "completed", reasonCode="finish_completed")
        return {"finalized": True}

    agent = Agent(model, deps_type=InvestigationSession, output_type=str,
                  name="data_pipeline_investigator" if pipeline else "kubernetes_investigator",
                  instructions=build_agent_instructions(persona_enabled, incremental=True, pipeline=pipeline), retries=2, tool_timeout=5,
                  capabilities=[InvestigationDiagnostics()],
                  tools=[Tool(inspect, prepare=read_prepare, sequential=True), Tool(read_more, prepare=page_prepare, sequential=True),
                         Tool(record_finding, prepare=record_prepare, sequential=True),
                         Tool(verify, prepare=verify_prepare, sequential=True), Tool(finish, prepare=finish_prepare, sequential=True)])

    @agent.instructions
    async def current(ctx: RunContext[InvestigationSession]) -> str:
        if ctx.deps.verified is not None:
            return "The assessment is verified. Call finish() now."
        if ctx.deps.review_until is not None:
            targets = sorted(ctx.deps.review_targets - ctx.deps.shown_nodes)
            if targets:
                return "Inspect the remaining verification targets: " + encoded(targets)
            return "Review the delivered evidence. Save any additional supported finding, then call verify."
        if ctx.deps.wrap_up:
            return "Reads are closed. Call verify with the findings and any evidence gaps."
        if not ctx.usage.requests:
            return "Use inspect on rootNodeId to begin."
        if ctx.deps.repeated_reads:
            return "The last read repeated a checked page. Use an unchecked related resource or an unused section cursor."
        return f"Model requests remaining, including this one: {max(0, ctx.deps.budget.requests - ctx.usage.requests)}."

    @agent.output_validator
    async def require_finish(ctx: RunContext[InvestigationSession], _text: str) -> str:
        tool = "finish()" if ctx.deps.verified is not None else "verify"
        raise ModelRetry(f"Call {tool} to continue.")

    return agent


async def run_incremental_agent(req: AnalyzeRequest, sink: legacy.ProgressSink, *, model: Model | None = None, budget: legacy.AgentBudget | None = None) -> AnalyzeResponse:
    budget = budget or legacy.AgentBudget()
    await sink({"stage": "indexing", "phase": "started"})
    index = SnapshotIndex(req.payload)
    prompt = build_agent_prompt(req.payload, index.bootstrap(full_map=True), req.userMessage)
    session = InvestigationSession(index, req, budget, sink, startup_prompt=prompt)
    await session.emit("indexing", "completed", snapshotHash=index.snapshot_hash, draft=session.draft.view(index),
                       investigationGraph=session.graph_scope())
    model = model or legacy.make_agent_model(req)
    agent = build_investigation_agent(model, req.personaEnabled, pipeline=index.pipeline)
    limits = UsageLimits(request_limit=budget.requests, tool_calls_limit=budget.tool_calls+MAX_WRITES+3,
                         input_tokens_limit=budget.input_tokens, output_tokens_limit=budget.output_tokens)
    settings: legacy.BedrockModelSettings = {"max_tokens": session.max_output_tokens}
    if req.credentials.get("temperature") not in (None, ""):
        settings["temperature"] = float(req.credentials["temperature"])
    async with asyncio.timeout(budget.seconds):
        async with agent.iter(prompt, deps=session, usage_limits=limits, model_settings=settings) as run:
            node = run.next_node
            while not Agent.is_end_node(node) and session.finalized is None:
                is_request = Agent.is_model_request_node(node)
                if is_request and run.usage.requests < budget.requests:
                    await session.advance_phase(run.usage)
                    await session.emit("model_request", turn=run.usage.requests+1)
                old_input, old_output = run.usage.input_tokens, run.usage.output_tokens
                try:
                    node = await run.next(node)
                except legacy.UsageLimitExceeded as exc:
                    for marker, kind, used, maximum in (("request_limit", "requests", run.usage.requests, budget.requests),
                        ("input_tokens_limit", "input_tokens", run.usage.input_tokens, budget.input_tokens),
                        ("output_tokens_limit", "output_tokens", run.usage.output_tokens, budget.output_tokens),
                        ("tool_calls_limit", "tool_calls", run.usage.tool_calls, budget.tool_calls+MAX_WRITES+3)):
                        if marker in str(exc):
                            await session.emit("execution_limit", "failed", executionLimit={"kind": kind, "used": used, "maximum": maximum})
                            break
                    raise
                if is_request:
                    session.reads_in_turn = 0
                    session.last_input_tokens = run.usage.input_tokens-old_input
                    session.last_output_tokens = run.usage.output_tokens-old_output
                    index.acknowledge_delivery()
                    await session.emit("context_delivered", "completed", exposed=index.exposed.as_dict(),
                                       turn=run.usage.requests, turnTokens={"input": session.last_input_tokens,
                                                                          "output": session.last_output_tokens})
            usage = run.usage
    if session.finalized is None:
        raise ValueError("investigation did not finalize its draft")
    assessment = session.finalized
    legacy.validate_agent_assessment(assessment, index)
    return AnalyzeResponse(evidenceId=req.evidenceId, model=f"bedrock/{model.model_name}", likelyRootCause=assessment.summary,
        evidenceChain="\n".join(c.statement for c in assessment.claims), recommendation="\n".join(s.title for s in assessment.remediation),
        confidence=assessment.confidence, needsMoreInfo=bool(assessment.missingEvidence), assessmentVersion=2, assessment=assessment,
        citationValidation=CitationValidation(total=sum(len(x.citations) for x in [*assessment.claims, *assessment.alternativeHypotheses, *assessment.remediation])),
        analysisMode="snapshot-agent-v1", prompt=build_agent_instructions(req.personaEnabled, incremental=True, pipeline=index.pipeline)+"\n"+prompt,
        tokenUsage=TokenUsage(inputTokens=usage.input_tokens, outputTokens=usage.output_tokens, totalTokens=usage.input_tokens+usage.output_tokens, inputSource="provider", outputSource="provider"),
        agentExecution={"version": 2, "assessmentBuilder": "incremental-v1", "analysisRequestId": req.analysisRequestId, "attemptId": req.attemptId,
            "investigator": "data-pipeline-v1" if index.pipeline else "kubernetes-v1",
            "historyPolicy": "snapshot-reads-v2", "toolProtocol": "snapshot-verify-v1",
            "snapshotHash": index.snapshot_hash, "status": "partial" if session.wrap_up else "completed", "stopReason": "budget_or_no_new_facts" if session.wrap_up else "final_answer",
            "requests": usage.requests, "toolCalls": usage.tool_calls, "readToolCalls": session.calls, "writeToolCalls": session.writes,
            "contextBytes": index.context_bytes, "contextJournal": index.journal, "exposed": index.exposed.as_dict(),
            "draft": session.draft.view(index), "investigationGraph": session.graph_scope(),
            "citationQuotes": session.citation_quotes, "validationIssues": session.output_rejections})
