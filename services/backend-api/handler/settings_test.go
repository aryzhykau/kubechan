// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettings_GetDetectorConfig(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	r := httptest.NewRequest(http.MethodGet, "/internal/settings", nil)
	w := httptest.NewRecorder()
	h.GetDetectorConfig(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var cfg DetectorConfig
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Migration seeds should provide non-zero defaults.
	if cfg.DebounceWindowSecs == 0 {
		t.Error("expected non-zero DebounceWindowSecs from migration seed")
	}
	if cfg.PendingThresholdSecs == 0 {
		t.Error("expected non-zero PendingThresholdSecs from migration seed")
	}
	if cfg.UnavailableThresholdSecs == 0 {
		t.Error("expected non-zero UnavailableThresholdSecs from migration seed")
	}
}

func TestSettings_AllowedKeys(t *testing.T) {
	t.Parallel()
	// Verify the allowed keys map covers the detector-related keys returned by GetDetectorConfig.
	for _, k := range []string{
		"detector.debounce_window_secs",
		"detector.pending_threshold_secs",
		"detector.unavailable_threshold_secs",
	} {
		if !allowedSettingKeys[k] {
			t.Errorf("key %q not in allowedSettingKeys", k)
		}
	}
}

func TestSettings_Get_ReturnsRows(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// openDB runs migrations which seed some settings
	if len(result) == 0 {
		t.Error("expected at least one setting from migration seed")
	}
}

func TestSettings_Update_BadJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSettings_Update_BadKey(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	body := `{"unknown.key": "value"}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSettings_Update_ValidKey(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	body := `{"detector.debounce_window_secs": 42}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestSettings_IdleMessage_NotImplemented(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &Settings{DB: db}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/persona/idle-message", nil)
	w := httptest.NewRecorder()
	h.IdleMessage(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
}
