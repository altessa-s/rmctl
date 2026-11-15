// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"sync"

	"github.com/altessa-s/rmctl/internal/app/transfer"
	"github.com/altessa-s/rmctl/internal/port"
)

// treeProgress reports each worker's bytes against the whole plan. Page
// totals become known as the renderer opens documents; counts are serialized
// across workers and include pages the renderer processed but omitted.
type treeProgress struct {
	mu            sync.Mutex
	opts          port.RenderOptions
	bytes         *transfer.Tracker
	pages, totals []int64
}

func newTreeProgress(opts port.RenderOptions, plan []Planned) *treeProgress {
	sizes := make([]int64, len(plan))
	for i, p := range plan {
		for _, f := range p.Document.Files {
			sizes[i] += f.Size
		}
	}
	var report func(port.SyncProgress)
	if opts.DownloadProgress != nil {
		report = func(p port.SyncProgress) { opts.DownloadProgress(p.Bytes, p.BytesTotal) }
	}
	return &treeProgress{opts: opts, bytes: transfer.New(report, sizes), pages: make([]int64, len(plan)), totals: make([]int64, len(plan))}
}

func (p *treeProgress) options(i int) port.RenderOptions {
	opts := p.opts
	if opts.Progress != nil {
		opts.Progress = func(n, total int64) { p.drawn(i, n, total) }
	}
	if update := p.bytes.File(i); update != nil {
		opts.DownloadProgress = func(n, total int64) {
			update(n, total)
			if n >= total {
				p.bytes.Finish(i)
			}
		}
	}
	return opts
}

func (p *treeProgress) drawn(i int, n, total int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pages[i], p.totals[i] = max(p.pages[i], n), max(p.totals[i], total)
	var done, all int64
	for j := range p.pages {
		done += p.pages[j]
		all += p.totals[j]
	}
	p.opts.Progress(done, all)
}
