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
	// Detector / debounce thresholds (seconds)
	"detector.debounce_window_secs":       true,
	"detector.pending_threshold_secs":     true,
	"detector.unavailable_threshold_secs": true,
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
	defer func() { _ = rows.Close() }()

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

// DetectorConfig is the detector/debounce configuration returned to internal callers.
type DetectorConfig struct {
	DebounceWindowSecs       int64 `json:"debounce_window_secs"`
	PendingThresholdSecs     int64 `json:"pending_threshold_secs"`
	UnavailableThresholdSecs int64 `json:"unavailable_threshold_secs"`
}

// GetDetectorConfig handles GET /internal/settings — returns detector thresholds.
// No auth is required; this endpoint is only reachable from inside the cluster.
func (h *Settings) GetDetectorConfig(w http.ResponseWriter, r *http.Request) {
	keys := []string{
		"detector.debounce_window_secs",
		"detector.pending_threshold_secs",
		"detector.unavailable_threshold_secs",
	}
	vals := map[string]int64{
		"detector.debounce_window_secs":       30,
		"detector.pending_threshold_secs":     300,
		"detector.unavailable_threshold_secs": 300,
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT key, value FROM settings WHERE key IN ('detector.debounce_window_secs','detector.pending_threshold_secs','detector.unavailable_threshold_secs')`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k, v string
			if scanErr := rows.Scan(&k, &v); scanErr == nil {
				var n int64
				if jsonErr := json.Unmarshal([]byte(v), &n); jsonErr == nil && n > 0 {
					vals[k] = n
				}
			}
		}
	}
	_ = keys // used via vals map
	writeJSON(w, http.StatusOK, DetectorConfig{
		DebounceWindowSecs:       vals["detector.debounce_window_secs"],
		PendingThresholdSecs:     vals["detector.pending_threshold_secs"],
		UnavailableThresholdSecs: vals["detector.unavailable_threshold_secs"],
	})
}
