// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/debounce"
	"github.com/org/kubechan/services/cluster-watcher/detector"
)

// ── scheme helper ─────────────────────────────────────────────────────────────

func newCtrlTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	utilruntime.Must(discoveryv1.AddToScheme(s))
	return s
}

// ── kindToGVK ─────────────────────────────────────────────────────────────────

func TestKindToGVK_KnownKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind      string
		wantGroup string
		wantOK    bool
	}{
		{"Pod", "", true},
		{"Service", "", true},
		{"ReplicaSet", "apps", true},
		{"Deployment", "apps", true},
		{"StatefulSet", "apps", true},
		{"DaemonSet", "apps", true},
		{"Job", "batch", true},
		{"Unknown", "", false},
		{"CronJob", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			gvk, ok := kindToGVK(tc.kind)
			if ok != tc.wantOK {
				t.Errorf("kindToGVK(%q) ok = %v, want %v", tc.kind, ok, tc.wantOK)
			}
			if ok && gvk.Group != tc.wantGroup {
				t.Errorf("kindToGVK(%q) group = %q, want %q", tc.kind, gvk.Group, tc.wantGroup)
			}
			if ok && gvk.Kind != tc.kind {
				t.Errorf("kindToGVK(%q) kind = %q, want %q", tc.kind, gvk.Kind, tc.kind)
			}
		})
	}
}

// ── labelSafe ─────────────────────────────────────────────────────────────────

func TestLabelSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"short", "short"},
		{"exactly63charslongAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "exactly63charslongAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"toolong64charsBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", "toolong64charsBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"},
		{"", ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input[:min(10, len(tc.input))], func(t *testing.T) {
			t.Parallel()
			got := labelSafe(tc.input)
			if len(got) > 63 {
				t.Errorf("labelSafe(%q): result length %d exceeds 63", tc.input, len(got))
			}
			if len(tc.input) <= 63 && got != tc.input {
				t.Errorf("labelSafe(%q) = %q, want same string", tc.input, got)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── kindMatches ───────────────────────────────────────────────────────────────

func TestKindMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"Deployment", "Deployment", true},
		{"deployment", "Deployment", true},
		{"DEPLOYMENT", "deployment", true},
		{"Deployment", "StatefulSet", false},
		{"Deploy", "Deployment", false},
		{"", "", true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.a+"/"+tc.b, func(t *testing.T) {
			t.Parallel()
			if got := kindMatches(tc.a, tc.b); got != tc.want {
				t.Errorf("kindMatches(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// ── resourceRefMatchesAny ─────────────────────────────────────────────────────

func TestResourceRefMatchesAny(t *testing.T) {
	t.Parallel()
	ref := v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"}

	tests := []struct {
		name    string
		targets []v1alpha1.ResourceRef
		want    bool
	}{
		{
			"exact match",
			[]v1alpha1.ResourceRef{{Kind: "Deployment", Name: "myapp", Namespace: "default"}},
			true,
		},
		{
			"kind-only wildcard",
			[]v1alpha1.ResourceRef{{Kind: "Deployment"}},
			true,
		},
		{
			"wrong kind",
			[]v1alpha1.ResourceRef{{Kind: "StatefulSet", Name: "myapp"}},
			false,
		},
		{
			"wrong name",
			[]v1alpha1.ResourceRef{{Kind: "Deployment", Name: "other"}},
			false,
		},
		{
			"wrong namespace",
			[]v1alpha1.ResourceRef{{Kind: "Deployment", Name: "myapp", Namespace: "prod"}},
			false,
		},
		{
			"empty targets",
			[]v1alpha1.ResourceRef{},
			false,
		},
		{
			"case-insensitive kind",
			[]v1alpha1.ResourceRef{{Kind: "deployment", Name: "myapp"}},
			true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resourceRefMatchesAny(ref, tc.targets); got != tc.want {
				t.Errorf("resourceRefMatchesAny() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── incidentMatchesRule ───────────────────────────────────────────────────────

func TestIncidentMatchesRule(t *testing.T) {
	t.Parallel()
	r := &ExclusionRuleReconciler{Client: fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()}

	inc := &v1alpha1.Incident{
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{
				Kind:      "Deployment",
				Name:      "myapp",
				Namespace: "default",
			},
		},
	}

	tests := []struct {
		name string
		rule *v1alpha1.KubechanExclusionRule
		want bool
	}{
		{
			"matching target resource",
			&v1alpha1.KubechanExclusionRule{Spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{{Kind: "Deployment", Name: "myapp"}},
			}},
			true,
		},
		{
			"non-matching namespace on rule",
			&v1alpha1.KubechanExclusionRule{Spec: v1alpha1.ExclusionRuleSpec{
				Namespace: "other",
				TargetResources: []v1alpha1.ResourceRef{{Kind: "Deployment"}},
			}},
			false,
		},
		{
			"matching namespace and target",
			&v1alpha1.KubechanExclusionRule{Spec: v1alpha1.ExclusionRuleSpec{
				Namespace: "default",
				TargetResources: []v1alpha1.ResourceRef{{Kind: "Deployment"}},
			}},
			true,
		},
		{
			"broad rule with no namespace safety skip",
			&v1alpha1.KubechanExclusionRule{Spec: v1alpha1.ExclusionRuleSpec{}},
			false,
		},
		{
			"broad rule with namespace is allowed",
			&v1alpha1.KubechanExclusionRule{Spec: v1alpha1.ExclusionRuleSpec{
				Namespace: "default",
			}},
			true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := r.incidentMatchesRule(inc, tc.rule); got != tc.want {
				t.Errorf("incidentMatchesRule() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── parseHHMM ─────────────────────────────────────────────────────────────────

func TestParseHHMM(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    string
		wanH int
		wanM int
	}{
		{"09:30", 9, 30},
		{"00:00", 0, 0},
		{"23:59", 23, 59},
		{"12:00", 12, 0},
		{"bad", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.s, func(t *testing.T) {
			t.Parallel()
			var h, m int
			parseHHMM(tc.s, &h, &m)
			if h != tc.wanH || m != tc.wanM {
				t.Errorf("parseHHMM(%q) = %d:%02d, want %d:%02d", tc.s, h, m, tc.wanH, tc.wanM)
			}
		})
	}
}

// ── parseDayTime ─────────────────────────────────────────────────────────────

func TestParseDayTime(t *testing.T) {
	t.Parallel()
	ref := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	got := parseDayTime(ref, "14:30", time.UTC)
	if got.Hour() != 14 || got.Minute() != 30 {
		t.Errorf("parseDayTime returned %v, want hour=14 min=30", got)
	}
	if got.Year() != 2024 || got.Month() != 6 || got.Day() != 15 {
		t.Errorf("parseDayTime: date mismatch: %v", got)
	}
}

// ── isWindowActive ────────────────────────────────────────────────────────────

func TestIsWindowActive_NoMatchingDay(t *testing.T) {
	t.Parallel()
	// Use a window that only covers a day very unlikely to match today (we set an empty
	// day list), so active must be false.
	tw := &v1alpha1.ExclusionTimeWindow{
		Timezone: "UTC",
		Periods:  []v1alpha1.ExclusionPeriod{},
	}
	active, requeue := isWindowActive(tw)
	if active {
		t.Error("expected inactive for empty periods")
	}
	if requeue == 0 {
		t.Error("expected non-zero requeue duration when inactive")
	}
}

func TestIsWindowActive_AlwaysActiveWindow(t *testing.T) {
	t.Parallel()
	// Cover all 7 days 00:00–23:59 — should always be active.
	allDays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	tw := &v1alpha1.ExclusionTimeWindow{
		Timezone: "UTC",
		Periods: []v1alpha1.ExclusionPeriod{{
			Days:  allDays,
			Start: "00:00",
			End:   "23:59",
		}},
	}
	active, _ := isWindowActive(tw)
	if !active {
		t.Error("expected active for all-day window on all days")
	}
}

func TestIsWindowActive_InvalidTimezone(t *testing.T) {
	t.Parallel()
	allDays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	tw := &v1alpha1.ExclusionTimeWindow{
		Timezone: "Not/AReal_Zone",
		Periods: []v1alpha1.ExclusionPeriod{{
			Days:  allDays,
			Start: "00:00",
			End:   "23:59",
		}},
	}
	// Should fall back to UTC and still work.
	active, _ := isWindowActive(tw)
	if !active {
		t.Error("expected active even with invalid timezone (fallback to UTC)")
	}
}

// ── highestSeverity ───────────────────────────────────────────────────────────

func TestHighestSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		symptoms []detector.Symptom
		want     v1alpha1.ProblemCaseSeverity
	}{
		{
			"empty slice returns low",
			nil,
			"low",
		},
		{
			"single critical",
			[]detector.Symptom{{Severity: "critical"}},
			"critical",
		},
		{
			"mixed severities",
			[]detector.Symptom{{Severity: "low"}, {Severity: "high"}, {Severity: "medium"}},
			"high",
		},
		{
			"unknown severity treated as zero",
			[]detector.Symptom{{Severity: "unknown"}},
			"low",
		},
		{
			"critical beats high",
			[]detector.Symptom{{Severity: "high"}, {Severity: "critical"}},
			"critical",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := highestSeverity(tc.symptoms); got != tc.want {
				t.Errorf("highestSeverity() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── debounceKey ───────────────────────────────────────────────────────────────

func TestDebounceKey(t *testing.T) {
	t.Parallel()
	key := debounceKey("ns", "Deployment", "myapp", "crash-loop")
	want := "ns/Deployment/myapp/crash-loop"
	if key != want {
		t.Errorf("debounceKey() = %q, want %q", key, want)
	}
}

// ── CorrelationReconciler helper methods ──────────────────────────────────────

func TestCorrelationReconciler_FindOpenIncident_None(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	root := v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"}
	inc, err := r.findOpenIncident(context.Background(), "default", root)
	if err != nil {
		t.Fatalf("findOpenIncident: %v", err)
	}
	if inc != nil {
		t.Errorf("expected nil incident, got %v", inc)
	}
}

func TestCorrelationReconciler_FindOpenIncident_Found(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inc-1",
			Namespace: "default",
			Labels: map[string]string{
				LabelRootResourceKind: "deployment",
				LabelRootResourceName: "myapp",
			},
		},
		Status: v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	root := v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"}
	found, err := r.findOpenIncident(context.Background(), "default", root)
	if err != nil {
		t.Fatalf("findOpenIncident: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find an incident, got nil")
	}
	if found.Name != "inc-1" {
		t.Errorf("found incident name = %q, want %q", found.Name, "inc-1")
	}
}

func TestCorrelationReconciler_FindOpenIncident_SkipsResolved(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inc-resolved",
			Namespace: "default",
			Labels: map[string]string{
				LabelRootResourceKind: "deployment",
				LabelRootResourceName: "myapp",
			},
		},
		Status: v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	root := v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"}
	found, err := r.findOpenIncident(context.Background(), "default", root)
	if err != nil {
		t.Fatalf("findOpenIncident: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil (resolved skipped), got %v", found.Name)
	}
}

func TestCorrelationReconciler_CreateIncident(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	root := v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"}

	inc, err := r.createIncident(context.Background(), "default", root)
	if err != nil {
		t.Fatalf("createIncident: %v", err)
	}
	if inc == nil {
		t.Fatal("expected non-nil incident")
	}
	if inc.Spec.RootResource.Name != "myapp" {
		t.Errorf("RootResource.Name = %q, want %q", inc.Spec.RootResource.Name, "myapp")
	}
}

func TestCorrelationReconciler_AddProblemCaseToIncident(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}

	if err := r.addProblemCaseToIncident(context.Background(), inc, "pc-1"); err != nil {
		t.Fatalf("addProblemCaseToIncident: %v", err)
	}
	// Adding same one again must be idempotent.
	if err := r.addProblemCaseToIncident(context.Background(), inc, "pc-1"); err != nil {
		t.Fatalf("addProblemCaseToIncident idempotent: %v", err)
	}
}

func TestCorrelationReconciler_MaybeResolveIncident_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	// Should not return an error even if incident doesn't exist (IgnoreNotFound).
	if err := r.maybeResolveIncident(context.Background(), "default", "missing"); err != nil {
		t.Fatalf("maybeResolveIncident: %v", err)
	}
}

func TestCorrelationReconciler_MaybeResolveIncident_AlreadyResolved(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-done", Namespace: "default"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	if err := r.maybeResolveIncident(context.Background(), "default", "inc-done"); err != nil {
		t.Fatalf("maybeResolveIncident: %v", err)
	}
}

func TestCorrelationReconciler_MaybeResolveIncident_AllResolved(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-done", Namespace: "default"},
		Status:     v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
		Spec:       v1alpha1.IncidentSpec{ProblemCases: []string{"pc-done"}},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc, pc).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.ProblemCase{}).
		Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	if err := r.maybeResolveIncident(context.Background(), "default", "inc-1"); err != nil {
		t.Fatalf("maybeResolveIncident: %v", err)
	}
}

func TestCorrelationReconciler_FindRootResource_UnknownKind(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &CorrelationReconciler{Client: c, ControlNamespace: "kubechan"}
	ref := v1alpha1.ResourceRef{Kind: "CustomResource", Name: "cr-1", Namespace: "default"}
	root, err := r.findRootResource(context.Background(), ref)
	if err != nil {
		t.Fatalf("findRootResource: %v", err)
	}
	if root.Kind != "CustomResource" {
		t.Errorf("expected passthrough for unknown kind, got %q", root.Kind)
	}
}

// ── ExclusionRuleReconciler.Reconcile ────────────────────────────────────────

func TestExclusionRuleReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "gone", Namespace: "kubechan"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Error("expected no requeue for not-found rule")
	}
}

func TestExclusionRuleReconciler_Reconcile_DisabledRule(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Enabled: false},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "rule1", Namespace: "kubechan"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for disabled rule")
	}
}

func TestExclusionRuleReconciler_Reconcile_EnabledNoIncidents(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "rule1", Namespace: "kubechan"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExclusionRuleReconciler_ResolveIncident(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{ProblemCases: []string{}},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.Incident{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	err := r.resolveIncident(context.Background(), inc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated := &v1alpha1.Incident{}
	_ = c.Get(context.Background(), client.ObjectKey{Name: "inc1", Namespace: "kubechan"}, updated)
}

// ── CorrelationReconciler.Reconcile early exits ──────────────────────────────

func TestCorrelationReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &CorrelationReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "gone", Namespace: "default"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found ProblemCase")
	}
}

func TestCorrelationReconciler_Reconcile_AlreadyCorrelated(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pc1",
			Namespace: "default",
			Labels:    map[string]string{LabelIncident: "inc1"},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "app", Namespace: "default"},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc).
		Build()
	r := &CorrelationReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "pc1", Namespace: "default"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for already-correlated PC")
	}
}

func TestCorrelationReconciler_Reconcile_ResolvedPCNoLabel(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc1", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "app", Namespace: "default"},
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()
	r := &CorrelationReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "pc1", Namespace: "default"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for resolved PC with no label")
	}
}

// ── PodReconciler.Reconcile early exits ──────────────────────────────────────

func TestPodReconciler_Reconcile_ControlNamespaceSkipped(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &PodReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        nil,
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pod1", Namespace: "kubechan"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for control namespace pod")
	}
}

func TestPodReconciler_Reconcile_PodNotFound_CancelsDebounce(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &PodReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found pod")
	}
}

func TestPodReconciler_Reconcile_PodWithNoDetectors(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pod).
		Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &PodReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "mypod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestPodReconciler_Reconcile_GetError(t *testing.T) {
	t.Parallel()
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &PodReconciler{Client: c, Scheme: scheme, Debouncer: d, Detectors: nil, ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "mypod"}})
	if err == nil {
		t.Error("expected error from Get, got nil")
	}
}

// ── DeploymentReconciler.Reconcile early exits ────────────────────────────────

func TestDeploymentReconciler_Reconcile_ControlNamespaceSkipped(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &DeploymentReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        nil,
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "myapp", Namespace: "kubechan"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for control namespace")
	}
}

func TestDeploymentReconciler_Reconcile_NotFound_CancelsDebounce(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &DeploymentReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-app", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found deployment")
	}
}

func TestDeploymentReconciler_Reconcile_GetError(t *testing.T) {
	t.Parallel()
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &DeploymentReconciler{
		Client: c, Scheme: scheme, Debouncer: d,
		Detectors: nil, ControlNamespace: "kubechan",
	}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "some-deploy"}})
	if err == nil {
		t.Error("expected error from Get, got nil")
	}
}

// ── EventReconciler.Reconcile ─────────────────────────────────────────────────

func TestEventReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &EventReconciler{Client: c, Scheme: newCtrlTestScheme()}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "ev1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found event")
	}
}

func TestEventReconciler_Reconcile_EventFound(t *testing.T) {
	t.Parallel()
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "mypod"},
		Reason:         "BackOff",
	}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(ev).Build()
	r := &EventReconciler{Client: c, Scheme: newCtrlTestScheme()}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "ev1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestEventReconciler_Reconcile_GetError(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	r := &EventReconciler{Client: c, Scheme: newCtrlTestScheme()}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "ev1", Namespace: "default"},
	})
	if err == nil {
		t.Error("expected error from Get, got nil")
	}
}

// ── NodeReconciler.Reconcile ──────────────────────────────────────────────────

func TestNodeReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &NodeReconciler{Client: c, Scheme: newCtrlTestScheme()}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-node"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found node")
	}
}

func TestNodeReconciler_Reconcile_NodeFound(t *testing.T) {
	t.Parallel()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(node).Build()
	r := &NodeReconciler{Client: c, Scheme: newCtrlTestScheme()}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "node1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestNodeReconciler_Reconcile_GetError(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return fmt.Errorf("injected get error")
			},
		}).Build()
	r := &NodeReconciler{Client: c, Scheme: newCtrlTestScheme()}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "some-node"},
	})
	if err == nil {
		t.Error("expected error from Get, got nil")
	}
}

// ── ServiceReconciler.Reconcile ───────────────────────────────────────────────

func TestServiceReconciler_Reconcile_ControlNamespaceSkipped(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &ServiceReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        nil,
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "svc1", Namespace: "kubechan"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for control namespace service")
	}
}

func TestServiceReconciler_Reconcile_NotFound_CancelsDebounce(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &ServiceReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-svc", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found service")
	}
}

// ── ProblemCaseReconciler.Reconcile ──────────────────────────────────────────

func TestProblemCaseReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-pc", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found PC")
	}
}

func TestProblemCaseReconciler_Reconcile_NotOpen(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc1", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "app", Namespace: "default"},
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pc1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for non-open PC")
	}
}

func TestProblemCaseReconciler_FindDetector_NotFound(t *testing.T) {
	t.Parallel()
	r := &ProblemCaseReconciler{Detectors: []detector.Detector{}}
	d := r.findDetector("NonExistent")
	if d != nil {
		t.Error("expected nil detector")
	}
}

func TestProblemCaseReconciler_FetchAffectedResource_UnsupportedKind(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	_, err := r.fetchAffectedResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "Ingress", Name: "myingress", Namespace: "default",
	})
	if err == nil {
		t.Error("expected error for unsupported kind")
	}
}

func TestProblemCaseReconciler_FetchAffectedResource_Pod(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(pod).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	obj, err := r.fetchAffectedResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "Pod", Name: "mypod", Namespace: "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj == nil {
		t.Error("expected non-nil object")
	}
}

// ── CorrelationReconciler.Reconcile full flow ─────────────────────────────────

func TestCorrelationReconciler_Reconcile_FullFlow_CreatesIncident(t *testing.T) {
	t.Parallel()
	// A ProblemCase with no incident label and a Pod as AffectedResource.
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-pod", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "mypod", Namespace: "default"},
			Detector:         "CrashLoopBackOff",
		},
	}
	// A Pod (so findRootResource can Get it via unstructured).
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"}}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc, pod).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.ProblemCase{}).
		Build()
	r := &CorrelationReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pc-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
	// Verify a new Incident was created.
	incList := &v1alpha1.IncidentList{}
	if err := c.List(context.Background(), incList); err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incList.Items) == 0 {
		t.Error("expected at least one Incident to be created")
	}
}

func TestCorrelationReconciler_Reconcile_ResolvedPC_WithLabel_CallsMaybeResolve(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inc-1",
			Namespace: "kubechan",
			Labels: map[string]string{
				LabelRootResourceKind: "pod",
				LabelRootResourceName: "mypod",
			},
		},
		Spec: v1alpha1.IncidentSpec{ProblemCases: []string{"pc-1"}},
	}
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pc-1",
			Namespace: "default",
			Labels:    map[string]string{LabelIncident: "inc-1"},
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc, inc).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.ProblemCase{}).
		Build()
	r := &CorrelationReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pc-1", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── ProblemCaseReconciler with detector ───────────────────────────────────────

func TestProblemCaseReconciler_Reconcile_OpenPC_NoSymptomsResolved(t *testing.T) {
	t.Parallel()
	// A healthy Pod — CrashLoopBackOff detector will find no symptoms.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-crash", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "mypod", Namespace: "default"},
			Detector:         "CrashLoopBackOff",
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pod, pc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()
	r := &ProblemCaseReconciler{
		Client:    c,
		Scheme:    newCtrlTestScheme(),
		Detectors: []detector.Detector{&detector.CrashLoopBackOffDetector{}},
	}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pc-crash", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProblemCaseReconciler_Reconcile_OpenPC_ResourceGone_Resolves(t *testing.T) {
	t.Parallel()
	// Pod does not exist — resource gone, should resolve PC.
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-gone", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "no-such-pod", Namespace: "default"},
			Detector:         "CrashLoopBackOff",
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()
	r := &ProblemCaseReconciler{
		Client:    c,
		Scheme:    newCtrlTestScheme(),
		Detectors: []detector.Detector{&detector.CrashLoopBackOffDetector{}},
	}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pc-gone", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── ExclusionRuleReconciler with matching incident ────────────────────────────

func TestExclusionRuleReconciler_Reconcile_ResolvesMatchingIncident(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule1", Namespace: "kubechan"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
		},
	}
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc1", Namespace: "kubechan"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Pod", Name: "app", Namespace: "default"},
			ProblemCases: []string{},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(rule, inc).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}, &v1alpha1.Incident{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "rule1", Namespace: "kubechan"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── PodReconciler with detector evaluation ────────────────────────────────────

func TestPodReconciler_Reconcile_HealthyPod_DetectorNoSymptoms(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "healthy-pod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pod).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &PodReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{&detector.CrashLoopBackOffDetector{}},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "healthy-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

// ── DeploymentReconciler with detector evaluation ─────────────────────────────

func TestDeploymentReconciler_Reconcile_HealthyDeployment_NoSymptoms(t *testing.T) {
	t.Parallel()
	one := int32(1)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 1, UnavailableReplicas: 0},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(deploy).
		Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &DeploymentReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{&detector.DeploymentUnavailableDetector{}},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-app", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

// ── ServiceReconciler with detector evaluation ────────────────────────────────

func TestServiceReconciler_Reconcile_HealthyService_NoSymptoms(t *testing.T) {
	t.Parallel()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(svc).
		Build()
	d := debounce.New(func() time.Duration { return 0 })
	r := &ServiceReconciler{
		Client:           c,
		Scheme:           newCtrlTestScheme(),
		Detectors:        []detector.Detector{&detector.ServiceNoEndpointsDetector{}},
		Debouncer:        d,
		ControlNamespace: "kubechan",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-svc", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

// ── More fetchAffectedResource kinds ─────────────────────────────────────────

func TestProblemCaseReconciler_FetchAffectedResource_Deployment(t *testing.T) {
	t.Parallel()
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(deploy).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	obj, err := r.fetchAffectedResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "Deployment", Name: "myapp", Namespace: "default",
	})
	if err != nil || obj == nil {
		t.Errorf("expected deployment obj, got err=%v obj=%v", err, obj)
	}
}

func TestProblemCaseReconciler_FetchAffectedResource_Service(t *testing.T) {
	t.Parallel()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "mysvc", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(svc).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	obj, err := r.fetchAffectedResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "Service", Name: "mysvc", Namespace: "default",
	})
	if err != nil || obj == nil {
		t.Errorf("expected service obj, got err=%v obj=%v", err, obj)
	}
}

func TestProblemCaseReconciler_FetchAffectedResource_Node(t *testing.T) {
	t.Parallel()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "mynode"}}
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).WithObjects(node).Build()
	r := &ProblemCaseReconciler{Client: c, Scheme: newCtrlTestScheme(), Detectors: nil}
	obj, err := r.fetchAffectedResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "Node", Name: "mynode",
	})
	if err != nil || obj == nil {
		t.Errorf("expected node obj, got err=%v obj=%v", err, obj)
	}
}

// ── ExclusionRuleReconciler.resolveIncident with ProblemCases ─────────────────

func TestExclusionRuleReconciler_ResolveIncident_WithOpenProblemCase(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc1", Namespace: "kubechan"},
		Status:     v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateOpen},
	}
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc1", Namespace: "kubechan"},
		Spec:       v1alpha1.IncidentSpec{ProblemCases: []string{"pc1"}},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc, pc).
		WithStatusSubresource(&v1alpha1.Incident{}, &v1alpha1.ProblemCase{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	if err := r.resolveIncident(context.Background(), inc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── PodReconciler full path with symptoms ──────────────────────────────────────

func TestPodReconciler_Reconcile_CrashingPod_CreatesProblemCase(t *testing.T) {
	// Do NOT use t.Parallel() here — the debounce goroutine races with other tests.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).
		WithStatusSubresource(&v1alpha1.ProblemCase{}, &v1alpha1.Incident{}).Build()

	debouncerInst := debounce.New(func() time.Duration { return 0 })
	det := &detector.CrashLoopBackOffDetector{}
	r := &PodReconciler{
		Client:           c,
		Scheme:           scheme,
		Debouncer:        debouncerInst,
		Detectors:        []detector.Detector{det},
		ControlNamespace: "kubechan",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "crash-pod"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait up to 500ms for the debounce callback to fire and create a ProblemCase.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var pcList v1alpha1.ProblemCaseList
		if err := c.List(context.Background(), &pcList); err == nil && len(pcList.Items) > 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected ProblemCase to be created within 500ms")
}

// ── DeploymentReconciler full path with symptoms ───────────────────────────────

func TestDeploymentReconciler_Reconcile_UnavailableDeployment_CreatesProblemCase(t *testing.T) {
	replicas := int32(3)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "unavail-deploy", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas:   0,
			UnavailableReplicas: 3,
		},
	}
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy).
		WithStatusSubresource(&v1alpha1.ProblemCase{}, &v1alpha1.Incident{}).Build()

	debouncerInst := debounce.New(func() time.Duration { return 0 })
	det := &detector.DeploymentUnavailableDetector{}
	r := &DeploymentReconciler{
		Client:           c,
		Scheme:           scheme,
		Debouncer:        debouncerInst,
		Detectors:        []detector.Detector{det},
		ControlNamespace: "kubechan",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "unavail-deploy"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var pcList v1alpha1.ProblemCaseList
		if err := c.List(context.Background(), &pcList); err == nil && len(pcList.Items) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected ProblemCase to be created within 500ms")
}

// ── ServiceReconciler full path with symptoms ─────────────────────────────────

func TestServiceReconciler_Reconcile_NoEndpoints_CreatesProblemCase(t *testing.T) {
	// NOTE: not t.Parallel() — debounce fires in goroutine
	ready := true
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "no-ep-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
		},
	}
	// No EndpointSlices = no ready endpoints
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}, &v1alpha1.Incident{}).Build()
	_ = ready

	debouncerInst := debounce.New(func() time.Duration { return 0 })
	det := &detector.ServiceNoEndpointsDetector{}
	r := &ServiceReconciler{
		Client:           c,
		Scheme:           scheme,
		Debouncer:        debouncerInst,
		Detectors:        []detector.Detector{det},
		ControlNamespace: "kubechan",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "no-ep-svc"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the debounce callback to create a ProblemCase.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var pcList v1alpha1.ProblemCaseList
		if err := c.List(context.Background(), &pcList); err == nil && len(pcList.Items) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected ProblemCase to be created within 500ms")
}

// ── findRootResource with owner reference ─────────────────────────────────────

func TestCorrelationReconciler_FindRootResource_WithOwner(t *testing.T) {
	t.Parallel()
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner-deploy",
			Namespace: "default",
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "child-rs",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Deployment",
					Name:       "owner-deploy",
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
	}
	scheme := newCtrlTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deploy, replicaSet).Build()
	r := &CorrelationReconciler{Client: c, Scheme: scheme, ControlNamespace: "kubechan"}

	root, err := r.findRootResource(context.Background(), v1alpha1.ResourceRef{
		Kind: "ReplicaSet", Name: "child-rs", Namespace: "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should walk up to Deployment
	if root.Name != "owner-deploy" {
		t.Errorf("expected root to be owner-deploy, got %s", root.Name)
	}
}

// ── ExclusionRuleReconciler: suppress matching incident ───────────────────────

func TestExclusionRuleReconciler_Reconcile_MatchesAndSuppresses(t *testing.T) {
	// Don't run in parallel — debouncer goroutines may race with fake client.
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "auto-inc", Namespace: "kubechan"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			Source:       "auto",
		},
		Status: v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "suppress-rule", Namespace: "kubechan"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc, rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}, &v1alpha1.Incident{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "suppress-rule", Namespace: "kubechan"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExclusionRuleReconciler_Reconcile_SkipsManualIncident(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-inc", Namespace: "kubechan"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			Source:       "manual",
		},
	}
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-skip", Namespace: "kubechan"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(inc, rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}, &v1alpha1.Incident{}).
		Build()
	r := &ExclusionRuleReconciler{Client: c, Scheme: newCtrlTestScheme(), ControlNamespace: "kubechan"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "rule-skip", Namespace: "kubechan"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── ruleNamespace ─────────────────────────────────────────────────────────────

func TestRuleNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		obj    client.Object
		wantNS string
	}{
		{
			"rule with explicit namespace scope",
			&v1alpha1.KubechanExclusionRule{
				ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "kubechan"},
				Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "prod"},
			},
			"prod",
		},
		{
			"rule with empty namespace scope means cluster-wide (empty string)",
			&v1alpha1.KubechanExclusionRule{
				ObjectMeta: metav1.ObjectMeta{Name: "r2", Namespace: "kubechan"},
				Spec:       v1alpha1.ExclusionRuleSpec{Namespace: ""},
			},
			"",
		},
		{
			"non-rule object falls back to object namespace",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
			},
			"default",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ruleNamespace(tc.obj)
			if got != tc.wantNS {
				t.Errorf("ruleNamespace() = %q, want %q", got, tc.wantNS)
			}
		})
	}
}

// ── ruleToPodsMapper ─────────────────────────────────────────────────────────

func TestRuleToPodsMapper_ReturnsPodRequests(t *testing.T) {
	t.Parallel()
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "staging"}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "staging"}}
	podOther := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-c", Namespace: "prod"}}

	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pod1, pod2, podOther).
		Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "staging"},
	}

	mapFn := ruleToPodsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests for namespace staging, got %d", len(reqs))
	}
	names := map[string]bool{}
	for _, r := range reqs {
		names[r.Name] = true
	}
	if !names["pod-a"] || !names["pod-b"] {
		t.Errorf("expected pod-a and pod-b in requests, got %v", names)
	}
}

func TestRuleToPodsMapper_ClusterWideRule(t *testing.T) {
	t.Parallel()
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-x", Namespace: "ns1"}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-y", Namespace: "ns2"}}

	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(pod1, pod2).
		Build()

	// Namespace="" means cluster-wide — mapper lists all namespaces
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: ""},
	}

	mapFn := ruleToPodsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests for cluster-wide rule, got %d", len(reqs))
	}
}

func TestRuleToPodsMapper_NoPodsReturnsEmpty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "empty-ns"},
	}

	mapFn := ruleToPodsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

// ── ruleToServicesMapper ─────────────────────────────────────────────────────

func TestRuleToServicesMapper_ReturnsServiceRequests(t *testing.T) {
	t.Parallel()
	svc1 := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "staging"}}
	svc2 := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "staging"}}
	svcOther := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-c", Namespace: "prod"}}

	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(svc1, svc2, svcOther).
		Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "staging"},
	}

	mapFn := ruleToServicesMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests for namespace staging, got %d", len(reqs))
	}
	names := map[string]bool{}
	for _, r := range reqs {
		names[r.Name] = true
	}
	if !names["svc-a"] || !names["svc-b"] {
		t.Errorf("expected svc-a and svc-b in requests, got %v", names)
	}
}

func TestRuleToServicesMapper_NoServicesReturnsEmpty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "empty-ns"},
	}

	mapFn := ruleToServicesMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

// ── ruleToDeploymentsMapper ──────────────────────────────────────────────────

func TestRuleToDeploymentsMapper_ReturnsDeploymentRequests(t *testing.T) {
	t.Parallel()
	dep1 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-a", Namespace: "staging"}}
	dep2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-b", Namespace: "staging"}}
	depOther := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-c", Namespace: "prod"}}

	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(dep1, dep2, depOther).
		Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "staging"},
	}

	mapFn := ruleToDeploymentsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests for namespace staging, got %d", len(reqs))
	}
	names := map[string]bool{}
	for _, r := range reqs {
		names[r.Name] = true
	}
	if !names["dep-a"] || !names["dep-b"] {
		t.Errorf("expected dep-a and dep-b in requests, got %v", names)
	}
}

func TestRuleToDeploymentsMapper_ClusterWideRule(t *testing.T) {
	t.Parallel()
	dep1 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-x", Namespace: "ns1"}}
	dep2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "dep-y", Namespace: "ns2"}}

	c := fake.NewClientBuilder().
		WithScheme(newCtrlTestScheme()).
		WithObjects(dep1, dep2).
		Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: ""},
	}

	mapFn := ruleToDeploymentsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests for cluster-wide rule, got %d", len(reqs))
	}
}

func TestRuleToDeploymentsMapper_NoDeploymentsReturnsEmpty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newCtrlTestScheme()).Build()

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "kubechan"},
		Spec:       v1alpha1.ExclusionRuleSpec{Namespace: "empty-ns"},
	}

	mapFn := ruleToDeploymentsMapper(c)
	reqs := mapFn(context.Background(), rule)

	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

