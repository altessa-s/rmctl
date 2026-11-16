// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/port"
)

func TestTreeProgressConcurrentPages(t *testing.T) {
	t.Parallel()
	var steps [][2]int64
	p := newTreeProgress(port.RenderOptions{Progress: func(n, total int64) {
		// The tracker must serialize observers, including this slice append.
		steps = append(steps, [2]int64{n, total})
	}}, make([]Planned, 2))
	var ready, workers sync.WaitGroup
	ready.Add(2)
	workers.Add(2)
	start := make(chan struct{})
	for i, total := range []int64{3, 5} {
		go func() {
			defer workers.Done()
			report := p.options(i).Progress
			report(0, total)
			ready.Done()
			<-start
			for n := int64(1); n <= total; n++ {
				report(n, total)
				// A retry's old counts must not undo this document's work.
				report(0, total-1)
			}
		}()
	}
	ready.Wait()
	close(start)
	workers.Wait()
	var done, all int64
	for _, step := range steps {
		if step[0] < done || step[0] > done+1 || step[1] < all || step[0] > step[1] {
			t.Fatalf("progress regressed or skipped a page: %d/%d -> %v; steps %v", done, all, step, steps)
		}
		done, all = step[0], step[1]
	}
	if len(steps) != 18 || done != 8 || all != 8 {
		t.Fatalf("progress %v; want 18 reports ending at 8/8", steps)
	}
}
