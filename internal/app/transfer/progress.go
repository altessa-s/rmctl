// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package transfer aggregates progress from independent concurrent transfers.
package transfer

import (
	"sync"

	"github.com/altessa-s/rmctl/internal/port"
)

// Tracker serializes callbacks and counts each successful file once. Bytes
// measure completed work, including files found unchanged. Retries never
// subtract bytes or count the same work twice.
type Tracker struct {
	mu           sync.Mutex
	report       func(port.SyncProgress)
	state        port.SyncProgress
	bytes, sizes []int64
	finished     []bool
}

// New starts a transfer with the given file sizes, emitting its initial totals.
func New(report func(port.SyncProgress), sizes []int64) *Tracker {
	t := &Tracker{report: report, sizes: append([]int64(nil), sizes...), bytes: make([]int64, len(sizes)), finished: make([]bool, len(sizes))}
	t.state.FilesTotal = len(sizes)
	for _, n := range sizes {
		t.state.BytesTotal += max(n, 0)
	}
	t.emit()
	return t
}

func (t *Tracker) emit() {
	if t.report != nil {
		t.report(t.state)
	}
}

// File is the byte callback of file i, or nil when reporting is disabled.
func (t *Tracker) File(i int) port.Progress {
	if t.report == nil {
		return nil
	}
	return func(n, _ int64) {
		t.mu.Lock()
		defer t.mu.Unlock()
		n = min(max(n, t.bytes[i]), max(t.sizes[i], 0))
		t.state.Bytes += n - t.bytes[i]
		t.bytes[i] = n
		t.emit()
	}
}

// Finish counts a successful file, also when a cache or an incremental run
// needed no byte callbacks. A failed or canceled file must not call Finish.
func (t *Tracker) Finish(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished[i] {
		return
	}
	t.finished[i] = true
	t.state.Files++
	t.state.Bytes += max(t.sizes[i], 0) - t.bytes[i]
	t.bytes[i] = max(t.sizes[i], 0)
	t.emit()
}
