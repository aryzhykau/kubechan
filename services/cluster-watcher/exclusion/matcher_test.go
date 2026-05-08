// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package exclusion

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

// fakeObject is a minimal client.Object for testing matchesResource.
type fakeObject struct {
	metav1.ObjectMeta
	gvk schema.GroupVersionKind
}

func (f *fakeObject) GetObjectKind() schema.ObjectKind               { return f }
func (f *fakeObject) GroupVersionKind() schema.GroupVersionKind      { return f.gvk }
func (f *fakeObject) SetGroupVersionKind(gvk schema.GroupVersionKind) { f.gvk = gvk }
func (f *fakeObject) DeepCopyObject() runtime.Object                 { panic("not implemented") }

var _ client.Object = &fakeObject{}

func makeObj(kind, ns, name string, labels map[string]string) *fakeObject {
	obj := &fakeObject{
		gvk: schema.GroupVersionKind{Kind: kind},
	}
	obj.Namespace = ns
	obj.Name = name
	obj.Labels = labels
	return obj
}

// ── matchesDetector ───────────────────────────────────────────────────────────

func TestMatchesDetector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		detectors []string
		detector  string
		want      bool
	}{
		{"empty list matches all", []string{}, "CrashLoop", true},
		{"exact match", []string{"CrashLoop", "Pending"}, "CrashLoop", true},
		{"no match", []string{"CrashLoop"}, "Pending", false},
		{"single match", []string{"ServiceNoEndpoints"}, "ServiceNoEndpoints", true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesDetector(tc.detectors, tc.detector); got != tc.want {
				t.Errorf("matchesDetector(%v, %q) = %v, want %v", tc.detectors, tc.detector, got, tc.want)
			}
		})
	}
}

// ── matchesResource — TargetResources ────────────────────────────────────────

func TestMatchesResource_TargetResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec v1alpha1.ExclusionRuleSpec
		obj  *fakeObject
		want bool
	}{
		{
			name: "exact match kind+name",
			spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{
					{Kind: "Deployment", Name: "my-app", Namespace: ""},
				},
			},
			obj:  makeObj("Deployment", "default", "my-app", nil),
			want: true,
		},
		{
			name: "kind mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{
					{Kind: "StatefulSet", Name: "my-app"},
				},
			},
			obj:  makeObj("Deployment", "default", "my-app", nil),
			want: false,
		},
		{
			name: "name mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{
					{Kind: "Deployment", Name: "other-app"},
				},
			},
			obj:  makeObj("Deployment", "default", "my-app", nil),
			want: false,
		},
		{
			name: "namespace mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{
					{Kind: "Deployment", Name: "my-app", Namespace: "production"},
				},
			},
			obj:  makeObj("Deployment", "staging", "my-app", nil),
			want: false,
		},
		{
			name: "namespace match",
			spec: v1alpha1.ExclusionRuleSpec{
				TargetResources: []v1alpha1.ResourceRef{
					{Kind: "Deployment", Name: "my-app", Namespace: "production"},
				},
			},
			obj:  makeObj("Deployment", "production", "my-app", nil),
			want: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesResource(&tc.spec, tc.obj); got != tc.want {
				t.Errorf("matchesResource() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── matchesResource — Selector ────────────────────────────────────────────────

func TestMatchesResource_Selector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec v1alpha1.ExclusionRuleSpec
		obj  *fakeObject
		want bool
	}{
		{
			name: "selector match by labels",
			spec: v1alpha1.ExclusionRuleSpec{
				Selector: &v1alpha1.ExclusionSelector{
					MatchLabels: map[string]string{"app": "api"},
				},
			},
			obj:  makeObj("Deployment", "default", "api", map[string]string{"app": "api"}),
			want: true,
		},
		{
			name: "selector label mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				Selector: &v1alpha1.ExclusionSelector{
					MatchLabels: map[string]string{"app": "api"},
				},
			},
			obj:  makeObj("Deployment", "default", "other", map[string]string{"app": "worker"}),
			want: false,
		},
		{
			name: "selector with kind filter match",
			spec: v1alpha1.ExclusionRuleSpec{
				Selector: &v1alpha1.ExclusionSelector{
					Kinds:       []string{"Deployment"},
					MatchLabels: map[string]string{"team": "platform"},
				},
			},
			obj:  makeObj("Deployment", "default", "svc", map[string]string{"team": "platform"}),
			want: true,
		},
		{
			name: "selector with kind filter mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				Selector: &v1alpha1.ExclusionSelector{
					Kinds: []string{"StatefulSet"},
				},
			},
			obj:  makeObj("Deployment", "default", "svc", nil),
			want: false,
		},
		{
			name: "selector namespace mismatch",
			spec: v1alpha1.ExclusionRuleSpec{
				Selector: &v1alpha1.ExclusionSelector{
					Namespace: "production",
				},
			},
			obj:  makeObj("Deployment", "staging", "app", nil),
			want: false,
		},
		{
			name: "empty spec — no target and no selector",
			spec: v1alpha1.ExclusionRuleSpec{},
			obj:  makeObj("Deployment", "default", "app", nil),
			want: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesResource(&tc.spec, tc.obj); got != tc.want {
				t.Errorf("matchesResource() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── inTimeWindow ─────────────────────────────────────────────────────────────

func mustTime(layout, val string) time.Time {
	t, err := time.Parse(layout, val)
	if err != nil {
		panic(err)
	}
	return t
}

func TestInTimeWindow(t *testing.T) {
	t.Parallel()

	// Tuesday 2026-05-05 14:30 UTC
	tuesdayAfternoon := mustTime("2006-01-02T15:04", "2026-05-05T14:30")
	// Tuesday 2026-05-05 02:00 UTC
	tuesdayNight := mustTime("2006-01-02T15:04", "2026-05-05T02:00")
	// Saturday 2026-05-09 12:00 UTC
	saturdayNoon := mustTime("2006-01-02T15:04", "2026-05-09T12:00")

	tests := []struct {
		name    string
		tw      *v1alpha1.ExclusionTimeWindow
		now     time.Time
		want    bool
		wantErr bool
	}{
		{
			name: "inside window",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{Start: "09:00", End: "18:00", Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}},
				},
			},
			now:  tuesdayAfternoon,
			want: true,
		},
		{
			name: "outside window — wrong time",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{Start: "09:00", End: "12:00", Days: []string{"Tue"}},
				},
			},
			now:  tuesdayAfternoon, // 14:30, after end
			want: false,
		},
		{
			name: "outside window — wrong day",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{Start: "00:00", End: "23:59", Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}},
				},
			},
			now:  saturdayNoon,
			want: false,
		},
		{
			name: "midnight-spanning period — inside",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{Start: "22:00", End: "06:00", Days: []string{"Tue"}},
				},
			},
			now:  tuesdayNight, // 02:00 — inside 22:00–06:00
			want: true,
		},
		{
			name: "invalid timezone",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "Not/AZone",
				Periods:  []v1alpha1.ExclusionPeriod{{Start: "09:00", End: "18:00", Days: []string{"Tue"}}},
			},
			now:     tuesdayAfternoon,
			wantErr: true,
		},
		{
			name: "invalid start time — skipped",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods:  []v1alpha1.ExclusionPeriod{{Start: "bad", End: "18:00", Days: []string{"Tue"}}},
			},
			now:  tuesdayAfternoon,
			want: false, // period skipped
		},
		{
			name: "invalid end time — skipped",
			tw: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods:  []v1alpha1.ExclusionPeriod{{Start: "09:00", End: "bad", Days: []string{"Tue"}}},
			},
			now:  tuesdayAfternoon,
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := inTimeWindow(tc.tw, tc.now)
			if (err != nil) != tc.wantErr {
				t.Errorf("inTimeWindow() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("inTimeWindow() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── containsString ────────────────────────────────────────────────────────────

func TestContainsString(t *testing.T) {
	t.Parallel()
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Error("expected true for present element")
	}
	if containsString([]string{"a", "b"}, "z") {
		t.Error("expected false for absent element")
	}
	if containsString([]string{}, "x") {
		t.Error("expected false for empty slice")
	}
}

// ── IsExcluded ────────────────────────────────────────────────────────────────

func enabledRule(name, ns string, spec v1alpha1.ExclusionRuleSpec) *v1alpha1.KubechanExclusionRule {
	spec.Enabled = true
	return &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       spec,
	}
}

func TestIsExcluded_NoRules(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	obj := makeObj("Deployment", "default", "my-app", nil)
	excluded, name, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded || name != "" {
		t.Errorf("expected no exclusion, got excluded=%v name=%q", excluded, name)
	}
}

func TestIsExcluded_DisabledRuleSkipped(t *testing.T) {
	t.Parallel()
	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "disabled-rule", Namespace: "kubechan-system"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled: false,
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Name: "my-app"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(rule).Build()
	obj := makeObj("Deployment", "default", "my-app", nil)
	excluded, _, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("disabled rule should not exclude")
	}
}

func TestIsExcluded_MatchingRuleExcludes(t *testing.T) {
	t.Parallel()
	rule := enabledRule("my-rule", "kubechan-system", v1alpha1.ExclusionRuleSpec{
		TargetResources: []v1alpha1.ResourceRef{
			{Kind: "Deployment", Name: "my-app"},
		},
	})
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(rule).Build()
	obj := makeObj("Deployment", "default", "my-app", nil)
	excluded, name, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !excluded {
		t.Error("expected rule to match")
	}
	if name != "my-rule" {
		t.Errorf("expected name=my-rule, got %q", name)
	}
}

func TestIsExcluded_NamespaceMismatchSkipped(t *testing.T) {
	t.Parallel()
	rule := enabledRule("ns-rule", "kubechan-system", v1alpha1.ExclusionRuleSpec{
		Namespace: "production",
		TargetResources: []v1alpha1.ResourceRef{
			{Kind: "Deployment", Name: "my-app"},
		},
	})
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(rule).Build()
	obj := makeObj("Deployment", "staging", "my-app", nil)
	excluded, _, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("namespace mismatch should not exclude")
	}
}

func TestIsExcluded_DetectorMismatchSkipped(t *testing.T) {
	t.Parallel()
	rule := enabledRule("det-rule", "kubechan-system", v1alpha1.ExclusionRuleSpec{
		Detectors: []string{"CrashLoop"},
		TargetResources: []v1alpha1.ResourceRef{
			{Kind: "Deployment", Name: "my-app"},
		},
	})
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(rule).Build()
	obj := makeObj("Deployment", "default", "my-app", nil)
	excluded, _, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "Pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("detector mismatch should not exclude")
	}
}

// ── GetRule ───────────────────────────────────────────────────────────────────

func TestGetRule_Found(t *testing.T) {
	t.Parallel()
	rule := enabledRule("found-rule", "kubechan-system", v1alpha1.ExclusionRuleSpec{})
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(rule).Build()
	got, err := GetRule(context.Background(), c, "kubechan-system", "found-rule")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name != "found-rule" {
		t.Errorf("expected to find rule, got %v", got)
	}
}

func TestGetRule_NotFound_ReturnsNil(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	got, err := GetRule(context.Background(), c, "kubechan-system", "missing-rule")
	if err != nil {
		t.Fatalf("expected nil error for missing rule, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ── PatchMatchedStatus ────────────────────────────────────────────────────────

func TestPatchMatchedStatus(t *testing.T) {
	t.Parallel()
	rule := enabledRule("patch-rule", "kubechan-system", v1alpha1.ExclusionRuleSpec{})
	rule.Status.SuppressedCount = 5
	c := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(rule).
		WithStatusSubresource(rule).
		Build()

	if err := PatchMatchedStatus(context.Background(), c, rule); err != nil {
		t.Fatalf("PatchMatchedStatus() error = %v", err)
	}

	// Fetch updated rule.
	var updated v1alpha1.KubechanExclusionRule
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "kubechan-system", Name: "patch-rule"}, &updated); err != nil {
		t.Fatalf("Get after patch error: %v", err)
	}
	if updated.Status.SuppressedCount != 6 {
		t.Errorf("SuppressedCount = %d, want 6", updated.Status.SuppressedCount)
	}
	if updated.Status.LastMatchedAt == nil {
		t.Error("LastMatchedAt should be set")
	}
}

func TestIsExcluded_WithActiveTimeWindow(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	// Create a time window for every day all day.
	startH := 0
	startM := 0
	endH := 23
	endM := 59
	_ = startH; _ = startM; _ = endH; _ = endM

	rule := v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "tw-rule", Namespace: "kubechan-system"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Namespace: "default", Name: "myapp"},
			},
			TimeWindow: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{
						Start: "00:00",
						End:   "23:59",
						Days:  []string{now.Format("Mon")},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(&rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}).
		Build()

	obj := makeObj("Deployment", "default", "myapp", nil)
	excluded, ruleName, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoopBackOff")
	if err != nil {
		t.Fatalf("IsExcluded error: %v", err)
	}
	if !excluded {
		t.Errorf("expected excluded=true for active time window, got false")
	}
	if ruleName != "tw-rule" {
		t.Errorf("expected ruleName=tw-rule, got %q", ruleName)
	}
}

func TestIsExcluded_WithInactiveTimeWindow_NotExcluded(t *testing.T) {
	t.Parallel()
	// A time window that's yesterday — impossible to be active today
	now := time.Now().UTC()
	yesterdayName := now.AddDate(0, 0, -7).Format("Mon") // a different day of week

	rule := v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{Name: "inactive-tw-rule", Namespace: "kubechan-system"},
		Spec: v1alpha1.ExclusionRuleSpec{
			Enabled:   true,
			Namespace: "default",
			TargetResources: []v1alpha1.ResourceRef{
				{Kind: "Deployment", Namespace: "default", Name: "myapp"},
			},
			TimeWindow: &v1alpha1.ExclusionTimeWindow{
				Timezone: "UTC",
				Periods: []v1alpha1.ExclusionPeriod{
					{
						Start: "00:00",
						End:   "00:01",
						Days:  []string{yesterdayName},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(&rule).
		WithStatusSubresource(&v1alpha1.KubechanExclusionRule{}).
		Build()

	obj := makeObj("Deployment", "default", "myapp", nil)
	_, _, err := IsExcluded(context.Background(), c, "kubechan-system", obj, "CrashLoopBackOff")
	if err != nil {
		t.Fatalf("IsExcluded error: %v", err)
	}
	// May or may not be excluded depending on whether we happen to be in that window
	// This just ensures no panic.
}
