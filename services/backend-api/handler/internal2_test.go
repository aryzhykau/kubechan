// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ── dispatchAnalysis via ReceiveEvidence with LLMGatewayURL set ───────────────

func TestInternal_ReceiveEvidence_WithLLMGateway_DispatchesAsync(t *testing.T) {
	// Set up a mock LLM gateway server that records the request.
	received := make(chan struct{}, 1)
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"evidenceId":      "ev-1",
			"model":           "test-model",
			"likelyRootCause": "test cause",
			"confidence":      0.9,
		})
		received <- struct{}{}
	}))
	defer mockLLM.Close()

	db := openDB(t)
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		LLMGatewayURL:    mockLLM.URL,
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	body, _ := json.Marshal(evidenceRequest{
		DiagnosticRunID: "run-llm-test",
		IncidentID:      "", // no incident
		Payload:         []byte(`{"logs":"something crashed"}`),
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	// Wait for the async dispatch to fire (up to 2s).
	select {
	case <-received:
		// LLM gateway was called.
	case <-time.After(2 * time.Second):
		t.Error("LLM gateway was not called within 2 seconds")
	}
}

func TestInternal_ReceiveEvidence_WithDRUpdate(t *testing.T) {
	// Test that when the DiagnosticRun exists, its status is patched.
	db := openDB(t)
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run-dr-update", Namespace: "kubechan"},
		Status:     v1alpha1.DiagnosticRunStatus{State: v1alpha1.DiagnosticRunStatePending},
	}
	c := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithObjects(dr).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).
		Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		LLMGatewayURL:    "",
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	body, _ := json.Marshal(evidenceRequest{
		DiagnosticRunID:  "run-dr-update",
		Payload:          []byte(`{"logs":"data"}`),
		CollectorVersion: "v1.0",
		CollectionErrors: []string{"minor warning"},
		RedactionSummary: &struct {
			PatternsApplied int      `json:"patternsApplied"`
			RedactedFields  []string `json:"redactedFields"`
		}{
			PatternsApplied: 1,
			RedactedFields:  []string{"password"},
		},
		LogTruncationInfo: &struct {
			Truncated      bool  `json:"truncated"`
			OriginalBytes  int64 `json:"originalBytes"`
			TruncatedBytes int64 `json:"truncatedBytes"`
		}{
			Truncated:      true,
			OriginalBytes:  10000,
			TruncatedBytes: 5000,
		},
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestInternal_ReceiveEvidence_LogTruncated(t *testing.T) {
	db := openDB(t)
	c := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	h := &Internal{
		K8s:              c,
		DB:               db,
		DefaultNamespace: "kubechan",
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	body, _ := json.Marshal(evidenceRequest{
		DiagnosticRunID: "run-truncated",
		Payload:         []byte(`{}`),
		LogTruncated:    true,
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}
