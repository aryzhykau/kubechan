// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// LLMSettings holds dependencies for user LLM settings handlers.
type LLMSettings struct {
	DB *sql.DB
}

// llmSettingsResponse is returned by GET /api/v1/me/llm-settings.
// Secret values are never returned — only whether they are configured.
type llmSettingsResponse struct {
	Provider    string         `json:"provider"`
	Configured  bool           `json:"configured"`
	CredFields  map[string]any `json:"credFields"` // keys present, values masked to "***" if non-empty
}

// Get handles GET /api/v1/me/llm-settings
func (h *LLMSettings) Get(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := UserFromCtx(r.Context())

	var provider, credsJSON string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT provider, credentials FROM user_llm_settings WHERE user_id = ?`, userID,
	).Scan(&provider, &credsJSON)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, llmSettingsResponse{
			Provider:   "bedrock",
			Configured: false,
			CredFields: map[string]any{},
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var raw map[string]any
	_ = json.Unmarshal([]byte(credsJSON), &raw)

	// Mask secret values — return key names only with "***" placeholder.
	masked := make(map[string]any, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			masked[k] = "***"
		} else {
			masked[k] = v
		}
	}

	writeJSON(w, http.StatusOK, llmSettingsResponse{
		Provider:   provider,
		Configured: len(raw) > 0,
		CredFields: masked,
	})
}

// llmSettingsPutRequest is the body for PUT /api/v1/me/llm-settings.
type llmSettingsPutRequest struct {
	Provider    string         `json:"provider"`
	Credentials map[string]any `json:"credentials"`
}

var allowedProviders = map[string]bool{
	"bedrock": true,
	"copilot": true,
}

// Put handles PUT /api/v1/me/llm-settings
func (h *LLMSettings) Put(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := UserFromCtx(r.Context())

	var req llmSettingsPutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !allowedProviders[req.Provider] {
		writeError(w, http.StatusBadRequest, "provider must be one of: bedrock, copilot")
		return
	}
	if req.Credentials == nil {
		req.Credentials = map[string]any{}
	}

	credsJSON, err := json.Marshal(req.Credentials)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid credentials")
		return
	}

	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO user_llm_settings (user_id, provider, credentials, updated_at)
		 VALUES (?, ?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   provider    = excluded.provider,
		   credentials = excluded.credentials,
		   updated_at  = excluded.updated_at`,
		userID, req.Provider, string(credsJSON),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
