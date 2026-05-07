// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

// Package debounce provides a key-based debouncer that coalesces rapid repeated calls
// into a single execution after a configurable quiet window.
package debounce

import (
	"sync"
	"time"
)

// Debouncer coalesces rapid calls for the same key into a single execution
// after the configured quiet window expires.
// It is safe for concurrent use.
type Debouncer struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
	window func() time.Duration
}

// New creates a Debouncer whose quiet window is returned by windowFn on each
// Debounce call.  This allows the window to be adjusted at runtime without
// restarting the process.
func New(windowFn func() time.Duration) *Debouncer {
	return &Debouncer{
		timers: make(map[string]*time.Timer),
		window: windowFn,
	}
}

// Debounce schedules fn to run after the quiet window.
// If called again for the same key before the window expires, the timer is reset.
// fn runs in a new goroutine.
func (d *Debouncer) Debounce(key string, fn func()) {
	w := d.window()
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	d.timers[key] = time.AfterFunc(w, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}

// Cancel stops any pending debounced call for key without executing it.
func (d *Debouncer) Cancel(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.timers[key]; ok {
		t.Stop()
		delete(d.timers, key)
	}
}
