// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package transfer_test

import (
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/transfer"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestConcurrentProgressAndRetries(t *testing.T) {
	t.Parallel()
	var last port.SyncProgress
	calls := 0
	tracker := transfer.New(func(p port.SyncProgress) {
		// Deliberately no mutex: the tracker must serialize its consumer.
		if p.Bytes < last.Bytes || p.Files < last.Files || p.Bytes > p.BytesTotal || p.Files > p.FilesTotal {
			t.Errorf("progress regressed: %+v -> %+v", last, p)
		}
		calls++
		last = p
	}, []int64{20, 30, 0, -1})
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			update := tracker.File(i)
			for _, n := range []int64{0, 10, 3, 100} {
				update(n, 100)
			}
			tracker.Finish(i)
			tracker.Finish(i)
			update(0, 100)
		})
	}
	wg.Wait()
	if last != (port.SyncProgress{Files: 4, FilesTotal: 4, Bytes: 50, BytesTotal: 50}) || calls < 5 {
		t.Fatalf("last %+v (%d calls)", last, calls)
	}
	quiet := transfer.New(nil, []int64{10})
	if quiet.File(0) != nil {
		t.Fatal("disabled tracker returned a callback")
	}
	quiet.Finish(0)
}
