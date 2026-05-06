// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import "net/http"

// llmModelsResponse is returned by GET /api/v1/llm-models.
type llmModelsResponse struct {
	Providers map[string][]llmModelEntry `json:"providers"`
}

type llmModelEntry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var llmModels = llmModelsResponse{
	Providers: map[string][]llmModelEntry{
		"copilot": {
			{ID: "gpt-4.1", Label: "GPT-4.1"},
			{ID: "gpt-5-mini", Label: "GPT-5 mini"},
			{ID: "gpt-5.2", Label: "GPT-5.2"},
			{ID: "gpt-5.2-codex", Label: "GPT-5.2 Codex"},
			{ID: "gpt-5.3-codex", Label: "GPT-5.3 Codex"},
			{ID: "gpt-5.4", Label: "GPT-5.4"},
			{ID: "gpt-5.4-mini", Label: "GPT-5.4 mini"},
			{ID: "gpt-5.5", Label: "GPT-5.5"},
			{ID: "claude-haiku-4.5", Label: "Claude Haiku 4.5"},
			{ID: "claude-sonnet-4.5", Label: "Claude Sonnet 4.5"},
			{ID: "claude-sonnet-4.6", Label: "Claude Sonnet 4.6"},
			{ID: "claude-opus-4.5", Label: "Claude Opus 4.5"},
			{ID: "claude-opus-4.6", Label: "Claude Opus 4.6"},
			{ID: "claude-opus-4.7", Label: "Claude Opus 4.7"},
			{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro"},
			{ID: "gemini-3-flash", Label: "Gemini 3 Flash"},
			{ID: "gemini-3.1-pro", Label: "Gemini 3.1 Pro"},
			{ID: "grok-code-fast-1", Label: "Grok Code Fast 1"},
		},
		"bedrock": {
			{ID: "qwen3-32b", Label: "Qwen3 32B"},
			{ID: "qwen3-235b", Label: "Qwen3 235B"},
		},
	},
}

// GetLLMModels handles GET /api/v1/llm-models.
func GetLLMModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, llmModels)
}
