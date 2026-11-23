// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"os"
	"sync"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

// putProgress keeps one operation's counters across files. Mixed imports and
// text notebooks expose no byte callbacks, so they show an honest file count.
// complete counts a successful input; failures stay incomplete.
func putProgress(streams cliutil.Streams, files []string, dry bool) (next func(int) port.Progress, complete func(int), done func()) {
	if len(files) < 2 || dry || !streams.IsTerminal {
		return func(int) port.Progress { return nil }, func(int) {}, func() {}
	}
	sizes := make([]int64, len(files))
	mixed := false
	for i, file := range files {
		if !isDocument(file) {
			mixed = true
		}
		if info, err := os.Stat(file); err == nil {
			sizes[i] = info.Size()
		}
	}
	if mixed {
		return putCountProgress(streams, len(files))
	}
	update, done := cliutil.Transfer(streams, "uploading")
	var mu sync.Mutex
	state := port.SyncProgress{FilesTotal: len(files)}
	for _, size := range sizes {
		state.BytesTotal += size
	}
	sent := make([]int64, len(files))
	return func(i int) port.Progress {
			return func(n, _ int64) {
				mu.Lock()
				defer mu.Unlock()
				n = min(max(n, sent[i]), sizes[i])
				state.Bytes += n - sent[i]
				sent[i] = n
				update(state)
			}
		}, func(i int) {
			mu.Lock()
			defer mu.Unlock()
			state.Files++
			state.Bytes += sizes[i] - sent[i]
			sent[i] = sizes[i]
			update(state)
		}, done
}

func putCountProgress(streams cliutil.Streams, total int) (func(int) port.Progress, func(int), func()) {
	update, done := cliutil.CountProgress(streams, "uploading files")
	var mu sync.Mutex
	completed := 0
	return func(int) port.Progress {
			mu.Lock()
			defer mu.Unlock()
			update(int64(completed), int64(total))
			return func(int64, int64) {}
		}, func(int) {
			mu.Lock()
			defer mu.Unlock()
			completed++
			update(int64(completed), int64(total))
		}, done
}
