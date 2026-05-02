// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// allowedSettingKeys is the exhaustive list of mutable settings keys.
var allowedSettingKeys = map[string]bool{
	"persona.enabled":            true,
	"persona.idle_chatter":       true,
	"persona.idle_interval_secs": true,
	"bedrock.model_id":           true,
	"bedrock.region":             true,
	"bedrock.thinking_budget":    true,
	"evidence.retention_days":    true,
	"analysis.retention_days":    true,
}

// Settings holds dependencies for settings handlers.
type Settings struct {
	DB *sql.DB
}

// Get handles GET /api/v1/settings
func (h *Settings) Get(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT key, value FROM settings`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	result := map[string]any{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			decoded = v
		}
		result[k] = decoded
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Update handles PUT /api/v1/settings
func (h *Settings) Update(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	for k := range body {
		if !allowedSettingKeys[k] {
			writeError(w, http.StatusBadRequest, "unknown setting key: "+k)
			return
		}
	}

	for k, v := range body {
		encoded, err := json.Marshal(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "cannot encode value for key: "+k)
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE settings SET value = ?, updated_at = datetime('now') WHERE key = ?`,
			string(encoded), k,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// IdleMessage handles GET /api/v1/persona/idle-message — stubbed until Phase 3B.
func (h *Settings) IdleMessage(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "llm-gateway not yet implemented (Phase 3B)")
}
