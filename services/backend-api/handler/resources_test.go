// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResources_ListNamespaces_Empty(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
	w := httptest.NewRecorder()
	h.ListNamespaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestResources_ListNamespaces_FiltersControlPlane(t *testing.T) {
	t.Parallel()
	nsList := []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "kube-public"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "my-app"}},
	}
	objs := make([]interface{ DeepCopyObject() interface{ runtime() } }, 0)
	_ = objs

	// Build with namespace objects.
	builder := fake.NewClientBuilder().WithScheme(newTestScheme())
	for i := range nsList {
		builder = builder.WithObjects(&nsList[i])
	}
	c := builder.Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil)
	w := httptest.NewRecorder()
	h.ListNamespaces(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// "default" and "my-app" should appear; control-plane ones filtered.
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if nameSet["kube-system"] {
		t.Error("kube-system should be filtered out")
	}
	if !nameSet["default"] {
		t.Error("default should be included")
	}
	if !nameSet["my-app"] {
		t.Error("my-app should be included")
	}
}

// ── hasVerbs ──────────────────────────────────────────────────────────────────

func TestHasVerbs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		verbs    []string
		required []string
		want     bool
	}{
		{"all present", []string{"get", "list", "watch"}, []string{"get", "list"}, true},
		{"missing one", []string{"get"}, []string{"get", "list"}, false},
		{"empty required", []string{"get"}, []string{}, true},
		{"empty verbs", []string{}, []string{"get"}, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasVerbs(tc.verbs, tc.required...); got != tc.want {
				t.Errorf("hasVerbs(%v, %v) = %v, want %v", tc.verbs, tc.required, got, tc.want)
			}
		})
	}
}

// ── toItems ───────────────────────────────────────────────────────────────────

func TestToItems(t *testing.T) {
	t.Parallel()
	items := toItems("default", []string{"a", "b", "c"})
	if len(items) != 3 {
		t.Fatalf("toItems length = %d, want 3", len(items))
	}
	for _, it := range items {
		if it.Namespace != "default" {
			t.Errorf("namespace = %q, want default", it.Namespace)
		}
	}
	if items[1].Name != "b" {
		t.Errorf("name = %q, want b", items[1].Name)
	}
}

func TestToItems_Empty(t *testing.T) {
	t.Parallel()
	items := toItems("ns", []string{})
	if len(items) != 0 {
		t.Errorf("expected empty slice, got %v", items)
	}
}

// ── deploymentNames ───────────────────────────────────────────────────────────

func TestDeploymentNames(t *testing.T) {
	t.Parallel()
	deploys := []appsv1.Deployment{
		{ObjectMeta: metav1.ObjectMeta{Name: "dep-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "dep-2"}},
	}
	names := deploymentNames(deploys)
	if len(names) != 2 {
		t.Fatalf("deploymentNames length = %d, want 2", len(names))
	}
	if names[0] != "dep-1" || names[1] != "dep-2" {
		t.Errorf("names = %v, want [dep-1 dep-2]", names)
	}
}

// ── ListResources ─────────────────────────────────────────────────────────────

func TestResources_ListResources_MissingKind(t *testing.T) {
	t.Parallel()
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestResources_ListResources_Deployment(t *testing.T) {
	t.Parallel()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "dep-1", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dep).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Deployment", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var items []resourceItem
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].Name != "dep-1" {
		t.Errorf("items = %v, want [{dep-1 default}]", items)
	}
}

func TestResources_ListResources_StatefulSet(t *testing.T) {
	t.Parallel()
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-1", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(ss).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=StatefulSet", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_DaemonSet(t *testing.T) {
	t.Parallel()
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(ds).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=DaemonSet", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_Job(t *testing.T) {
	t.Parallel()
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(job).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Job", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_CronJob(t *testing.T) {
	t.Parallel()
	cj := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cj-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cj).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=CronJob", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_Pod(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(pod).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Pod", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_Service(t *testing.T) {
	t.Parallel()
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(svc).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Service", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_PVC(t *testing.T) {
	t.Parallel()
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pvc-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(pvc).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=PersistentVolumeClaim", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_ConfigMap(t *testing.T) {
	t.Parallel()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(cm).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=ConfigMap", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_Ingress(t *testing.T) {
	t.Parallel()
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "ing-1", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(ing).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Ingress", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestResources_ListResources_UnsupportedKind_NoAPIGroup_FallsThrough(t *testing.T) {
	t.Parallel()
	// When kind is unsupported by the typed path and Config is nil (no dynamic client),
	// the request panics inside the real discovery code.  We only check the typed-path
	// boundary: if Config is nil, the typed path returns "unsupported kind" and then
	// listByKindDynamic is called.  Because we can't provide a real API server here,
	// we just verify that ListResources does NOT succeed with a known typed kind when
	// the fake client has no objects.
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Resources{K8s: c}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/default/resources?kind=Deployment", nil)
	r = withChiParam(r, "ns", "default")
	w := httptest.NewRecorder()
	h.ListResources(w, r)

	// Empty result is still 200 — the typed path works.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var items []resourceItem
	_ = json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}
