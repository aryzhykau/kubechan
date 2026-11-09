"""Bounded, deterministic reads over one frozen Kubernetes evidence snapshot.

No Kubernetes clients, raw-resource fallbacks, network access, or model-generated
topology belong here. Merely revealing a source ID does not reveal its value.
"""
from __future__ import annotations

import base64
from collections import defaultdict
from copy import deepcopy
from dataclasses import dataclass, field
import hashlib
import json
from typing import Any, Literal


def encoded(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False)


def digest(value: Any) -> str:
    return hashlib.sha256(encoded(value).encode()).hexdigest()


def resource_key(resource: dict) -> tuple[str, str, str, str]:
    return tuple(str(resource.get(key) or "") for key in ("apiGroup", "kind", "namespace", "name"))  # type: ignore[return-value]


@dataclass
class Exposure:
    nodeIds: set[str] = field(default_factory=set)
    edgeIds: set[str] = field(default_factory=set)
    candidateIds: set[str] = field(default_factory=set)
    sourceIds: set[str] = field(default_factory=set)

    def merge(self, other: Exposure) -> None:
        for name in ("nodeIds", "edgeIds", "candidateIds", "sourceIds"):
            getattr(self, name).update(getattr(other, name))

    def as_dict(self) -> dict[str, list[str]]:
        return {name: sorted(getattr(self, name)) for name in ("nodeIds", "edgeIds", "candidateIds", "sourceIds")}


class SnapshotIndex:
    max_result_bytes = 12_000
    max_context_bytes = 128_000

    def __init__(self, payload: dict[str, Any]):
        self.payload = deepcopy(payload)
        self.snapshot_hash = digest(self.payload)
        graph = self.payload.get("graph") or {}
        if not graph or graph.get("schemaVersion") not in (2, 3):
            raise ValueError("snapshot-agent-v1 requires a versioned investigation graph")
        self.nodes = self._catalog(graph.get("nodes", []))
        self.edges = self._catalog(graph.get("edges", []))
        self.candidates = self._catalog(graph.get("candidates", []))
        self.pipeline = any(node.get("type") == "external" or node.get("resource", {}).get("kind") == "DataSource" for node in self.nodes.values())
        self.sources = self._catalog(self.payload.get("sources", []))
        entry = graph.get("entryResourceId")
        if not isinstance(entry, str) or entry not in self.nodes or not self.sources:
            raise ValueError("snapshot entry and atomic source catalog are required")
        self.entry: str = entry
        for edge in self.edges.values():
            if edge.get("from") not in self.nodes or edge.get("to") not in self.nodes:
                raise ValueError("snapshot graph contains an unresolved edge ID")
        self.by_resource: dict[tuple, list[dict]] = {}
        for source in self.sources.values():
            ref = source.get("resource") or {}
            # Defense in depth: secret values are never an agent-readable field.
            if ref.get("kind") == "Secret" and source.get("path") != "/exists":
                continue
            self.by_resource.setdefault(resource_key(ref), []).append(source)
        self.allowed_nodes: set[str] = set()
        self.allowed_sources: set[str] = set()
        self.exposed = Exposure()
        self.pending: list[tuple[dict, Exposure]] = []
        self.journal: list[dict] = []
        self.context_bytes = 0
        self.no_progress = 0
        self.topology_mapped = False

    @staticmethod
    def _catalog(items: list[dict]) -> dict[str, dict]:
        result = {}
        for item in items:
            identity = item.get("id")
            if not isinstance(identity, str) or not identity or identity in result:
                raise ValueError("snapshot IDs must be unique nonempty strings")
            result[identity] = item
        return dict(sorted(result.items()))

    def _node(self, identity: str) -> dict:
        node = self.nodes[identity]
        self.allowed_nodes.add(identity)
        view = self._node_view(node)
        self.allowed_sources.update(view.get("sourceIds") or [])
        return view

    @staticmethod
    def _node_view(node: dict) -> dict:
        view = {key: deepcopy(node[key]) for key in ("id", "type", "resource", "state", "role") if key in node}
        refs = sorted(set(node.get("sourceIds") or []))
        view.update(sourceIds=refs[:16], sourceRefsTruncated=len(refs) > 16)
        return view

    def _authorize(self, identity: str) -> None:
        if identity not in self.allowed_nodes:
            raise ValueError("request a start node or reveal its graph relationship first")

    def _stage(self, tool: str, result: dict, exposure: Exposure, *, byte_limit: int | None = None) -> dict:
        size = len(encoded(result).encode())
        if size > (byte_limit if byte_limit is not None else self.max_result_bytes) or self.context_bytes + size > self.max_context_bytes:
            raise ValueError("snapshot context budget exhausted; finalize using delivered evidence")
        self.context_bytes += size
        known = Exposure()
        known.merge(self.exposed)
        for _, pending in self.pending:
            known.merge(pending)
        before = known.as_dict()
        known.merge(exposure)
        self.no_progress = 0 if before != known.as_dict() else self.no_progress + 1
        self.pending.append(({
            "tool": tool, "resultHash": digest(result), "bytes": size,
            **exposure.as_dict(),
        }, exposure))
        return result

    def acknowledge_delivery(self) -> None:
        """Commit only after the provider acknowledges a request containing these results."""
        for record, exposure in self.pending:
            self.exposed.merge(exposure)
            self.journal.append({"seq": len(self.journal) + 1, **record})
        self.pending.clear()

    def bootstrap(self, *, full_map: bool = False) -> dict:
        seeds = [self.entry]
        requested = [pc.get("affectedResource", {}) for pc in self.payload.get("problemCases", [])]
        # Retain access to disconnected explicitly collected context without
        # copying every graph node or classifying it as relevant.
        for identity, node in self.nodes.items():
            if node.get("role") == "related" or any(resource_key(node.get("resource", {})) == resource_key(ref) for ref in requested):
                seeds.append(identity)
        seeds = list(dict.fromkeys(seeds))
        omitted_seeds = max(0, len(seeds) - 8)
        seeds = seeds[:8]
        result = {
            "snapshotHash": self.snapshot_hash,
            "collectedAt": self.payload.get("collectedAt"),
            "entryResourceId": self.entry,
            "startNodes": [self._node(identity) for identity in seeds] if not full_map else [],
            "counts": {"nodes": len(self.nodes), "edges": len(self.edges), "sources": len(self.sources)},
            "graphTruncated": bool(self.payload["graph"].get("truncated")),
            "policy": self.payload["graph"].get("policy", {}),
            "signals": [],
            "signalsTruncated": False,
            "omittedStartNodes": omitted_seeds,
            "completeness": {
                "collectionErrorSourceIds": [s["id"] for s in self.sources.values() if s.get("type") == "collection_error"][:16],
                "collectionErrorCount": sum(s.get("type") == "collection_error" for s in self.sources.values()),
                "truncatedSourceRecords": sum(bool(s.get("truncated")) for s in self.sources.values()),
                "scope": "bounded snapshot; uncollected data is unknown",
            },
        }
        self.allowed_sources.update(result["completeness"]["collectionErrorSourceIds"])
        exposure = Exposure(nodeIds=set(seeds))
        # Only short, addressable detector signals are initially revealed.
        signals = [s for s in self.sources.values() if str(s.get("path", "")).startswith("/problemCases/")]
        for source in signals:
            if len(result["signals"]) >= 8 or len(encoded(source).encode()) > 800:
                result["signalsTruncated"] = True
                continue
            result["signals"].append(deepcopy(source))
            exposure.sourceIds.add(source["id"])
            self.allowed_sources.add(source["id"])
        if not full_map:
            return self._stage("bootstrap", result, exposure)

        # Graph metadata and evidence delivery are separate. Nothing in these
        # rows grants citation access to a field, event, or log value.
        candidates = [
            c for c in self.candidates.values()
            if all(c.get(key) in self.nodes for key in ("sourceNodeId", "referenceNodeId", "candidateNodeId"))
        ]
        result["graph"] = {
            "nodes": [
                {**{key: node[key] for key in ("id", "type", "state") if key in node},
                 **{key: node["resource"][key] for key in ("kind", "name", "namespace", "apiGroup") if key in node["resource"]},
                 **({"scope": node["external"]["scope"]} if (node.get("external") or {}).get("scope") else {})}
                for node in self.nodes.values()
            ],
            "edges": [{key: edge[key] for key in ("id", "from", "to", "type", "state", "origin", "basis") if key in edge}
                      for edge in self.edges.values()],
            "candidates": [{key: c[key] for key in ("id", "sourceNodeId", "referenceNodeId", "candidateNodeId", "strength") if key in c}
                           for c in candidates],
            "truncated": result.pop("graphTruncated"),
        }
        result.pop("startNodes")
        result.pop("omittedStartNodes")
        result["rootNodeId"] = result.pop("entryResourceId")
        exposure.nodeIds = set(self.nodes)
        exposure.edgeIds = set(self.edges)
        exposure.candidateIds = {c["id"] for c in candidates}
        staged = self._stage("bootstrap", result, exposure, byte_limit=self.max_context_bytes)
        self.allowed_nodes.update(self.nodes)
        self.topology_mapped = True
        return staged

    def _page(self, tool: str, query: dict, items: list[dict], cursor: str | None, limit: int,
              *, byte_budget: int | None = None) -> tuple[list[dict], str | None, list[str]]:
        if not 1 <= limit <= 25:
            raise ValueError("limit must be between 1 and 25")
        fingerprint = digest([self.snapshot_hash, tool, query])
        offset = 0
        if cursor:
            try:
                token = json.loads(base64.urlsafe_b64decode(cursor))
                if token["query"] != fingerprint or type(token["offset"]) is not int or not 0 <= token["offset"] <= len(items):
                    raise ValueError
                offset = token["offset"]
            except Exception as exc:
                raise ValueError("cursor does not match this snapshot and query") from exc
        selected, omitted = [], []
        size = 0
        while offset < len(items) and len(selected) < limit:
            item = items[offset]
            item_size = len(encoded(item).encode())
            if item_size > self.max_result_bytes // 2:
                omitted.append(str(item.get("id", "")))
                offset += 1
                # Bound the omission list too.
                if len(omitted) >= limit:
                    break
                continue
            if size + item_size > (byte_budget if byte_budget is not None else self.max_result_bytes - 3000):
                break
            selected.append(deepcopy(item))
            size += item_size
            offset += 1
        next_cursor = base64.urlsafe_b64encode(encoded({"query": fingerprint, "offset": offset}).encode()).decode() if offset < len(items) else None
        return selected, next_cursor, omitted

    def resource_sections(self, node_id: str) -> dict[str, list[dict]]:
        self._authorize(node_id)
        sections: dict[str, list[dict]] = {key: [] for key in ("fields", "events", "currentLogs", "previousLogs", "collectionErrors")}
        field_groups: dict[str, list[dict]] = defaultdict(list)
        kind = self.nodes[node_id].get("resource", {}).get("kind")
        for source in self._resource_sources(node_id):
            path = str(source.get("path") or "")
            match source.get("type"):
                case "resource_field":
                    parts = path.lstrip("/").split("/", 2)
                    group = "/".join(parts[:2]) if kind == "DataSource" and parts[0] == "externalSnapshot" else parts[0]
                    field_groups[group].append(source)
                case "event":
                    sections["events"].append(source)
                case "log_line":
                    sections["previousLogs" if "/previous/" in path else "currentLogs"].append(source)
                case "collection_error":
                    sections["collectionErrors"].append(source)
        # Give each field group room on the first page, even for large resources.
        groups = [iter(sorted(fields, key=lambda source: str(source.get("path") or ""))) for _, fields in sorted(field_groups.items())]
        while groups:
            remaining = []
            for group in groups:
                if (source := next(group, None)) is not None:
                    sections["fields"].append(source)
                    remaining.append(group)
            groups = remaining
        if self.nodes[node_id].get("type") == "external":
            # In normalized observations the protocol path and scalar are separate sources.
            # Put the pairs together before listing structural fields and repeated subject metadata.
            sections["fields"].sort(key=lambda source: not (
                source.get("path", "").startswith("/externalSnapshot/observations/")
                and source.get("path", "").endswith(("/path", "/value"))))
        sections["events"].sort(key=lambda source: str((source.get("event") or {}).get("lastTime") or ""), reverse=True)
        for section in ("currentLogs", "previousLogs"):
            sections[section].sort(key=lambda source: (source.get("line") or {}).get("number") or 0, reverse=True)
        return sections

    def neighbors(self, node_id: str, direction: Literal["both", "in", "out"] = "both", relation_types: list[str] | None = None, cursor: str | None = None, limit: int = 10) -> dict:
        self._authorize(node_id)
        if direction not in ("both", "in", "out"):
            raise ValueError("invalid direction")
        types = sorted(set(relation_types or []))
        items = []
        for edge in self.edges.values():
            if types and edge.get("type") not in types:
                continue
            if (direction != "in" and edge.get("from") == node_id) or (direction != "out" and edge.get("to") == node_id):
                ids = {edge["from"], edge["to"]}
                items.append({"id": edge["id"], "edge": deepcopy(edge), "nodes": [self.nodes[n] for n in sorted(ids)]})
        for candidate in self.candidates.values():
            if node_id not in (candidate.get("sourceNodeId"), candidate.get("referenceNodeId")) or types:
                continue
            ids = {str(candidate.get(key, "")) for key in ("sourceNodeId", "referenceNodeId", "candidateNodeId")}
            if not ids <= self.nodes.keys():
                continue
            items.append({"id": candidate["id"], "candidate": deepcopy(candidate), "nodes": [self.nodes[n] for n in sorted(ids)]})
        # Node attributes are omitted: their values must be read as sources.
        for item in items:
            item["nodes"] = [self._node_view(node) for node in item["nodes"]]
            if "edge" in item:
                item["edge"].pop("attributes", None)
        page, next_cursor, omitted = self._page("get_graph_neighbors", {"nodeId": node_id, "direction": direction, "types": types}, items, cursor, limit)
        exposure = Exposure(nodeIds={node_id})
        for item in page:
            for node in item["nodes"]:
                self._node(node["id"])
                exposure.nodeIds.add(node["id"])
            if "edge" in item:
                exposure.edgeIds.add(item["id"])
                self.allowed_sources.update(item["edge"].get("sourceIds") or [])
                for reason in item["edge"].get("reasons") or []:
                    self.allowed_sources.update(reason.get("sourceIds") or [])
            if "candidate" in item:
                exposure.candidateIds.add(item["id"])
                for reason in item["candidate"].get("reasons") or []:
                    self.allowed_sources.update(reason.get("sourceIds") or [])
        return self._stage("get_graph_neighbors", {"snapshotHash": self.snapshot_hash, "items": page, "nextCursor": next_cursor, "returnedCount": len(page), "matchingCount": len(items), "omittedSourceIds": omitted, "graphTruncated": bool(self.payload["graph"].get("truncated"))}, exposure)

    def _resource_sources(self, node_id: str) -> list[dict]:
        node = self.nodes[node_id]
        if node.get("type") == "external":
            # The graph attaches exact DataSource-owned records to scoped entities.
            # Names alone can collide across connectors, databases and brokers.
            return [self.sources[identity] for identity in sorted(set(node.get("sourceIds") or []))
                    if identity in self.sources and self.sources[identity].get("resource", {}).get("kind") == "DataSource"]
        sources = {s["id"]: s for s in self.by_resource.get(resource_key(node.get("resource", {})), [])}
        if node.get("type") == "reference":
            # Missing resources have no object to describe. Their existence
            # observations live on the referring resource, with original provenance.
            refs = set(node.get("sourceIds") or [])
            known_edges = set(self.exposed.edgeIds)
            for _, exposure in self.pending:
                known_edges.update(exposure.edgeIds)
            for identity in known_edges:
                edge = self.edges[identity]
                if edge.get("origin") == "observed" and node_id in (edge.get("from"), edge.get("to")):
                    refs.update(edge.get("sourceIds") or [])
            for identity in refs if self.topology_mapped else refs & self.allowed_sources:
                source = self.sources.get(identity)
                if source and (source.get("resource", {}).get("kind") != "Secret" or source.get("path") == "/exists"):
                    sources[identity] = source
        return [sources[identity] for identity in sorted(sources)]

    def observations(self, node_id: str, source_type: str, path_prefix: str = "", contains: str = "", previous: bool = False,
                     cursor: str | None = None, limit: int = 10) -> dict:
        self._authorize(node_id)
        sources = self._resource_sources(node_id)
        available = [s for s in sources if s.get("type") == source_type]
        if source_type == "log_line":
            available = [s for s in available if ("/previous/" in s.get("path", "")) == previous]
        matches = [s for s in available if str(s.get("path", "")).startswith(path_prefix) and (not contains or contains.casefold() in encoded(s).casefold())]
        tool = {"resource_field": "get_resource", "event": "get_events", "log_line": "get_logs"}[source_type]
        query = {"nodeId": node_id, "type": source_type, "path": path_prefix, "contains": contains, "previous": previous}
        page, next_cursor, omitted = self._page(tool, query, matches, cursor, limit)
        self.allowed_sources.update(s["id"] for s in page)
        self.allowed_sources.update(s["id"] for s in sources if s.get("type") == "collection_error")
        result = {
            "snapshotHash": self.snapshot_hash, "nodeId": node_id, "items": page, "nextCursor": next_cursor,
            "returnedCount": len(page), "matchingCount": len(matches),
            "availability": "collected" if available else "not_collected", "omittedSourceIds": omitted,
            "collectionErrors": [s["id"] for s in sources if s.get("type") == "collection_error"],
            "truncated": bool(next_cursor or omitted or any(s.get("truncated") for s in available)),
        }
        return self._stage(tool, result, Exposure(nodeIds={node_id}, sourceIds={s["id"] for s in page}))

    def read_sources(self, source_ids: list[str]) -> dict:
        if not 1 <= len(source_ids) <= 10 or any(identity not in self.allowed_sources or identity not in self.sources for identity in source_ids):
            raise ValueError("read 1..10 source IDs already revealed by snapshot tools")
        items = []
        for identity in dict.fromkeys(source_ids):
            source = self.sources[identity]
            if source.get("resource", {}).get("kind") == "Secret" and source.get("path") != "/exists":
                raise ValueError("secret contents are unavailable")
            items.append(source)
        page, _, omitted = self._page("read_sources", {"ids": source_ids}, items, None, 10)
        return self._stage("read_sources", {"snapshotHash": self.snapshot_hash, "items": page, "omittedSourceIds": omitted, "returnedCount": len(page)}, Exposure(sourceIds={s["id"] for s in page}))
