// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
