// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAugment_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Augment{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	r = withChiParam(r, "id", "kubechan/inc-1")
	w := httptest.NewRecorder()
	h.Augment(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAugment_EmptyResources(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Augment{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(augmentRequest{RelatedResources: []relatedResourceIn{}})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withChiParam(r, "id", "kubechan/inc-1")
	w := httptest.NewRecorder()
	h.Augment(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAugment_IncidentNotFound(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Augment{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	body, _ := json.Marshal(augmentRequest{
		RelatedResources: []relatedResourceIn{{Kind: "Deployment", Name: "app"}},
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r = withChiParam(r, "id", "kubechan/missing-incident")
	w := httptest.NewRecorder()
	h.Augment(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAugment_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!")
	t.Setenv("JWT_TTL_HOURS", "1")

	db := openDB(t)
	insertTestUser(t, db, "uid-1")
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{Name: "inc-aug", Namespace: "kubechan"},
		Spec: v1alpha1.IncidentSpec{
			Source: "auto",
			RootResource: v1alpha1.ResourceRef{Kind: "Deployment", Name: "app"},
		},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(inc).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()
	h := &Augment{K8s: c, DB: db, DefaultNamespace: "kubechan"}

	// Inject auth context.
	token, _ := signToken("uid-1", "alice", "admin")
	baseReq := httptest.NewRequest(http.MethodPost, "/", nil)
	baseReq.Header.Set("Authorization", "Bearer "+token)
	var authedReq *http.Request
	RequireAuth(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		authedReq = req
	})).ServeHTTP(httptest.NewRecorder(), baseReq)

	body, _ := json.Marshal(augmentRequest{
		RelatedResources: []relatedResourceIn{
			{Kind: "Service", Name: "svc", Namespace: "default"},
		},
	})
	authedReq = authedReq.Clone(authedReq.Context())
	authedReq.Body = io.NopCloser(bytes.NewReader(body))
	authedReq = withChiParam(authedReq, "id", "kubechan/inc-aug")

	w := httptest.NewRecorder()
	h.Augment(w, authedReq)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202; body: %s", w.Code, w.Body.String())
	}
}
