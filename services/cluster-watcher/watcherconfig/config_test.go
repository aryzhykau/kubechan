// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package watcherconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── New / defaults ────────────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	c := New(0, 0, 0)
	if c.DebounceWindow() != defaultDebounceWindow {
		t.Errorf("DebounceWindow = %v, want %v", c.DebounceWindow(), defaultDebounceWindow)
	}
	if c.PendingThreshold() != defaultPendingThreshold {
		t.Errorf("PendingThreshold = %v, want %v", c.PendingThreshold(), defaultPendingThreshold)
	}
	if c.UnavailableThreshold() != defaultUnavailableThreshold {
		t.Errorf("UnavailableThreshold = %v, want %v", c.UnavailableThreshold(), defaultUnavailableThreshold)
	}
}

func TestNew_CustomValues(t *testing.T) {
	t.Parallel()
	c := New(10*time.Second, 2*time.Minute, 3*time.Minute)
	if c.DebounceWindow() != 10*time.Second {
		t.Errorf("DebounceWindow = %v, want 10s", c.DebounceWindow())
	}
	if c.PendingThreshold() != 2*time.Minute {
		t.Errorf("PendingThreshold = %v, want 2m", c.PendingThreshold())
	}
	if c.UnavailableThreshold() != 3*time.Minute {
		t.Errorf("UnavailableThreshold = %v, want 3m", c.UnavailableThreshold())
	}
}

// ── Set ───────────────────────────────────────────────────────────────────────

func TestSet_UpdatesValues(t *testing.T) {
	t.Parallel()
	c := New(0, 0, 0)
	c.Set(5*time.Second, 1*time.Minute, 90*time.Second)
	if c.DebounceWindow() != 5*time.Second {
		t.Errorf("DebounceWindow = %v, want 5s", c.DebounceWindow())
	}
	if c.PendingThreshold() != 1*time.Minute {
		t.Errorf("PendingThreshold = %v, want 1m", c.PendingThreshold())
	}
	if c.UnavailableThreshold() != 90*time.Second {
		t.Errorf("UnavailableThreshold = %v, want 90s", c.UnavailableThreshold())
	}
}

func TestSet_ZeroResetsToDefault(t *testing.T) {
	t.Parallel()
	c := New(5*time.Second, 1*time.Minute, 90*time.Second)
	c.Set(0, 0, 0)
	if c.DebounceWindow() != defaultDebounceWindow {
		t.Errorf("DebounceWindow = %v, want default %v", c.DebounceWindow(), defaultDebounceWindow)
	}
}

// ── Refresh ───────────────────────────────────────────────────────────────────

func TestRefresh_AppliesRemoteSettings(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/settings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(remoteSettings{
			DebounceWindowSecs:       10,
			PendingThresholdSecs:     120,
			UnavailableThresholdSecs: 180,
		})
	}))
	defer srv.Close()

	c := New(0, 0, 0)
	if err := c.Refresh(context.Background(), srv.URL); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if c.DebounceWindow() != 10*time.Second {
		t.Errorf("DebounceWindow = %v, want 10s", c.DebounceWindow())
	}
	if c.PendingThreshold() != 2*time.Minute {
		t.Errorf("PendingThreshold = %v, want 2m", c.PendingThreshold())
	}
	if c.UnavailableThreshold() != 3*time.Minute {
		t.Errorf("UnavailableThreshold = %v, want 3m", c.UnavailableThreshold())
	}
}

func TestRefresh_NonOKStatusKeepsCurrentValues(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(7*time.Second, 0, 0)
	if err := c.Refresh(context.Background(), srv.URL); err != nil {
		t.Fatalf("Refresh() should not error on non-OK status, got: %v", err)
	}
	// Values should be unchanged.
	if c.DebounceWindow() != 7*time.Second {
		t.Errorf("DebounceWindow changed after non-OK response: %v", c.DebounceWindow())
	}
}

func TestRefresh_NetworkError(t *testing.T) {
	t.Parallel()
	c := New(0, 0, 0)
	err := c.Refresh(context.Background(), "http://127.0.0.1:1") // nothing listening
	if err == nil {
		t.Error("Refresh() should return error on network failure")
	}
}

func TestRefresh_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := New(0, 0, 0)
	if err := c.Refresh(context.Background(), srv.URL); err == nil {
		t.Error("Refresh() should return error on invalid JSON")
	}
}

// ── StartPolling ──────────────────────────────────────────────────────────────

func TestStartPolling_RefreshesAndStops(t *testing.T) {
	t.Parallel()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(remoteSettings{
			DebounceWindowSecs: 5,
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := New(0, 0, 0)
	c.StartPolling(ctx, srv.URL, 20*time.Millisecond, nil)

	// Allow a couple of poll cycles.
	time.Sleep(60 * time.Millisecond)
	cancel()

	// At least the initial + one interval poll should have happened.
	if calls < 2 {
		t.Errorf("expected ≥2 refresh calls, got %d", calls)
	}
	if c.DebounceWindow() != 5*time.Second {
		t.Errorf("DebounceWindow = %v, want 5s", c.DebounceWindow())
	}
}
