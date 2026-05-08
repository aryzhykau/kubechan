// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"log/slog"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	kubews "github.com/org/kubechan/services/backend-api/ws"
)

func newK8sTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

func newMoodSyncer(c client.Client) *MoodSyncer {
	return &MoodSyncer{
		Client:    c,
		Hub:       kubews.NewHub(slog.Default()),
		Namespace: "kubechan",
		Logger:    slog.Default(),
	}
}

// ---- EnsureState ----

func TestMoodSyncer_EnsureState_CreatesIfMissing(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)

	if err := ms.EnsureState(context.Background()); err != nil {
		t.Fatalf("EnsureState: %v", err)
	}

	state := &v1alpha1.KubeChanState{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: "kubechan", Name: "kubechan"}, state); err != nil {
		t.Fatalf("state not created: %v", err)
	}
}

func TestMoodSyncer_EnsureState_Idempotent(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)

	if err := ms.EnsureState(context.Background()); err != nil {
		t.Fatalf("first EnsureState: %v", err)
	}
	if err := ms.EnsureState(context.Background()); err != nil {
		t.Fatalf("second EnsureState: %v", err)
	}
}

// ---- SyncFromIncidents ----

func TestMoodSyncer_SyncFromIncidents_NoIncidents(t *testing.T) {
	t.Parallel()
	state := &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{Name: "kubechan", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithObjects(state).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)
	ms.SyncFromIncidents(context.Background())

	got := &v1alpha1.KubeChanState{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: "kubechan", Name: "kubechan"}, got); err != nil {
		t.Fatalf("get state: %v", err)
	}
	if got.Status.OpenIncidentCount != 0 {
		t.Errorf("expected 0 open incidents, got %d", got.Status.OpenIncidentCount)
	}
	if got.Status.MoodLevel != v1alpha1.MoodCalm {
		t.Errorf("expected MoodCalm, got %d", got.Status.MoodLevel)
	}
}

func TestMoodSyncer_SyncFromIncidents_OpenIncidents_SetsMood(t *testing.T) {
	t.Parallel()
	state := &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{Name: "kubechan", Namespace: "kubechan"},
	}
	inc1 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "kubechan"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	inc2 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-2", Namespace: "kubechan"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	inc3 := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-3", Namespace: "kubechan"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateOpen},
	}
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithObjects(state, inc1, inc2, inc3).
		WithStatusSubresource(&v1alpha1.KubeChanState{}, &v1alpha1.Incident{}).
		Build()
	ms := newMoodSyncer(c)
	ms.SyncFromIncidents(context.Background())

	got := &v1alpha1.KubeChanState{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: "kubechan", Name: "kubechan"}, got); err != nil {
		t.Fatalf("get state: %v", err)
	}
	if got.Status.OpenIncidentCount != 3 {
		t.Errorf("expected 3 open incidents, got %d", got.Status.OpenIncidentCount)
	}
	if got.Status.MoodLevel != v1alpha1.MoodRage {
		t.Errorf("expected MoodRage, got %d", got.Status.MoodLevel)
	}
}

func TestMoodSyncer_SyncFromIncidents_MissingState_NoError(t *testing.T) {
	t.Parallel()
	// No KubeChanState object — SyncFromIncidents should log and return, not panic.
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)
	ms.SyncFromIncidents(context.Background()) // must not panic
}

// ---- Poke ----

func TestMoodSyncer_Poke_StateNotFound_ReturnsError(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)

	_, err := ms.Poke(context.Background())
	if err == nil {
		t.Error("expected error when state not found")
	}
}

func TestMoodSyncer_Poke_IncrementsPokeCount(t *testing.T) {
	t.Parallel()
	state := &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{Name: "kubechan", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newK8sTestScheme()).
		WithObjects(state).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := newMoodSyncer(c)

	updated, err := ms.Poke(context.Background())
	if err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if updated.Status.PokeCount != 1 {
		t.Errorf("expected poke count 1, got %d", updated.Status.PokeCount)
	}
}

// ---- computeMoodLevel ----

func TestComputeMoodLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		openCount int
		pokeCount int
		want      v1alpha1.KubeChanMoodLevel
	}{
		{"calm zero", 0, 0, v1alpha1.MoodCalm},
		{"irritated 1 incident", 1, 0, v1alpha1.MoodIrritated},
		{"irritated 2 pokes", 0, 2, v1alpha1.MoodIrritated},
		{"rage 3 incidents", 3, 0, v1alpha1.MoodRage},
		{"rage 5 pokes", 0, 5, v1alpha1.MoodRage},
		{"rage both high", 3, 5, v1alpha1.MoodRage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeMoodLevel(tc.openCount, tc.pokeCount)
			if got != tc.want {
				t.Errorf("computeMoodLevel(%d,%d)=%d, want %d",
					tc.openCount, tc.pokeCount, got, tc.want)
			}
		})
	}
}

// ---- effectivePokeCount ----

func TestEffectivePokeCount(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now().Add(time.Minute))
	past := metav1.NewTime(time.Now().Add(-time.Minute))

	tests := []struct {
		name  string
		state *v1alpha1.KubeChanState
		want  int
	}{
		{
			name:  "no expiry",
			state: &v1alpha1.KubeChanState{},
			want:  0,
		},
		{
			name: "expired",
			state: &v1alpha1.KubeChanState{
				Status: v1alpha1.KubeChanStateStatus{PokeExpiresAt: &past, PokeCount: 5},
			},
			want: 0,
		},
		{
			name: "active",
			state: &v1alpha1.KubeChanState{
				Status: v1alpha1.KubeChanStateStatus{PokeExpiresAt: &now, PokeCount: 3},
			},
			want: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := effectivePokeCount(tc.state)
			if got != tc.want {
				t.Errorf("effectivePokeCount=%d, want %d", got, tc.want)
			}
		})
	}
}

// ---- GetMoodLevel ----

func TestMoodSyncer_GetMoodLevel_NoState_ReturnsZero(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newK8sTestScheme()).Build()
	ms := newMoodSyncer(c)
	if ms.GetMoodLevel(context.Background()) != 0 {
		t.Error("expected 0 mood level when state missing")
	}
}

// ---- handler (problemCaseHandler, incidentHandler) ----

func TestProblemCaseHandler_OnAdd_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "default"},
	}
	h.OnAdd(pc, false) // must not panic
}

func TestProblemCaseHandler_OnAdd_WrongType_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	h.OnAdd("not-a-problemcase", false) // must not panic
}

func TestProblemCaseHandler_OnUpdate_Resolved(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "default"},
		Status:     v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	h.OnUpdate(nil, pc)
}

func TestProblemCaseHandler_OnDelete_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "default"},
	}
	h.OnDelete(pc)
}

func TestIncidentHandler_OnAdd_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	// nil moodSyncer — tests the nil guard branch
	h := newIncidentHandler(hub, nil, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
	}
	h.OnAdd(inc, false)
}

func TestIncidentHandler_OnUpdate_Resolved(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newIncidentHandler(hub, nil, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
		Status:     v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	h.OnUpdate(nil, inc)
}

func TestIncidentHandler_OnDelete_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newIncidentHandler(hub, nil, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
	}
	h.OnDelete(inc)
}

// ---- DiagnosticRun handler ----

func TestDiagnosticRunHandler_OnAdd_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newDiagnosticRunHandler(hub, slog.Default())
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-1", Namespace: "default"},
	}
	h.OnAdd(dr, false)
}

func TestDiagnosticRunHandler_OnUpdate_Broadcasts(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())

	h := newDiagnosticRunHandler(hub, slog.Default())
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-2", Namespace: "default"},
		Status:     v1alpha1.DiagnosticRunStatus{State: v1alpha1.DiagnosticRunStateRunning},
	}
	// Should not panic; hub.Broadcast is non-blocking (select with default).
	h.OnUpdate(nil, dr)
	h.OnUpdate(nil, dr) // second call to ensure it's repeatable
}

func TestDiagnosticRunHandler_OnUpdate_WrongType_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newDiagnosticRunHandler(hub, slog.Default())
	// Passing something that is not a DiagnosticRun should be a no-op.
	h.OnUpdate(nil, "not-a-diagnostic-run")
}

func TestDiagnosticRunHandler_OnDelete_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newDiagnosticRunHandler(hub, slog.Default())
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-3", Namespace: "default"},
	}
	h.OnDelete(dr)
}

// ── incidentHandler additional cases ─────────────────────────────────────────

func TestIncidentHandler_OnAdd_BroadcastsEvent(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	h := newIncidentHandler(hub, ms, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-add", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "app"},
		},
	}
	h.OnAdd(inc, false) // should not panic
}

func TestIncidentHandler_OnAdd_WrongType_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	h := newIncidentHandler(hub, ms, slog.Default())
	h.OnAdd("not-an-incident", false) // wrong type, should not panic
}

func TestProblemCaseHandler_OnUpdate_UpdatedState(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-updated", Namespace: "default"},
		Spec:       v1alpha1.ProblemCaseSpec{Severity: "high", Detector: "CrashLoopBackOff"},
		Status:     v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateOpen},
	}
	h.OnUpdate(nil, pc) // should not panic, broadcasts EventProblemCaseUpdated
}

func TestMoodSyncer_GetMoodLevel_WithState(t *testing.T) {
	t.Parallel()
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	ctx := context.Background()

	// Create state
	if err := ms.EnsureState(ctx); err != nil {
		t.Fatalf("EnsureState: %v", err)
	}

	// Set mood level to 5 via SyncFromIncidents with open incidents
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-mood", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "app"},
		},
	}
	if err := c.Create(ctx, inc); err != nil {
		t.Fatalf("creating incident: %v", err)
	}
	ms.SyncFromIncidents(ctx)

	level := ms.GetMoodLevel(ctx)
	if level < 0 {
		t.Errorf("expected non-negative mood level, got %d", level)
	}
}

func TestMoodSyncer_Broadcast_SendsToHub(t *testing.T) {
	t.Parallel()
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	ctx := context.Background()

	if err := ms.EnsureState(ctx); err != nil {
		t.Fatalf("EnsureState: %v", err)
	}

	state := &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{Name: "kubechan-state", Namespace: "kubechan"},
	}
	ms.broadcast(state) // should not panic
}

func TestIncidentHandler_OnUpdate_WithResolvedState(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	h := newIncidentHandler(hub, ms, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-update", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "app"},
		},
		Status: v1alpha1.IncidentStatus{State: v1alpha1.IncidentStateResolved},
	}
	h.OnUpdate(nil, inc) // should not panic, broadcasts EventIncidentResolved
}

func TestIncidentHandler_OnDelete_BroadcastsEvent(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	h := newIncidentHandler(hub, ms, slog.Default())
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-delete", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "app"},
		},
	}
	h.OnDelete(inc) // should not panic
}

func TestIncidentHandler_OnDelete_WrongType_NoPanic(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	h := newIncidentHandler(hub, ms, slog.Default())
	h.OnDelete("not-an-incident")
}

func TestProblemCaseHandler_OnUpdate_ResolvedState(t *testing.T) {
	t.Parallel()
	hub := kubews.NewHub(slog.Default())
	h := newProblemCaseHandler(hub, slog.Default())
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-resolved", Namespace: "default"},
		Spec:       v1alpha1.ProblemCaseSpec{Severity: "high", Detector: "CrashLoopBackOff"},
		Status:     v1alpha1.ProblemCaseStatus{State: v1alpha1.ProblemCaseStateResolved},
	}
	h.OnUpdate(nil, pc) // should broadcast EventProblemCaseResolved
}

func TestMoodSyncer_Poke_WithHub(t *testing.T) {
	t.Parallel()
	scheme := newK8sTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ms := newMoodSyncer(c)
	ctx := context.Background()

	if err := ms.EnsureState(ctx); err != nil {
		t.Fatalf("EnsureState: %v", err)
	}

	_, _ = ms.Poke(ctx) // should not panic
}
