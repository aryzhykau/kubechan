"""Bounded investigation leads backed by existing snapshot relationships."""
from collections import defaultdict, deque
from dataclasses import dataclass

from app.snapshot import SnapshotIndex, encoded, resource_key

MAX_CHECKS = 4


@dataclass(frozen=True)
class RelationshipCheck:
    edge_id: str
    node_id: str
    target_id: str
    question: str
    source_ids: tuple[str, ...]
    comparison: bool

    def view(self) -> dict:
        return {"edgeId": self.edge_id, "nodeId": self.node_id, "targetId": self.target_id,
                "question": self.question, "evidenceSourceIds": list(self.source_ids)}


def graph_distances(index: SnapshotIndex, roots: list[str]) -> dict[str, int]:
    adjacent = defaultdict(list)
    for edge in index.edges.values():
        adjacent[edge["from"]].append(edge["to"])
        adjacent[edge["to"]].append(edge["from"])
    distance = dict.fromkeys(roots, 0)
    pending = deque(distance)
    while pending:
        node = pending.popleft()
        for neighbor in adjacent[node]:
            if neighbor not in distance:
                distance[neighbor] = distance[node] + 1
                pending.append(neighbor)
    return distance


def runtime_followup(index: SnapshotIndex) -> tuple[str, str] | None:
    """Reserve one independent runtime check when both warnings and logs were captured."""
    related = [identity for identity, node in index.nodes.items() if node.get("role") == "related"]
    distances = graph_distances(index, [index.entry, *related])
    for identity in sorted(distances, key=lambda node: (distances[node], node)):
        ref = index.nodes[identity]["resource"]
        records = index.by_resource.get(resource_key(ref), [])
        has_warning = any((item.get("event") or {}).get("type") == "Warning" for item in records)
        if has_warning and any(item.get("type") == "log_line" for item in records):
            question = f"What do the captured warning events and logs for {ref['kind']}/{ref['name']} establish about recovery?"
            return identity, question[:250]
    return None


def relationship_checks(index: SnapshotIndex) -> dict[str, RelationshipCheck]:
    """Index leads in the root component. A graph state is a lead, not a verdict."""
    distance = graph_distances(index, [index.entry])

    def label(node_id: str) -> str:
        ref = index.nodes[node_id]["resource"]
        return f"{ref['kind']}/{ref['name']}"

    readable = {item["id"] for records in index.by_resource.values() for item in records}
    checks, missing = {}, set()
    reference_sources = defaultdict(list)
    edges = [edge for edge in index.edges.values() if edge["from"] in distance and (
        (edge.get("origin"), edge.get("state")) in {("observed", "broken"), ("inferred", "proposed")}
    )]
    edges.sort(key=lambda edge: (min(distance[edge["from"]], distance[edge["to"]]), edge["id"]))
    for edge in edges:
        if edge["origin"] == "observed":
            reference_sources[(edge.get("type"), edge["to"])].extend(edge.get("sourceIds") or [])
    for edge in edges:
        node, target = edge["from"], edge["to"]
        relation = edge.get("type", "dependency").replace("_", " ")
        is_missing = edge["origin"] == "observed" and index.nodes[target].get("state") == "missing"
        key = (edge.get("type"), target)
        if is_missing and key in missing:
            continue
        comparison = edge["origin"] == "inferred"
        # Keep exact relationship records. Repeated references can contribute an
        # explicit absence field without opening a separate branch per replica.
        refs = list(edge.get("sourceIds") or [])
        if is_missing:
            refs += index.nodes[target].get("sourceIds") or []
            refs += reference_sources[key]
        refs = [ref for ref in dict.fromkeys(refs) if ref in readable and len(encoded(index.sources[ref]).encode()) <= 600]
        if comparison and len(refs) != len(set(edge.get("sourceIds") or [])):
            continue
        refs.sort(key=lambda ref: not str(index.sources[ref].get("path") or "").lower().endswith("exists"))
        if not refs or comparison and len(refs) > 6:
            continue
        if is_missing:
            missing.add(key)
        question = f"Does the {relation} relationship from {label(node)} to {label(target)} need to change for recovery?"
        checks[edge["id"]] = RelationshipCheck(edge["id"], node, target, question[:250], tuple(refs[:6]), comparison)
    return checks
