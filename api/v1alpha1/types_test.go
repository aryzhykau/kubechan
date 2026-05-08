// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"
)

// ── ResourceRef ──────────────────────────────────────────────────────────────

func TestResourceRef_Fields(t *testing.T) {
	t.Parallel()
	ref := ResourceRef{
		Namespace:      "default",
		Kind:           "Deployment",
		Name:           "my-app",
		APIGroup:       "apps",
		EvidenceSlices: []string{"spec", "events"},
	}
	if ref.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "default")
	}
	if ref.Kind != "Deployment" {
		t.Errorf("Kind = %q, want %q", ref.Kind, "Deployment")
	}
	if ref.APIGroup != "apps" {
		t.Errorf("APIGroup = %q, want %q", ref.APIGroup, "apps")
	}
	if len(ref.EvidenceSlices) != 2 {
		t.Errorf("EvidenceSlices len = %d, want 2", len(ref.EvidenceSlices))
	}
}

func TestResourceRef_ClusterScoped(t *testing.T) {
	t.Parallel()
	ref := ResourceRef{Kind: "Node", Name: "node-1"}
	if ref.Namespace != "" {
		t.Errorf("expected empty Namespace for cluster-scoped resource, got %q", ref.Namespace)
	}
}

// ── ProblemCase constants & states ───────────────────────────────────────────

func TestProblemCaseSeverity_Values(t *testing.T) {
	t.Parallel()
	severities := []ProblemCaseSeverity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow}
	expected := []string{"critical", "high", "medium", "low"}
	for i, s := range severities {
		if string(s) != expected[i] {
			t.Errorf("severity[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestProblemCaseState_Values(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state ProblemCaseState
		want  string
	}{
		{ProblemCaseStateOpen, "open"},
		{ProblemCaseStateInvestigating, "investigating"},
		{ProblemCaseStateResolved, "resolved"},
	}
	for _, tc := range tests {
		if string(tc.state) != tc.want {
			t.Errorf("state %q: got %q, want %q", tc.state, tc.state, tc.want)
		}
	}
}

// ── Incident constants & states ───────────────────────────────────────────────

func TestIncidentState_Values(t *testing.T) {
	t.Parallel()
	if string(IncidentStateOpen) != "open" {
		t.Errorf("IncidentStateOpen = %q, want \"open\"", IncidentStateOpen)
	}
	if string(IncidentStateResolved) != "resolved" {
		t.Errorf("IncidentStateResolved = %q, want \"resolved\"", IncidentStateResolved)
	}
}

// ── DiagnosticRun constants & states ─────────────────────────────────────────

func TestDiagnosticRunState_Values(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state DiagnosticRunState
		want  string
	}{
		{DiagnosticRunStatePending, "pending"},
		{DiagnosticRunStateRunning, "running"},
		{DiagnosticRunStateCompleted, "completed"},
		{DiagnosticRunStateFailed, "failed"},
	}
	for _, tc := range tests {
		if string(tc.state) != tc.want {
			t.Errorf("DiagnosticRunState %q: got %q, want %q", tc.want, tc.state, tc.want)
		}
	}
}

// ── KubeChanMoodLevel ─────────────────────────────────────────────────────────

func TestKubeChanMoodLevel_Values(t *testing.T) {
	t.Parallel()
	if MoodCalm != 0 {
		t.Errorf("MoodCalm = %d, want 0", MoodCalm)
	}
	if MoodIrritated != 1 {
		t.Errorf("MoodIrritated = %d, want 1", MoodIrritated)
	}
	if MoodRage != 2 {
		t.Errorf("MoodRage = %d, want 2", MoodRage)
	}
}

// ── ExclusionRuleSpec ─────────────────────────────────────────────────────────

func TestExclusionRuleSpec_Defaults(t *testing.T) {
	t.Parallel()
	spec := ExclusionRuleSpec{Enabled: true}
	if !spec.Enabled {
		t.Error("Enabled should be true")
	}
	if spec.Namespace != "" {
		t.Errorf("Namespace should default to empty, got %q", spec.Namespace)
	}
	if len(spec.Detectors) != 0 {
		t.Errorf("Detectors should default to empty, got %v", spec.Detectors)
	}
	if spec.TimeWindow != nil {
		t.Error("TimeWindow should default to nil (24/7)")
	}
}

func TestExclusionSelector_AllFields(t *testing.T) {
	t.Parallel()
	sel := ExclusionSelector{
		Namespace:   "production",
		Kinds:       []string{"Deployment", "StatefulSet"},
		MatchLabels: map[string]string{"app": "api", "env": "prod"},
	}
	if sel.Namespace != "production" {
		t.Errorf("Namespace = %q", sel.Namespace)
	}
	if len(sel.Kinds) != 2 {
		t.Errorf("Kinds len = %d", len(sel.Kinds))
	}
	if sel.MatchLabels["app"] != "api" {
		t.Errorf("MatchLabels[app] = %q", sel.MatchLabels["app"])
	}
}

// ── RedactionSummary & LogTruncationInfo ──────────────────────────────────────

func TestRedactionSummary(t *testing.T) {
	t.Parallel()
	rs := RedactionSummary{
		PatternsApplied: 3,
		RedactedFields:  []string{"spec.env[0].value", "data.password"},
	}
	if rs.PatternsApplied != 3 {
		t.Errorf("PatternsApplied = %d, want 3", rs.PatternsApplied)
	}
	if len(rs.RedactedFields) != 2 {
		t.Errorf("RedactedFields len = %d, want 2", len(rs.RedactedFields))
	}
}

func TestLogTruncationInfo(t *testing.T) {
	t.Parallel()
	info := LogTruncationInfo{
		Truncated:      true,
		OriginalBytes:  1024 * 1024,
		TruncatedBytes: 512 * 1024,
	}
	if !info.Truncated {
		t.Error("Truncated should be true")
	}
	if info.OriginalBytes <= info.TruncatedBytes {
		t.Error("OriginalBytes should be > TruncatedBytes")
	}
}

// ── DeepCopy coverage (generated code) ───────────────────────────────────────

func TestDeepCopy_AllTypes(t *testing.T) {
	t.Parallel()

	// ResourceRef
	ref := ResourceRef{Kind: "Deployment", Namespace: "ns", Name: "app", EvidenceSlices: []string{"spec"}}
	ref.DeepCopyInto(&ResourceRef{})
	_ = ref.DeepCopy()

	// RedactionSummary
	rs := RedactionSummary{PatternsApplied: 1, RedactedFields: []string{"a"}}
	rs.DeepCopyInto(&RedactionSummary{})
	_ = rs.DeepCopy()

	// LogTruncationInfo
	lti := LogTruncationInfo{Truncated: true, OriginalBytes: 10, TruncatedBytes: 5}
	lti.DeepCopyInto(&LogTruncationInfo{})
	_ = lti.DeepCopy()

	// ExclusionPeriod
	ep := ExclusionPeriod{Start: "09:00", End: "18:00", Days: []string{"Mon"}}
	ep.DeepCopyInto(&ExclusionPeriod{})
	_ = ep.DeepCopy()

	// ExclusionTimeWindow
	etw := ExclusionTimeWindow{Timezone: "UTC", Periods: []ExclusionPeriod{ep}}
	etw.DeepCopyInto(&ExclusionTimeWindow{})
	_ = etw.DeepCopy()

	// ExclusionSelector
	es := ExclusionSelector{Namespace: "ns", Kinds: []string{"Deployment"}, MatchLabels: map[string]string{"app": "x"}}
	es.DeepCopyInto(&ExclusionSelector{})
	_ = es.DeepCopy()

	// ExclusionRuleSpec
	ers := ExclusionRuleSpec{Enabled: true, Namespace: "ns", Detectors: []string{"A"}, Selector: &es, TimeWindow: &etw}
	ers.DeepCopyInto(&ExclusionRuleSpec{})
	_ = ers.DeepCopy()

	// ExclusionRuleStatus
	erst := ExclusionRuleStatus{SuppressedCount: 2}
	erst.DeepCopyInto(&ExclusionRuleStatus{})
	_ = erst.DeepCopy()

	// KubechanExclusionRule
	rule := KubechanExclusionRule{Spec: ers, Status: erst}
	rule.DeepCopyInto(&KubechanExclusionRule{})
	_ = rule.DeepCopy()
	_ = rule.DeepCopyObject()

	// KubechanExclusionRuleList
	ruleList := KubechanExclusionRuleList{Items: []KubechanExclusionRule{rule}}
	ruleList.DeepCopyInto(&KubechanExclusionRuleList{})
	_ = ruleList.DeepCopy()
	_ = ruleList.DeepCopyObject()

	// ProblemCaseSpec
	pcs := ProblemCaseSpec{AffectedResource: ref, Detector: "X", Severity: SeverityHigh, Symptoms: []string{"s"}}
	pcs.DeepCopyInto(&ProblemCaseSpec{})
	_ = pcs.DeepCopy()

	// ProblemCaseStatus
	pcst := ProblemCaseStatus{State: ProblemCaseStateOpen}
	pcst.DeepCopyInto(&ProblemCaseStatus{})
	_ = pcst.DeepCopy()

	// ProblemCase
	pc := ProblemCase{Spec: pcs, Status: pcst}
	pc.DeepCopyInto(&ProblemCase{})
	_ = pc.DeepCopy()
	_ = pc.DeepCopyObject()

	// ProblemCaseList
	pcl := ProblemCaseList{Items: []ProblemCase{pc}}
	pcl.DeepCopyInto(&ProblemCaseList{})
	_ = pcl.DeepCopy()
	_ = pcl.DeepCopyObject()

	// IncidentSpec
	is := IncidentSpec{RelatedResources: []ResourceRef{ref}}
	is.DeepCopyInto(&IncidentSpec{})
	_ = is.DeepCopy()

	// IncidentStatus
	ist := IncidentStatus{State: IncidentStateOpen}
	ist.DeepCopyInto(&IncidentStatus{})
	_ = ist.DeepCopy()

	// Incident
	inc := Incident{Spec: is, Status: ist}
	inc.DeepCopyInto(&Incident{})
	_ = inc.DeepCopy()
	_ = inc.DeepCopyObject()

	// IncidentList
	il := IncidentList{Items: []Incident{inc}}
	il.DeepCopyInto(&IncidentList{})
	_ = il.DeepCopy()
	_ = il.DeepCopyObject()

	// DiagnosticRunSpec
	drs := DiagnosticRunSpec{IncidentRef: "inc-1"}
	drs.DeepCopyInto(&DiagnosticRunSpec{})
	_ = drs.DeepCopy()

	// DiagnosticRunStatus
	drst := DiagnosticRunStatus{State: DiagnosticRunStatePending}
	drst.DeepCopyInto(&DiagnosticRunStatus{})
	_ = drst.DeepCopy()

	// DiagnosticRun
	dr := DiagnosticRun{Spec: drs, Status: drst}
	dr.DeepCopyInto(&DiagnosticRun{})
	_ = dr.DeepCopy()
	_ = dr.DeepCopyObject()

	// DiagnosticRunList
	drl := DiagnosticRunList{Items: []DiagnosticRun{dr}}
	drl.DeepCopyInto(&DiagnosticRunList{})
	_ = drl.DeepCopy()
	_ = drl.DeepCopyObject()

	// KubeChanStateSpec
	ksspec := KubeChanStateSpec{}
	ksspec.DeepCopyInto(&KubeChanStateSpec{})
	_ = ksspec.DeepCopy()

	// KubeChanStateStatus
	ksst := KubeChanStateStatus{MoodLevel: MoodCalm}
	ksst.DeepCopyInto(&KubeChanStateStatus{})
	_ = ksst.DeepCopy()

	// KubeChanState
	ks := KubeChanState{Spec: ksspec, Status: ksst}
	ks.DeepCopyInto(&KubeChanState{})
	_ = ks.DeepCopy()
	_ = ks.DeepCopyObject()

	// KubeChanStateList
	ksl := KubeChanStateList{Items: []KubeChanState{ks}}
	ksl.DeepCopyInto(&KubeChanStateList{})
	_ = ksl.DeepCopy()
	_ = ksl.DeepCopyObject()
}
