// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package debounce

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebounce_FiresAfterWindow(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	d := New(func() time.Duration { return 20 * time.Millisecond })

	d.Debounce("key", func() { count.Add(1) })

	time.Sleep(50 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected fn to fire once, got %d", count.Load())
	}
}

func TestDebounce_CoalescesRapidCalls(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	d := New(func() time.Duration { return 50 * time.Millisecond })

	// Fire 10 times quickly — should only result in 1 execution.
	for i := 0; i < 10; i++ {
		d.Debounce("key", func() { count.Add(1) })
	}

	time.Sleep(120 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1 execution after coalescing, got %d", count.Load())
	}
}

func TestDebounce_IndependentKeys(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	var countA, countB atomic.Int32
	wg.Add(2)

	d := New(func() time.Duration { return 20 * time.Millisecond })
	d.Debounce("a", func() { countA.Add(1); wg.Done() })
	d.Debounce("b", func() { countB.Add(1); wg.Done() })

	wg.Wait()
	if countA.Load() != 1 || countB.Load() != 1 {
		t.Errorf("expected each key to fire once: a=%d b=%d", countA.Load(), countB.Load())
	}
}

func TestCancel_PreventsExecution(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	d := New(func() time.Duration { return 50 * time.Millisecond })

	d.Debounce("key", func() { count.Add(1) })
	d.Cancel("key")

	time.Sleep(100 * time.Millisecond)
	if count.Load() != 0 {
		t.Errorf("fn should not have fired after Cancel, got %d", count.Load())
	}
}

func TestCancel_NoopOnMissingKey(t *testing.T) {
	t.Parallel()
	d := New(func() time.Duration { return 20 * time.Millisecond })
	// Should not panic on a key that was never debounced.
	d.Cancel("nonexistent")
}

func TestDebounce_ResetsTimer(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	window := 40 * time.Millisecond
	d := New(func() time.Duration { return window })

	// Debounce twice with a gap shorter than the window.
	d.Debounce("key", func() { count.Add(1) })
	time.Sleep(20 * time.Millisecond)
	d.Debounce("key", func() { count.Add(1) })

	// Wait for the second debounce window to expire.
	time.Sleep(80 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected exactly 1 execution after timer reset, got %d", count.Load())
	}
}
