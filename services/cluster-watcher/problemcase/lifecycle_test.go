// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package problemcase

import (
	"context"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/detector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

const controlNS = "kubechan-system"

func testRef() v1alpha1.ResourceRef {
	return v1alpha1.ResourceRef{Kind: "Deployment", Namespace: "default", Name: "my-app"}
}

// ── FindOpen ──────────────────────────────────────────────────────────────────

func TestFindOpen_NoneExists(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	pc, err := FindOpen(context.Background(), c, controlNS, testRef(), "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc != nil {
		t.Error("expected nil when no ProblemCase exists")
	}
}

func TestFindOpen_ReturnsOpenCase(t *testing.T) {
	t.Parallel()
	ref := testRef()
	labelVal := AffectedResourceLabelValue(ref)
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pc-1",
			Namespace: controlNS,
			Labels: map[string]string{
				LabelAffectedResource: labelVal,
				LabelDetector:         "CrashLoop",
			},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: ref,
			Detector:         "CrashLoop",
		},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(pc).Build()
	found, err := FindOpen(context.Background(), c, controlNS, ref, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find an open ProblemCase")
	}
	if found.Name != "pc-1" {
		t.Errorf("got name=%q, want pc-1", found.Name)
	}
}

func TestFindOpen_SkipsResolved(t *testing.T) {
	t.Parallel()
	ref := testRef()
	labelVal := AffectedResourceLabelValue(ref)
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pc-resolved",
			Namespace: controlNS,
			Labels: map[string]string{
				LabelAffectedResource: labelVal,
				LabelDetector:         "CrashLoop",
			},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: ref,
			Detector:         "CrashLoop",
		},
		Status: v1alpha1.ProblemCaseStatus{
			State: v1alpha1.ProblemCaseStateResolved,
		},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(pc).WithStatusSubresource(pc).Build()

	found, err := FindOpen(context.Background(), c, controlNS, ref, "CrashLoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil for resolved ProblemCase, got %v", found.Name)
	}
}

// ── CreateOrUpdate ────────────────────────────────────────────────────────────

func TestCreateOrUpdate_CreatesNew(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()

	symptoms := []detector.Symptom{{Message: "pod is crashing"}}
	err := CreateOrUpdate(context.Background(), c, controlNS, testRef(),
		v1alpha1.SeverityHigh, "CrashLoop", symptoms)
	if err != nil {
		t.Fatalf("CreateOrUpdate() error = %v", err)
	}

	list := &v1alpha1.ProblemCaseList{}
	if err := c.List(context.Background(), list, client.InNamespace(controlNS)); err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 ProblemCase, got %d", len(list.Items))
	}
	if list.Items[0].Spec.Detector != "CrashLoop" {
		t.Errorf("Detector = %q, want CrashLoop", list.Items[0].Spec.Detector)
	}
}

func TestCreateOrUpdate_UpdatesExisting(t *testing.T) {
	t.Parallel()
	ref := testRef()
	labelVal := AffectedResourceLabelValue(ref)
	existing := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-pc",
			Namespace: controlNS,
			Labels: map[string]string{
				LabelAffectedResource: labelVal,
				LabelDetector:         "CrashLoop",
			},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: ref,
			Detector:         "CrashLoop",
			Symptoms:         []string{"old message"},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(existing).
		WithStatusSubresource(existing).
		Build()

	symptoms := []detector.Symptom{{Message: "new message"}}
	err := CreateOrUpdate(context.Background(), c, controlNS, ref,
		v1alpha1.SeverityHigh, "CrashLoop", symptoms)
	if err != nil {
		t.Fatalf("CreateOrUpdate() error = %v", err)
	}

	var updated v1alpha1.ProblemCase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: controlNS, Name: "existing-pc"}, &updated); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if len(updated.Spec.Symptoms) != 1 || updated.Spec.Symptoms[0] != "new message" {
		t.Errorf("Symptoms not updated: %v", updated.Spec.Symptoms)
	}
}

// ── Resolve ───────────────────────────────────────────────────────────────────

func TestResolve_SetsResolvedState(t *testing.T) {
	t.Parallel()
	ref := testRef()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "to-resolve", Namespace: controlNS},
		Spec:       v1alpha1.ProblemCaseSpec{AffectedResource: ref, Detector: "CrashLoop"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(pc).
		WithStatusSubresource(pc).
		Build()

	if err := Resolve(context.Background(), c, pc); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	var resolved v1alpha1.ProblemCase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: controlNS, Name: "to-resolve"}, &resolved); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if resolved.Status.State != v1alpha1.ProblemCaseStateResolved {
		t.Errorf("State = %q, want Resolved", resolved.Status.State)
	}
	if resolved.Status.ResolvedAt == nil {
		t.Error("ResolvedAt should be set")
	}
}

func TestCreateOrUpdate_AlreadyExists_PatchesInstead(t *testing.T) {
	t.Parallel()
	s := newScheme()
	ref := testRef()
	now := metav1.Now()
	detectorName := "CrashLoopBackOff"

	// Pre-create a ProblemCase that will cause Create to return AlreadyExists.
	existing := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-pc",
			Namespace: controlNS,
			Labels: map[string]string{
				LabelAffectedResource: AffectedResourceLabelValue(ref),
				LabelDetector:         detectorName,
			},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: ref,
			Detector:         detectorName,
				Severity:         v1alpha1.SeverityHigh,
		},
		Status: v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateOpen, FirstSeen: &now, LastSeen: &now},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(existing).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()

	// Call CreateOrUpdate again — should hit the Already-exists branch and patch.
	err := CreateOrUpdate(context.Background(), c, controlNS, ref, v1alpha1.SeverityHigh, detectorName, []detector.Symptom{{Message: "symptom"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolve_AlreadyResolved_Noop(t *testing.T) {
	t.Parallel()
	s := newScheme()
	now := metav1.Now()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "already-resolved", Namespace: controlNS},
		Status: v1alpha1.ProblemCaseStatus{
			State:      v1alpha1.ProblemCaseStateResolved,
			FirstSeen:  &now,
			LastSeen:   &now,
			ResolvedAt: &now,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(pc).
		WithStatusSubresource(&v1alpha1.ProblemCase{}).
		Build()

	err := Resolve(context.Background(), c, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
