// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLLMModels(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/llm-models", nil)
	w := httptest.NewRecorder()
	GetLLMModels(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp llmModelsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Providers) == 0 {
		t.Error("expected at least one provider in response")
	}
	if _, ok := resp.Providers["copilot"]; !ok {
		t.Error("expected 'copilot' provider in response")
	}
	if _, ok := resp.Providers["bedrock"]; !ok {
		t.Error("expected 'bedrock' provider in response")
	}
	foundClaudeSonnet := false
	for _, model := range resp.Providers["bedrock"] {
		if model.ID == "claude-sonnet-6" && model.Label == "Claude Sonnet 4.6" {
			foundClaudeSonnet = true
			break
		}
	}
	if !foundClaudeSonnet {
		t.Error("expected Claude Sonnet 4.6 in Bedrock models")
	}
}
