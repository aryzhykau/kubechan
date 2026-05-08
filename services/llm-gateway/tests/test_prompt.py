"""Unit tests for app.prompt — build_prompt and helpers."""
from __future__ import annotations

import pytest

from app.prompt import (
    _fmt_events,
    _fmt_logs,
    _build_mood_note,
    _build_reanalysis_note,
    _build_prior_history_note,
    _build_user_message_note,
    _build_workload_sections,
    _build_pvc_sections,
    _build_related_sections,
    build_prompt,
)


# ---------------------------------------------------------------------------
# _fmt_events
# ---------------------------------------------------------------------------

class TestFmtEvents:
    def test_empty_list(self):
        assert _fmt_events([]) == "  (none)"

    def test_single_event(self):
        events = [{"type": "Warning", "reason": "OOMKilled", "message": "out of mem", "count": 3, "lastTime": "2026-01-01T00:00:00Z"}]
        result = _fmt_events(events)
        assert "[Warning]" in result
        assert "OOMKilled" in result
        assert "out of mem" in result
        assert "count=3" in result

    def test_missing_fields_use_defaults(self):
        result = _fmt_events([{}])
        assert "[?]" in result
        assert "count=0" in result


# ---------------------------------------------------------------------------
# _fmt_logs
# ---------------------------------------------------------------------------

class TestFmtLogs:
    def test_empty_logs(self):
        assert _fmt_logs("", "my-pod") == "  (no logs)"

    def test_logs_truncated_to_50_lines(self):
        logs = "\n".join(f"line {i}" for i in range(100))
        result = _fmt_logs(logs, "pod")
        lines = result.strip().splitlines()
        # First line is "[pod: pod]", rest are log lines (last 50 of 100)
        assert "line 99" in result
        assert "line 49" not in result  # first half should be dropped

    def test_short_logs_not_truncated(self):
        logs = "line1\nline2\nline3"
        result = _fmt_logs(logs, "pod")
        assert "line1" in result
        assert "line2" in result
        assert "line3" in result

    def test_pod_name_appears_in_output(self):
        result = _fmt_logs("some log", "my-special-pod")
        assert "my-special-pod" in result


# ---------------------------------------------------------------------------
# _build_mood_note
# ---------------------------------------------------------------------------

class TestBuildMoodNote:
    def test_mood_0_empty(self):
        assert _build_mood_note(0) == ""

    def test_mood_negative_empty(self):
        assert _build_mood_note(-1) == ""

    def test_mood_1_irritated(self):
        note = _build_mood_note(1)
        assert "IRRITATED" in note

    def test_mood_2_rage(self):
        note = _build_mood_note(2)
        assert "RAGE" in note

    def test_mood_3_also_rage(self):
        note = _build_mood_note(3)
        assert "RAGE" in note


# ---------------------------------------------------------------------------
# _build_reanalysis_note
# ---------------------------------------------------------------------------

class TestBuildReanalysisNote:
    def test_zero_empty(self):
        assert _build_reanalysis_note(0) == ""

    def test_one_second_time_message(self):
        note = _build_reanalysis_note(1)
        assert "second time" in note.lower() or "RE-ANALYSIS" in note

    def test_two_escalated_message(self):
        note = _build_reanalysis_note(2)
        assert "RE-ANALYSIS" in note
        assert "3" in note  # analysis #3

    def test_count_appears_in_message(self):
        note = _build_reanalysis_note(5)
        assert "6" in note  # analysis #6


# ---------------------------------------------------------------------------
# _build_prior_history_note
# ---------------------------------------------------------------------------

class TestBuildPriorHistoryNote:
    def test_empty_list(self):
        assert _build_prior_history_note([]) == ""

    def test_confirmed_diagnosis(self):
        prior = [{"attempt": 1, "likelyRootCause": "missing secret", "userRating": "up"}]
        note = _build_prior_history_note(prior)
        assert "CONFIRMED" in note
        assert "missing secret" in note

    def test_rejected_diagnosis(self):
        prior = [{"attempt": 1, "likelyRootCause": "wrong cause", "userRating": "down"}]
        note = _build_prior_history_note(prior)
        assert "REJECTED" in note
        assert "wrong cause" in note
        assert "CRITICAL" in note

    def test_no_rating(self):
        prior = [{"attempt": 1, "likelyRootCause": "some cause", "userRating": ""}]
        note = _build_prior_history_note(prior)
        assert "some cause" in note
        # Should not contain rejected warning when no rating
        assert "CRITICAL" not in note


# ---------------------------------------------------------------------------
# _build_user_message_note
# ---------------------------------------------------------------------------

class TestBuildUserMessageNote:
    def test_empty_message(self):
        assert _build_user_message_note("") == ""

    def test_message_included(self):
        note = _build_user_message_note("pods keep crashing")
        assert "pods keep crashing" in note
        assert "USER REPORTED" in note


# ---------------------------------------------------------------------------
# build_prompt (integration-level)
# ---------------------------------------------------------------------------

_MINIMAL_PAYLOAD: dict = {
    "rootResource": {"kind": "Deployment", "name": "my-app"},
    "rootResourceEvents": [],
    "problemCases": [],
    "workloadPodLogs": [],
    "pvcInfos": [],
    "relatedResourceEvidence": [],
}


class TestBuildPrompt:
    def test_returns_string(self):
        result = build_prompt(_MINIMAL_PAYLOAD)
        assert isinstance(result, str)
        assert len(result) > 100

    def test_root_resource_in_prompt(self):
        result = build_prompt(_MINIMAL_PAYLOAD)
        assert "Deployment/my-app" in result

    def test_output_json_keys_mentioned(self):
        result = build_prompt(_MINIMAL_PAYLOAD)
        for key in ("openingRant", "likelyRootCause", "evidenceChain", "recommendation", "confidence"):
            assert key in result

    def test_user_message_included_when_provided(self):
        result = build_prompt(_MINIMAL_PAYLOAD, user_message="the app is down")
        assert "the app is down" in result

    def test_user_message_absent_when_empty(self):
        result = build_prompt(_MINIMAL_PAYLOAD, user_message="")
        assert "USER REPORTED" not in result

    def test_mood_note_included_when_nonzero(self):
        result = build_prompt(_MINIMAL_PAYLOAD, mood_level=1)
        assert "IRRITATED" in result

    def test_mood_note_absent_when_zero(self):
        result = build_prompt(_MINIMAL_PAYLOAD, mood_level=0)
        assert "IRRITATED" not in result

    def test_reanalysis_note_included(self):
        result = build_prompt(_MINIMAL_PAYLOAD, reanalysis_count=1)
        assert "RE-ANALYSIS" in result

    def test_prior_diagnoses_included(self):
        prior = [{"attempt": 1, "likelyRootCause": "bad config", "userRating": "down"}]
        result = build_prompt(_MINIMAL_PAYLOAD, prior_diagnoses=prior)
        assert "bad config" in result
        assert "REJECTED" in result

    def test_truncation_when_over_budget(self):
        from unittest.mock import patch
        # Patch EVIDENCE_TOKEN_BUDGET to a tiny value so the prompt is guaranteed to truncate.
        with patch("app.prompt.EVIDENCE_TOKEN_BUDGET", 10):
            result = build_prompt(_MINIMAL_PAYLOAD)
        assert result.endswith("...(truncated)")

    def test_no_truncation_when_under_budget(self):
        result = build_prompt(_MINIMAL_PAYLOAD)
        assert not result.endswith("...(truncated)")

    def test_manual_incident_source(self):
        result = build_prompt(_MINIMAL_PAYLOAD, incident_source="manual")
        assert "MANUALLY reported" in result

    def test_auto_incident_source(self):
        result = build_prompt(_MINIMAL_PAYLOAD, incident_source="auto")
        assert "AUTO-DETECTED" in result


# ---------------------------------------------------------------------------
# _build_workload_sections
# ---------------------------------------------------------------------------

class TestBuildWorkloadSections:
    def test_empty_list(self):
        assert _build_workload_sections([]) == []

    def test_basic_workload(self):
        wl = {"podName": "my-pod", "phase": "CrashLoopBackOff", "events": [], "logs": "err: boom"}
        sections = _build_workload_sections([wl])
        assert len(sections) == 1
        assert "my-pod" in sections[0]
        assert "CrashLoopBackOff" in sections[0]
        assert "err: boom" in sections[0]

    def test_configmap_volume_mounted(self):
        wl = {
            "podName": "p",
            "phase": "Running",
            "events": [],
            "logs": "",
            "dependencies": {
                "configMaps": [
                    {
                        "name": "app-config",
                        "missing": False,
                        "keys": ["key1", "key2"],
                        "mountPaths": ["/etc/config"],
                        "data": {"key1": "val1"},
                    }
                ],
                "secrets": [],
            },
        }
        sections = _build_workload_sections([wl])
        assert "app-config" in sections[0]
        assert "volume-mounted" in sections[0]
        assert "/etc/config/key1" in sections[0]
        assert "val1" in sections[0]

    def test_configmap_env_injected(self):
        wl = {
            "podName": "p",
            "phase": "Running",
            "events": [],
            "logs": "",
            "dependencies": {
                "configMaps": [
                    {
                        "name": "env-cfg",
                        "missing": False,
                        "keys": ["DB_HOST"],
                        "mountPaths": [],
                        "data": {},
                    }
                ],
                "secrets": [],
            },
        }
        sections = _build_workload_sections([wl])
        assert "env-cfg" in sections[0]
        assert "environment variables" in sections[0]
        assert "DB_HOST" in sections[0]

    def test_missing_configmap(self):
        wl = {
            "podName": "p",
            "phase": "Pending",
            "events": [],
            "logs": "",
            "dependencies": {
                "configMaps": [{"name": "ghost-config", "missing": True}],
                "secrets": [],
            },
        }
        sections = _build_workload_sections([wl])
        assert "MISSING" in sections[0]
        assert "ghost-config" in sections[0]

    def test_secret_existing(self):
        wl = {
            "podName": "p",
            "phase": "Running",
            "events": [],
            "logs": "",
            "dependencies": {
                "configMaps": [],
                "secrets": [{"name": "db-creds", "missing": False}],
            },
        }
        sections = _build_workload_sections([wl])
        assert "db-creds" in sections[0]
        assert "redacted" in sections[0]

    def test_secret_missing(self):
        wl = {
            "podName": "p",
            "phase": "Pending",
            "events": [],
            "logs": "",
            "dependencies": {
                "configMaps": [],
                "secrets": [{"name": "ghost-secret", "missing": True}],
            },
        }
        sections = _build_workload_sections([wl])
        assert "MISSING" in sections[0]
        assert "ghost-secret" in sections[0]


# ---------------------------------------------------------------------------
# _build_pvc_sections
# ---------------------------------------------------------------------------

class TestBuildPvcSections:
    def test_empty_list(self):
        assert _build_pvc_sections([]) == []

    def test_basic_pvc(self):
        pvc = {
            "name": "data-pvc",
            "phase": "Pending",
            "storageClass": "standard",
            "requestedStorage": "10Gi",
            "events": [{"type": "Warning", "reason": "ProvisioningFailed", "message": "no capacity", "count": 1, "lastTime": ""}],
        }
        sections = _build_pvc_sections([pvc])
        assert len(sections) == 1
        assert "data-pvc" in sections[0]
        assert "Pending" in sections[0]
        assert "ProvisioningFailed" in sections[0]


# ---------------------------------------------------------------------------
# _build_related_sections
# ---------------------------------------------------------------------------

class TestBuildRelatedSections:
    def test_empty_list(self):
        assert _build_related_sections([]) == []

    def _make_rr(self, kind, name, spec=None, api_group=""):
        return {
            "resource": {"kind": kind, "name": name, "apiGroup": api_group},
            "spec": spec or {},
            "events": [],
        }

    def test_ingress_with_rules_and_annotations(self):
        rr = self._make_rr("Ingress", "my-ingress", spec={
            "ingressClassName": "nginx",
            "rules": [{"host": "example.com", "paths": [{"path": "/", "backendService": "svc", "backendPort": 80}]}],
            "annotations": {"nginx.ingress.kubernetes.io/rewrite-target": "/"},
        }, api_group="networking.k8s.io")
        sections = _build_related_sections([rr])
        assert "example.com" in sections[0]
        assert "nginx" in sections[0]
        assert "rewrite-target" in sections[0]

    def test_service_with_ports_and_annotations(self):
        rr = self._make_rr("Service", "my-svc", spec={
            "type": "ClusterIP",
            "selector": {"app": "myapp"},
            "clusterIP": "10.96.0.1",
            "ports": [{"name": "http", "port": 80, "targetPort": 8080, "protocol": "TCP"}],
            "annotations": {"custom-ann": "value"},
        })
        sections = _build_related_sections([rr])
        assert "ClusterIP" in sections[0]
        assert "10.96.0.1" in sections[0]
        assert "custom-ann" in sections[0]

    def test_configmap_with_data(self):
        rr = self._make_rr("ConfigMap", "my-cm", spec={"data": {"key": "value"}})
        sections = _build_related_sections([rr])
        assert "key" in sections[0]
        assert "value" in sections[0]

    def test_deployment_with_conditions(self):
        rr = self._make_rr("Deployment", "my-deploy", spec={
            "replicas": 3,
            "readyReplicas": 0,
            "conditions": [{"type": "Available", "status": "False", "message": "not enough replicas"}],
        })
        sections = _build_related_sections([rr])
        assert "0/3" in sections[0]
        assert "not enough replicas" in sections[0]

    def test_cronjob(self):
        rr = self._make_rr("CronJob", "nightly-job", spec={
            "schedule": "0 2 * * *",
            "suspend": False,
            "lastScheduleTime": "2026-05-07T02:00:00Z",
            "lastSuccessfulTime": "2026-05-07T02:01:00Z",
        })
        sections = _build_related_sections([rr])
        assert "0 2 * * *" in sections[0]
        assert "2026-05-07" in sections[0]

    def test_job(self):
        rr = self._make_rr("Job", "batch-job", spec={"active": 0, "succeeded": 1, "failed": 0})
        sections = _build_related_sections([rr])
        assert "batch-job" in sections[0]
        assert "succeeded" in sections[0]

    def test_generic_crd(self):
        rr = self._make_rr("ScaledObject", "my-scaler", spec={
            "spec": {"minReplicaCount": 0, "maxReplicaCount": 5},
            "status": {"lastActiveTime": "2026-05-07T22:00:00Z"},
        }, api_group="keda.sh")
        sections = _build_related_sections([rr])
        assert "minReplicaCount" in sections[0]
        assert "lastActiveTime" in sections[0]
        assert "keda.sh/ScaledObject" in sections[0]

    def test_no_spec_still_renders_events(self):
        rr = {
            "resource": {"kind": "Pod", "name": "broken-pod", "apiGroup": ""},
            "spec": None,
            "events": [{"type": "Warning", "reason": "OOMKilled", "message": "oom", "count": 2, "lastTime": ""}],
        }
        sections = _build_related_sections([rr])
        assert "OOMKilled" in sections[0]

    def test_events_included_in_prompt(self):
        payload = {
            **_MINIMAL_PAYLOAD,
            "rootResourceEvents": [
                {"type": "Warning", "reason": "BackOff", "message": "back-off restarting", "count": 10, "lastTime": "2026-01-01T00:00:00Z"}
            ],
        }
        result = build_prompt(payload)
        assert "BackOff" in result

    def test_problem_case_included(self):
        payload = {
            **_MINIMAL_PAYLOAD,
            "problemCases": [{
                "name": "pc-abc",
                "detector": "crashloop",
                "severity": "critical",
                "symptoms": ["CrashLoopBackOff"],
                "affectedResource": {"kind": "Pod", "name": "my-pod", "namespace": "default"},
                "events": [],
                "logs": "Error: segfault",
            }],
        }
        result = build_prompt(payload)
        assert "pc-abc" in result
        assert "crashloop" in result
        assert "segfault" in result

    def test_pvc_section_included(self):
        payload = {
            **_MINIMAL_PAYLOAD,
            "pvcInfos": [{
                "name": "data-pvc",
                "phase": "Pending",
                "storageClass": "standard",
                "requestedStorage": "5Gi",
                "events": [],
            }],
        }
        result = build_prompt(payload)
        assert "data-pvc" in result
        assert "Pending" in result
