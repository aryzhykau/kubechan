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

// providerSecretFields lists credential keys that must be masked in GET responses.
// Non-listed keys are returned as their actual stored values so the frontend can
// pre-populate non-sensitive form fields (region, modelId, temperature, etc.).
var providerSecretFields = map[string]map[string]bool{
	"bedrock": {"bearerToken": true, "accessKeyId": true, "secretAccessKey": true},
	"copilot": {"token": true},
}

// llmSettingsResponse is returned by GET /api/v1/me/llm-settings.
// Secret fields are masked to "***". Non-secret fields are returned verbatim.
type llmSettingsResponse struct {
	Provider   string         `json:"provider"`
	Configured bool           `json:"configured"`
	CredFields map[string]any `json:"credFields"`
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

	secrets := providerSecretFields[provider]
	result := make(map[string]any, len(raw))
	for k, v := range raw {
		if secrets[k] {
			// Mask non-empty secret values; keep empty/null as-is.
			if s, ok := v.(string); ok && s != "" {
				result[k] = "***"
			} else {
				result[k] = v
			}
		} else {
			// Return non-secret fields (region, modelId, thinkingBudget, etc.) verbatim.
			result[k] = v
		}
	}

	writeJSON(w, http.StatusOK, llmSettingsResponse{
		Provider:   provider,
		Configured: len(raw) > 0,
		CredFields: result,
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

	// Merge: if a secret field is absent from the request (user left it blank to
	// keep the stored value), preserve the existing stored value rather than wiping it.
	var existingCredsJSON string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT credentials FROM user_llm_settings WHERE user_id = ?`, userID,
	).Scan(&existingCredsJSON)
	if existingCredsJSON != "" {
		var existing map[string]any
		if err := json.Unmarshal([]byte(existingCredsJSON), &existing); err == nil {
			for field := range providerSecretFields[req.Provider] {
				if _, supplied := req.Credentials[field]; !supplied {
					if val, ok := existing[field]; ok {
						req.Credentials[field] = val
					}
				}
			}
		}
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
