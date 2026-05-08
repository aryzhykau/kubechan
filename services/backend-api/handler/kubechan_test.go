// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	k8ssvc "github.com/org/kubechan/services/backend-api/k8s"
	kubews "github.com/org/kubechan/services/backend-api/ws"
)

func newTestMoodSyncer(t *testing.T) (*k8ssvc.MoodSyncer, *v1alpha1.KubeChanState) {
	t.Helper()
	state := &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{Name: "kubechan", Namespace: "kubechan"},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(state).
		WithStatusSubresource(&v1alpha1.KubeChanState{}).
		Build()
	ms := &k8ssvc.MoodSyncer{
		Client:    c,
		Hub:       kubews.NewHub(slog.Default()),
		Namespace: "kubechan",
		Logger:    slog.Default(),
	}
	return ms, state
}

// ── GetState ──────────────────────────────────────────────────────────────────

func TestKubeChan_GetState(t *testing.T) {
	t.Parallel()
	ms, _ := newTestMoodSyncer(t)
	h := &KubeChan{MoodSyncer: ms}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/kubechan/state", nil)
	w := httptest.NewRecorder()
	h.GetState(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["moodLevel"]; !ok {
		t.Error("response missing moodLevel field")
	}
}

// ── Poke ──────────────────────────────────────────────────────────────────────

func TestKubeChan_Poke_Success(t *testing.T) {
	t.Parallel()
	ms, _ := newTestMoodSyncer(t)
	h := &KubeChan{MoodSyncer: ms}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/kubechan/poke", nil)
	w := httptest.NewRecorder()
	h.Poke(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["moodLevel"]; !ok {
		t.Error("response missing moodLevel field")
	}
	if _, ok := body["pokeCount"]; !ok {
		t.Error("response missing pokeCount field")
	}
}

func TestKubeChan_Poke_NotFound(t *testing.T) {
	t.Parallel()
	// MoodSyncer with no state → Poke returns error.
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	ms := &k8ssvc.MoodSyncer{
		Client:    c,
		Hub:       kubews.NewHub(slog.Default()),
		Namespace: "kubechan",
		Logger:    slog.Default(),
	}
	h := &KubeChan{MoodSyncer: ms}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/kubechan/poke", nil)
	r = r.WithContext(context.Background())
	w := httptest.NewRecorder()
	h.Poke(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
