// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/diagnostics-worker/collector"
)

func newDWTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

func newSlogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

// ── summariseDeploymentConditions ────────────────────────────────────────────

func TestSummariseDeploymentConditions_Empty(t *testing.T) {
	t.Parallel()
	result := summariseDeploymentConditions(nil)
	if result != nil {
		t.Errorf("expected nil for empty conditions, got %v", result)
	}
}

func TestSummariseDeploymentConditions_WithConditions(t *testing.T) {
	t.Parallel()
	conds := []appsv1.DeploymentCondition{
		{
			Type:    appsv1.DeploymentAvailable,
			Status:  corev1.ConditionTrue,
			Reason:  "MinimumReplicasAvailable",
			Message: "Deployment has minimum availability",
		},
		{
			Type:    appsv1.DeploymentProgressing,
			Status:  corev1.ConditionFalse,
			Reason:  "ProgressDeadlineExceeded",
			Message: "Deployment timed out",
		},
	}
	result := summariseDeploymentConditions(conds)
	if len(result) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(result))
	}
	if result[0]["type"] != string(appsv1.DeploymentAvailable) {
		t.Errorf("condition[0].type = %v, want %v", result[0]["type"], appsv1.DeploymentAvailable)
	}
	if result[1]["reason"] != "ProgressDeadlineExceeded" {
		t.Errorf("condition[1].reason = %v, want ProgressDeadlineExceeded", result[1]["reason"])
	}
}

// ── appendUnique ──────────────────────────────────────────────────────────────

func TestAppendUnique_AddsNew(t *testing.T) {
	t.Parallel()
	result := appendUnique([]string{"a", "b"}, "c")
	if len(result) != 3 || result[2] != "c" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestAppendUnique_SkipsDuplicate(t *testing.T) {
	t.Parallel()
	result := appendUnique([]string{"a", "b"}, "a")
	if len(result) != 2 {
		t.Errorf("expected 2 elements, got %d: %v", len(result), result)
	}
}

func TestAppendUnique_EmptySlice(t *testing.T) {
	t.Parallel()
	result := appendUnique(nil, "x")
	if len(result) != 1 || result[0] != "x" {
		t.Errorf("unexpected result: %v", result)
	}
}

// ── podBelongsToWorkload ──────────────────────────────────────────────────────

func TestPodBelongsToWorkload_ByOwnerReference(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "myapp"},
			},
		},
	}
	if !podBelongsToWorkload(pod, "myapp") {
		t.Error("expected pod to belong to workload via owner reference")
	}
}

func TestPodBelongsToWorkload_ByLabel_App(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app": "myapp"},
		},
	}
	if !podBelongsToWorkload(pod, "myapp") {
		t.Error("expected pod to belong to workload via 'app' label")
	}
}

func TestPodBelongsToWorkload_ByLabel_KubernetesName(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app.kubernetes.io/name": "myapp"},
		},
	}
	if !podBelongsToWorkload(pod, "myapp") {
		t.Error("expected pod to belong to workload via k8s name label")
	}
}

func TestPodBelongsToWorkload_NoMatch(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app": "otherapp"},
		},
	}
	if podBelongsToWorkload(pod, "myapp") {
		t.Error("expected pod not to belong to workload")
	}
}

// ── Reconcile early exit paths ────────────────────────────────────────────────

func TestDiagnosticRunReconciler_Reconcile_NotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().
		WithScheme(newDWTestScheme()).
		Build()
	r := &DiagnosticRunReconciler{
		Client:           c,
		Logger:           newSlogDiscard(),
		BackendAPIURL:    "http://localhost",
		CollectorVersion: "test",
	}
	result, err := r.Reconcile(context.Background(), ctrl.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not-found DR")
	}
}

func TestDiagnosticRunReconciler_Reconcile_NotPending_Skipped(t *testing.T) {
	t.Parallel()
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-completed", Namespace: "kubechan"},
		Status:     v1alpha1.DiagnosticRunStatus{State: v1alpha1.DiagnosticRunStateCompleted},
	}
	c := fake.NewClientBuilder().
		WithScheme(newDWTestScheme()).
		WithObjects(dr).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()
	r := &DiagnosticRunReconciler{
		Client:           c,
		Logger:           newSlogDiscard(),
		BackendAPIURL:    "http://localhost",
		CollectorVersion: "test",
	}
	req := ctrl.Request{}
	req.Name = "dr-completed"
	req.Namespace = "kubechan"
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for completed DR")
	}
}

// ── collectResourceSpec ────────────────────────────────────────────────────────

func TestCollectResourceSpec_Deployment(t *testing.T) {
	t.Parallel()
	one := int32(1)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "myapp"}},
		},
		Status: appsv1.DeploymentStatus{AvailableReplicas: 1},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(deploy).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "Deployment", "myapp", "apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_Service(t *testing.T) {
	t.Parallel()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "mysvc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.0.0.1"},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(svc).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "Service", "mysvc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_ConfigMap(t *testing.T) {
	t.Parallel()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mycm", Namespace: "default"},
		Data:       map[string]string{"key": "value"},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(cm).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "ConfigMap", "mycm", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_PVC(t *testing.T) {
	t.Parallel()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "mypvc", Namespace: "default"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: func() *string { s := "standard"; return &s }()},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(pvc).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "PersistentVolumeClaim", "mypvc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_UnknownKind_NoAPIGroup(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "SomeCustomKind", "obj", "")
	// Unknown kind with no apiGroup should return nil, nil
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = spec
}

// ── collectEvents ─────────────────────────────────────────────────────────────

func TestCollectEvents_NoEvents(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	events, err := r.collectEvents(context.Background(), "default", "Pod", "mypod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestCollectEvents_WithMatchingEvent(t *testing.T) {
	t.Parallel()
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "mypod"},
		Reason:         "BackOff",
		Type:           "Warning",
		Count:          3,
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(ev).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	events, err := r.collectEvents(context.Background(), "default", "Pod", "mypod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if events[0].Reason != "BackOff" {
		t.Errorf("unexpected reason: %s", events[0].Reason)
	}
}

func TestCollectEvents_FiltersUnrelated(t *testing.T) {
	t.Parallel()
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "ev2", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "other-app"},
		Reason:         "Scaled",
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(ev).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	events, err := r.collectEvents(context.Background(), "default", "Pod", "mypod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events (unrelated), got %d", len(events))
	}
}

// ── postEvidence ──────────────────────────────────────────────────────────────

func TestPostEvidence_Success(t *testing.T) {
	t.Parallel()
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{
		Client:           c,
		Logger:           newSlogDiscard(),
		BackendAPIURL:    srv.URL,
		CollectorVersion: "v1.0.0",
	}
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-1", Namespace: "kubechan"},
		Spec:       v1alpha1.DiagnosticRunSpec{IncidentRef: "inc-1"},
	}
	ev := &collector.Evidence{CollectedAt: metav1.Now().Time}
	err := r.postEvidence(context.Background(), dr, ev, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-received:
	default:
		t.Error("expected HTTP request to be received")
	}
}

func TestPostEvidence_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{
		Client:           c,
		Logger:           newSlogDiscard(),
		BackendAPIURL:    srv.URL,
		CollectorVersion: "v1.0.0",
	}
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-2", Namespace: "kubechan"},
		Spec:       v1alpha1.DiagnosticRunSpec{IncidentRef: "inc-2"},
	}
	ev := &collector.Evidence{CollectedAt: metav1.Now().Time}
	err := r.postEvidence(context.Background(), dr, ev, nil)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

// ── collectPodDependencies ────────────────────────────────────────────────────

func TestCollectPodDependencies_NoDependencies(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Spec:       corev1.PodSpec{},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	deps := r.collectPodDependencies(context.Background(), "default", pod)
	if deps != nil {
		t.Errorf("expected nil deps for pod with no dependencies, got %+v", deps)
	}
}

func TestCollectPodDependencies_WithConfigMap(t *testing.T) {
	t.Parallel()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mycm", Namespace: "default"},
		Data:       map[string]string{"key": "val"},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "config-vol", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "mycm"},
					},
				}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(cm).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	deps := r.collectPodDependencies(context.Background(), "default", pod)
	if deps == nil {
		t.Fatal("expected non-nil deps")
	}
	if len(deps.ConfigMaps) == 0 {
		t.Error("expected at least one ConfigMap ref")
	}
}

// ── collect ───────────────────────────────────────────────────────────────────

func TestCollect_IncidentNotFound(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	ev, errs := r.collect(context.Background(), "default", "nonexistent-inc", "dr-1")
	if len(errs) == 0 {
		t.Error("expected error when incident not found")
	}
	if ev == nil {
		t.Error("expected non-nil evidence even on error")
	}
}

func TestCollect_WithIncidentNoProblemCases(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-1", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			ProblemCases: []string{},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(inc).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	ev, errs := r.collect(context.Background(), "default", "inc-1", "dr-1")
	var nonLogErrs []string
	for _, e := range errs {
		if !strings.Contains(e, "pod logs") {
			nonLogErrs = append(nonLogErrs, e)
		}
	}
	if len(nonLogErrs) != 0 {
		t.Errorf("expected no non-podlog errors, got: %v", nonLogErrs)
	}
	if ev.IncidentID != "inc-1" {
		t.Errorf("expected incidentID=inc-1, got %s", ev.IncidentID)
	}
}

func TestCollect_WithIncidentAndProblemCase(t *testing.T) {
	t.Parallel()
	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1", Namespace: "default"},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			Detector:         "deployment-unavailable",
			Severity:         "high",
		},
	}
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-2", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			ProblemCases: []string{"pc-1"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(inc, pc).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	ev, errs := r.collect(context.Background(), "default", "inc-2", "dr-2")
	var nonLogErrs2 []string
	for _, e := range errs {
		if !strings.Contains(e, "pod logs") {
			nonLogErrs2 = append(nonLogErrs2, e)
		}
	}
	if len(nonLogErrs2) != 0 {
		t.Errorf("expected no non-podlog errors, got: %v", nonLogErrs2)
	}
	if len(ev.ProblemCases) != 1 {
		t.Errorf("expected 1 problem case, got %d", len(ev.ProblemCases))
	}
}

func TestCollectResourceSpec_StatefulSet(t *testing.T) {
	t.Parallel()
	replicas := int32(1)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "myss", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(ss).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "StatefulSet", "myss", "apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

// ── Reconcile full flow ───────────────────────────────────────────────────────

func TestDiagnosticRunReconciler_Reconcile_PendingDR_FullFlow(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-reconcile", Namespace: "kubechan"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
		},
	}
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dr-reconcile", Namespace: "kubechan"},
		Spec:       v1alpha1.DiagnosticRunSpec{IncidentRef: "inc-reconcile"},
	}

	scheme := newDWTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(inc, dr).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()

	// Set initial pending state
	drCopy := dr.DeepCopy()
	drCopy.Status.State = v1alpha1.DiagnosticRunStatePending
	_ = c.Status().Update(context.Background(), drCopy)

	r := &DiagnosticRunReconciler{
		Client:           c,
		Logger:           newSlogDiscard(),
		BackendAPIURL:    srv.URL,
		CollectorVersion: "v1.0.0",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "kubechan", Name: "dr-reconcile"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}
}

func TestCollectPodDependencies_WithSecret(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: "secret-vol", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "mysecret"},
				}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	deps := r.collectPodDependencies(context.Background(), "default", pod)
	if deps == nil {
		t.Fatal("expected non-nil deps")
	}
	if len(deps.Secrets) == 0 {
		t.Error("expected at least one Secret ref")
	}
}

func TestCollectPodDependencies_WithEnvFromCM(t *testing.T) {
	t.Parallel()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "env-cm", Namespace: "default"},
		Data:       map[string]string{"key": "value"},
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					EnvFrom: []corev1.EnvFromSource{
						{ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "env-cm"},
						}},
					},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(cm).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	deps := r.collectPodDependencies(context.Background(), "default", pod)
	if deps == nil {
		t.Fatal("expected non-nil deps")
	}
	if len(deps.ConfigMaps) == 0 {
		t.Error("expected at least one ConfigMap ref")
	}
}

func TestCollectResourceSpec_DaemonSet(t *testing.T) {
	t.Parallel()
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "myds", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(ds).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}
	spec, err := r.collectResourceSpec(context.Background(), "default", "DaemonSet", "myds", "apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_Job(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newDWTestScheme()

	completions := int32(1)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "my-job", Namespace: "default"},
		Spec:       batchv1.JobSpec{Completions: &completions},
		Status:     batchv1.JobStatus{Succeeded: 1},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(job).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard()}

	spec, err := r.collectResourceSpec(ctx, "default", "Job", "my-job", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_CronJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newDWTestScheme()

	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cron", Namespace: "default"},
		Spec:       batchv1.CronJobSpec{Schedule: "*/5 * * * *"},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cj).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard()}

	spec, err := r.collectResourceSpec(ctx, "default", "CronJob", "my-cron", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_Ingress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newDWTestScheme()

	pt := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pt,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "svc",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ing).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard()}

	spec, err := r.collectResourceSpec(ctx, "default", "Ingress", "my-ingress", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}


func TestCollectResourceSpec_PVC_Bound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newDWTestScheme()

	sc := "standard"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc2", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(pvc).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard()}

	spec, err := r.collectResourceSpec(ctx, "default", "PersistentVolumeClaim", "my-pvc2", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Error("expected non-nil spec")
	}
}

func TestCollectResourceSpec_UnknownKind_WithAPIGroup_ReturnsDynamicError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newDWTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard()}

	// apiGroup non-empty → tries collectDynamicSpec → ctrl.GetConfig() errors in test
	_, err := r.collectResourceSpec(ctx, "default", "SomeCustomResource", "my-cr", "custom.io")
	if err == nil {
		t.Fatal("expected error from dynamic client (no cluster config)")
	}
}

func TestCollect_WithRelatedResources(t *testing.T) {
	t.Parallel()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-related", Namespace: "default"},
		Spec: v1alpha1.IncidentSpec{
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "myapp", Namespace: "default"},
			RelatedResources: []v1alpha1.ResourceRef{
				{Kind: "ConfigMap", Name: "my-cm", Namespace: "default"},
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cm", Namespace: "default"},
		Data:       map[string]string{"key": "value"},
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ing", Namespace: "default"},
		Spec:       networkingv1.IngressSpec{},
	}
	c := fake.NewClientBuilder().WithScheme(newDWTestScheme()).WithObjects(inc, cm, ing).Build()
	r := &DiagnosticRunReconciler{Client: c, Logger: newSlogDiscard(), CollectorVersion: "test"}

	ev, _ := r.collect(context.Background(), "default", "inc-related", "dr-related")
	if len(ev.RelatedResourceEvidence) == 0 {
		t.Error("expected related resource evidence")
	}
}
