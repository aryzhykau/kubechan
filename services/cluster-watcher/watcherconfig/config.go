// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

// Package watcherconfig provides a live-reloadable configuration for the
// cluster-watcher's detector thresholds.  Values are stored as atomic int64
// nanosecond durations so reads are lock-free.
package watcherconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	defaultDebounceWindow       = 30 * time.Second
	defaultPendingThreshold     = 5 * time.Minute
	defaultUnavailableThreshold = 5 * time.Minute
)

// Config holds the live detector thresholds.  The zero value is valid and
// uses the built-in defaults.
type Config struct {
	debounceWindow       atomic.Int64
	pendingThreshold     atomic.Int64
	unavailableThreshold atomic.Int64
}

// New creates a Config pre-seeded with the supplied values (0 → use default).
func New(debounce, pending, unavailable time.Duration) *Config {
	c := &Config{}
	c.Set(debounce, pending, unavailable)
	return c
}

// Set atomically updates all three thresholds.  A value of 0 resets the
// corresponding field to its built-in default.
func (c *Config) Set(debounce, pending, unavailable time.Duration) {
	store := func(a *atomic.Int64, d, def time.Duration) {
		if d <= 0 {
			a.Store(int64(def))
		} else {
			a.Store(int64(d))
		}
	}
	store(&c.debounceWindow, debounce, defaultDebounceWindow)
	store(&c.pendingThreshold, pending, defaultPendingThreshold)
	store(&c.unavailableThreshold, unavailable, defaultUnavailableThreshold)
}

func (c *Config) DebounceWindow() time.Duration {
	return time.Duration(c.debounceWindow.Load())
}

func (c *Config) PendingThreshold() time.Duration {
	return time.Duration(c.pendingThreshold.Load())
}

func (c *Config) UnavailableThreshold() time.Duration {
	return time.Duration(c.unavailableThreshold.Load())
}

// remoteSettings mirrors the JSON returned by GET /internal/settings.
type remoteSettings struct {
	DebounceWindowSecs       int64 `json:"debounce_window_secs"`
	PendingThresholdSecs     int64 `json:"pending_threshold_secs"`
	UnavailableThresholdSecs int64 `json:"unavailable_threshold_secs"`
}

// Refresh fetches the latest thresholds from the backend-api and applies them.
// backendURL should be the base URL, e.g. "http://kubechan-backend-api:8080".
// Returns a non-nil error only when the HTTP call itself fails; a 4xx/5xx
// response is treated as "keep current values" and returns nil.
func (c *Config) Refresh(ctx context.Context, backendURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backendURL+"/internal/settings", nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /internal/settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil // backend returned error; keep current values
	}

	var s remoteSettings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	c.Set(
		time.Duration(s.DebounceWindowSecs)*time.Second,
		time.Duration(s.PendingThresholdSecs)*time.Second,
		time.Duration(s.UnavailableThresholdSecs)*time.Second,
	)
	return nil
}

// StartPolling refreshes the config at the given interval until ctx is done.
// The first refresh happens immediately (errors are logged but not fatal).
// logFn receives informational messages; pass nil to silence.
func (c *Config) StartPolling(ctx context.Context, backendURL string, interval time.Duration, logFn func(msg string, args ...any)) {
	if logFn == nil {
		logFn = func(string, ...any) {}
	}
	refresh := func() {
		if err := c.Refresh(ctx, backendURL); err != nil {
			logFn("detector config refresh failed", "error", err)
		} else {
			logFn("detector config refreshed",
				"debounce", c.DebounceWindow(),
				"pending", c.PendingThreshold(),
				"unavailable", c.UnavailableThreshold(),
			)
		}
	}
	refresh()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh()
			}
		}
	}()
}
