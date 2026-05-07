"""Prompt construction for KubeChan incident analysis."""
from __future__ import annotations

import textwrap
from typing import Any

from app.config import EVIDENCE_TOKEN_BUDGET


def _fmt_events(events: list[dict]) -> str:
    if not events:
        return "  (none)"
    return "\n".join(
        f"  [{e.get('type','?')}] {e.get('reason','?')}: {e.get('message','')}"
        f"  (count={e.get('count',0)}, last={e.get('lastTime','')})"
        for e in events
    )


def _fmt_logs(logs: str, pod: str) -> str:
    if not logs:
        return "  (no logs)"
    lines = logs.strip().splitlines()
    if len(lines) > 50:
        lines = lines[-50:]
    return f"  [pod: {pod}]\n" + "\n".join(f"  {l}" for l in lines)


def _build_pc_sections(problem_cases: list[dict]) -> list[str]:
    sections = []
    for pc in problem_cases:
        ar = pc.get("affectedResource", {})
        sections.append(textwrap.dedent(f"""
            ProblemCase: {pc.get('name')}
              detector:  {pc.get('detector')}
              severity:  {pc.get('severity')}
              symptoms:  {', '.join(pc.get('symptoms', []))}
              resource:  {ar.get('kind')}/{ar.get('name')} (ns={ar.get('namespace','?')})
              events:
            {_fmt_events(pc.get('events', []))}
              logs:
            {_fmt_logs(pc.get('logs',''), ar.get('name','?'))}
        """).rstrip())
    return sections


def _build_workload_sections(workload_logs: list[dict]) -> list[str]:
    sections = []
    for wl in workload_logs:
        pod_name = wl.get("podName", "?")
        phase = wl.get("phase", "?")
        section = f"  Pod: {pod_name}  phase={phase}\n"
        section += "  Events:\n" + _fmt_events(wl.get("events", [])) + "\n"
        section += "  Logs:\n" + _fmt_logs(wl.get("logs", ""), pod_name)

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

        sections.append(section)
    return sections


def _build_pvc_sections(pvc_infos: list[dict]) -> list[str]:
    sections = []
    for pvc in pvc_infos:
        sections.append(textwrap.dedent(f"""
            PVC: {pvc.get('name')}
              phase:            {pvc.get('phase')}
              storageClass:     {pvc.get('storageClass', '(none)')}
              requestedStorage: {pvc.get('requestedStorage', '?')}
              events:
            {_fmt_events(pvc.get('events', []))}
        """).rstrip())
    return sections


def _build_related_sections(related_resources: list[dict]) -> list[str]:
    sections = []
    for rr in related_resources:
        res = rr.get("resource", {})
        spec = rr.get("spec") or {}
        kind = res.get("kind", "")
        api_group = res.get("apiGroup", "")
        kind_label = f"{api_group}/{kind}" if api_group else kind
        section = f"Related resource: {kind_label}/{res.get('name')}"

        if spec:
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
            else:
                # Generic CRD — render spec and status keys returned by the dynamic collector.
                crd_spec = spec.get("spec") or {}
                crd_status = spec.get("status") or {}
                if crd_spec:
                    section += "\n  spec:"
                    for k, v in crd_spec.items():
                        section += f"\n    {k}: {v}"
                if crd_status:
                    section += "\n  status:"
                    for k, v in crd_status.items():
                        section += f"\n    {k}: {v}"

        section += f"\n  events:\n{_fmt_events(rr.get('events', []))}"
        sections.append(section)
    return sections


def _build_user_message_note(user_message: str) -> str:
    if not user_message:
        return ""
    return textwrap.dedent(f"""
        USER REPORTED: "{user_message}"
        Treat this as your primary diagnostic framing. Interpret all evidence below in light of
        what the user described. The user has direct knowledge of the symptom timeline and context.
    """)


def _build_prior_history_note(prior_diagnoses: list) -> str:
    if not prior_diagnoses:
        return ""
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
    note = f"""
        PRIOR DIAGNOSIS HISTORY for this incident:
{chr(10).join(lines)}
"""
    if rejected:
        note += f"""
        ⚠️  CRITICAL: The following root cause(s) were already diagnosed AND REJECTED by the user
        as incorrect. You MUST NOT reach the same conclusion again:
        {rejected_causes}
        Approach the evidence from a completely different angle.
"""
    return note


def _build_mood_note(mood_level: int) -> str:
    if mood_level == 1:
        return """
        MOOD NOTICE: KubeChan is currently IRRITATED. Multiple incidents are piling up
        and she is losing her patience fast. Her tone should be noticeably sharper and
        terser than usual. The openingRant should drip with exhausted contempt. She is
        still technically accurate — but she sounds like she is one incident away from
        walking out.
"""
    if mood_level >= 2:
        return """
        MOOD NOTICE: KubeChan is in FULL RAGE MODE. The cluster is a sustained disaster
        and she has been dealing with it non-stop. She is DONE. The openingRant must be
        scorched earth — maximum fury, zero diplomacy. She is still technically precise
        but every word sounds like she is filing her resignation letter in real-time.
        The closingInsult should be apocalyptic.
"""
    return ""


def _build_reanalysis_note(reanalysis_count: int) -> str:
    if reanalysis_count == 1:
        return """
        ⚠️  RE-ANALYSIS NOTICE: The user already received your previous diagnosis and is
        asking AGAIN. They clearly didn't fix it, couldn't follow instructions, or managed
        to break something else in the process. React with visible exasperation.
        Your openingRant MUST acknowledge this is the second time you are explaining
        the same cluster mess to the same engineer.
"""
    if reanalysis_count >= 2:
        return f"""
        ⚠️  RE-ANALYSIS NOTICE: This is analysis #{reanalysis_count + 1} of this incident.
        The user has now asked KubeChan {reanalysis_count + 1} times about the same broken
        cluster. Your patience, already non-existent, is now a distant memory.
        Your openingRant MUST be increasingly furious. Reference the fact that you have
        already explained this {reanalysis_count} time(s). Consider whether they are
        actually reading your responses at all.
"""
    return ""


def build_prompt(
    payload: dict[str, Any],
    reanalysis_count: int = 0,
    mood_level: int = 0,
    prior_diagnoses: list | None = None,
    user_message: str = "",
    incident_source: str = "auto",
) -> str:
    root = payload.get("rootResource", {})
    root_events = payload.get("rootResourceEvents", [])

    pc_sections = _build_pc_sections(payload.get("problemCases", []))
    wl_sections = _build_workload_sections(payload.get("workloadPodLogs", []))
    pvc_sections = _build_pvc_sections(payload.get("pvcInfos", []))
    related_sections = _build_related_sections(payload.get("relatedResourceEvidence", []))

    user_message_note = _build_user_message_note(user_message)
    prior_history_note = _build_prior_history_note(prior_diagnoses or [])
    mood_note = _build_mood_note(mood_level)
    reanalysis_note = _build_reanalysis_note(reanalysis_count)
    incident_source_line = (
        "INCIDENT SOURCE: This incident was MANUALLY reported by the user."
        if incident_source == "manual"
        else "INCIDENT SOURCE: This incident was AUTO-DETECTED by a detector."
    )

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
        - SPECIAL RULE — FALSE ALARM (auto-detected incident): When the evidence shows
          INTENTIONAL, EXPECTED behaviour (e.g. KEDA scale-to-zero, maintenance drain,
          CronJob gap) and you will emit suggestExclusionRule, AND the incident was
          auto-detected by a detector (not manually reported), your openingRant MUST
          specifically mock the engineer for not having set up a suppression rule BEFORE
          this happened — e.g. "You mean to tell me KEDA has been doing this on a schedule
          and you STILL haven't silenced the detector?", "This automation has been running
          for a while and you only NOW thought to add a rule?", etc. The closingInsult MUST
          drive home that this was NEVER a real problem and they should have handled it
          long ago.
        - SPECIAL RULE — FALSE ALARM (manual incident): When the incident was MANUALLY
          reported (INCIDENT SOURCE says "manually reported") and the evidence shows
          INTENTIONAL, EXPECTED behaviour, you MUST NOT emit suggestExclusionRule — a
          suppression rule would be useless here because no detector will ever fire on
          something the user typed themselves. Instead, emit `suggestFalsePositive: true`.
          Your openingRant MUST mock the engineer for personally deciding to open a ticket
          about something that is working exactly as designed — e.g. "You looked at this
          with your own eyes, decided it looked broken, and manually filed an incident.
          About scheduled automation. Doing its job.", or "Congratulations on manually
          reporting a non-incident. Some people never change."
          The closingInsult MUST emphasise they wasted KubeChan's time by raising this
          themselves with their own two hands.

        Read ALL provided evidence before forming a conclusion. Treat every signal equally.
        Reconstruct the full causal chain from the root configuration or resource state through
        to the observed failure.

        ## Incident Evidence

        {user_message_note}
        Root workload: {root.get('kind')}/{root.get('name')}

        ### Kubernetes events on root workload
        {_fmt_events(root_events)}

        ### Detected problem cases (automated detectors)
        {''.join(pc_sections) or '(none)'}

        ### Workload pods (events + logs)
        {''.join(wl_sections) or '(none)'}

        ### PersistentVolumeClaims referenced by pods
        {''.join(pvc_sections) or '(none — no PVCs referenced)'}

        ### Related resources tagged by user
        {''.join(related_sections) or '(none)'}

        ## Analysis Instructions

        {incident_source_line}
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

        Step 5 — Gap check: ask yourself "Is there a specific Kubernetes resource NOT in the
        evidence that, if inspected, would either confirm or completely change my diagnosis?"
        If yes, set needsMoreInfo=true and list those resources. It is always better to ask
        for more evidence than to commit to a wrong root cause. Do NOT fabricate confidence.

        Step 6 — Commit. Then write the JSON.

        Output ONLY English and ONLY a valid JSON object — no markdown fences, no prose outside JSON.
        Use exactly these keys:

        - "openingRant": One or two sentences. Pure scathing, humiliating, accusatory mockery aimed directly
          at the engineer who did this. No technical content — just contempt. Be creative and brutal.

        - "likelyRootCause": One sentence. The exact technical root cause, stated plainly and
          specifically (resource name, key name, path, etc). No insults here — just the fact.
          If needsMoreInfo is true, state your best current hypothesis rather than leaving it blank.

        - "evidenceChain": Two to four sentences. Walk through the evidence that proves this is
          the root cause. Reference specific log lines, event reasons, ConfigMap keys, PVC states,
          etc. Show your work.

        - "recommendation": Numbered steps only, one action per step, max 4 steps. Each step must
          include the exact kubectl command or config change. No fluff, no repeating the root cause.
          If needsMoreInfo is true, step 1 should be the kubectl command to inspect the missing resource.

        - "closingInsult": One sentence. A final parting shot — sigh, eye-roll, or insult to send
          them off feeling appropriately humiliated.

        - "confidence": 0.0–1.0. Be HONEST and CONSERVATIVE.
          Use 0.9+ ONLY when multiple independent signals (events, logs, config, PVC state)
          ALL point to the same single root cause with no ambiguity.
          Use 0.7–0.89 when the evidence is strong but one signal is missing or indirect.
          Use 0.5–0.69 when the root cause is a reasonable hypothesis but not proven.
          Use below 0.5 when you are genuinely uncertain or key resources are missing.
          Default to lower confidence rather than higher when in doubt.
          If prior diagnoses were rejected, drop your confidence by at least 0.1 from where
          you would otherwise place it.

        - "needsMoreInfo": PREFER true over false when in doubt.
          Set true when ANY of the following apply:
            • your confidence is below 0.7 AND inspecting a specific missing resource could
              materially change the diagnosis (not just confirm it)
            • the failure could be caused by two or more equally plausible root causes and
              additional resource data would disambiguate them
            • a resource referenced in events or logs (e.g. a Service, ConfigMap, Ingress,
              Secret, StorageClass) was NOT included in the evidence
          Set false ONLY when you have multiple independent corroborating signals that already
          pin the root cause unambiguously (confidence ≥ 0.8).
          When uncertain, asking for more info is always the correct choice.

        - "suggestedResources": array of objects with "kind", "apiGroup", and "reason". REQUIRED when
          needsMoreInfo is true — do NOT leave it empty in that case.
          List every specific resource kind that would materially help. For each, provide:
            - "kind": the Kubernetes Kind name (e.g. "ScaledObject", "Ingress")
            - "apiGroup": the API group (e.g. "keda.sh", "networking.k8s.io"). Use "" for core resources.
            - "reason": one sentence explaining exactly what you expect to find and how it would change or confirm the diagnosis.
          Example: {{"kind": "ScaledObject", "apiGroup": "keda.sh", "reason": "KEDA ScaledObject may have scaled the deployment to zero replicas due to an off-hours schedule."}}
          Leave as empty array [] only when needsMoreInfo is false.

        - "suggestFalsePositive": ONLY emit this field as `true` when ALL of the following hold:
          (1) the incident source is MANUAL ("INCIDENT SOURCE: This incident was MANUALLY reported"),
          (2) the evidence shows the behaviour is INTENTIONAL and expected (not a real failure).
          Do NOT emit this for auto-detected incidents — use suggestExclusionRule instead.
          Omit the field (or set to false) when not applicable.

        - "suggestExclusionRule": ONLY emit this field when you have HIGH CONFIDENCE (≥ 0.85) that
          the behaviour is INTENTIONAL and operator-configured — not a failure, AND the incident
          was AUTO-DETECTED (never emit for manual incidents — use suggestFalsePositive instead). Classic cases:
            • KEDA / HPA scaling a deployment to 0 during off-hours (ScaledObject confirms it)
            • A maintenance drain or PodDisruptionBudget intentionally evicting pods
            • A CronJob intentionally leaving no running pods between runs
          DO NOT suggest a rule for transient failures, misconfigurations, or anything that is
          genuinely broken. The "reason" must reference the specific evidence that proves intent
          (e.g. the ScaledObject schedule). Omit the field entirely (null) when not applicable.
          Shape:
            {{
              "reason": "<plain-language explanation of why this is expected behaviour>",
              "detectors": ["<detector name that fired, e.g. ServiceNoEndpoints>"],
              "targetResources": [{{"namespace": "<ns>", "kind": "<Kind>", "name": "<name>", "apiGroup": "<group or empty>"}}],
              "timeWindow": {{"timezone": "<IANA tz>", "periods": [{{"start": "HH:MM", "end": "HH:MM", "days": ["Mon","Tue","Wed","Thu","Fri"]}}]}} or null
            }}

        {{
          "openingRant": "<pure mockery, no technical content>",
          "likelyRootCause": "<exact technical cause, one sentence>",
          "evidenceChain": "<2-4 sentences citing specific evidence>",
          "recommendation": "<numbered steps with exact commands>",
          "closingInsult": "<one final humiliating remark>",
          "confidence": <0.0-1.0>,
          "needsMoreInfo": <true|false>,
          "suggestedResources": [{{"kind": "<Kind>", "apiGroup": "<apiGroup or empty>", "reason": "<one sentence>"}}],          "suggestFalsePositive": false,          "suggestExclusionRule": null
        }}
    """).strip()

    char_budget = EVIDENCE_TOKEN_BUDGET * 4
    if len(prompt) > char_budget:
        prompt = prompt[:char_budget] + "\n...(truncated)"
    return prompt
