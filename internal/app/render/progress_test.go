// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func counts(t *testing.T, done, total *int64) port.Progress {
	t.Helper()
	return func(n, all int64) {
		if n < *done || all < *total || n > all {
			t.Errorf("progress regressed: %d/%d -> %d/%d", *done, *total, n, all)
		}
		*done, *total = n, all
	}
}

func TestRenderProgressAndTreeConcurrency(t *testing.T) {
	t.Parallel()
	f := treeSetup(t)
	var pages, pageTotal, bytes, byteTotal int64
	opts := port.RenderOptions{Progress: counts(t, &pages, &pageTotal), DownloadProgress: counts(t, &bytes, &byteTotal)}
	r, err := f.s.Render(t.Context(), "d1", "one.pdf", opts)
	if err != nil || pages != int64(r.Pages) || pages != pageTotal || bytes == 0 || bytes != byteTotal {
		t.Fatalf("render %+v %d/%d pages %d/%d bytes %v", r, pages, pageTotal, bytes, byteTotal, err)
	}
	pages, pageTotal, bytes, byteTotal = 0, 0, 0, 0
	tree, err := f.s.RenderTree(t.Context(), "/Books", "out", opts, port.RenderTreeOptions{Jobs: 8})
	var drawn int
	for _, d := range tree.Documents {
		drawn += d.Report.Pages
	}
	if err != nil || pages != int64(drawn) || pages != pageTotal || bytes == 0 || bytes != byteTotal {
		t.Fatalf("tree %d pages %d/%d, bytes %d/%d %v", drawn, pages, pageTotal, bytes, byteTotal, err)
	}
	// Progress does not change the manifest identity: an incremental repeat keeps every PDF.
	pages, pageTotal, bytes, byteTotal = 0, 0, 0, 0
	tree, err = f.s.RenderTree(t.Context(), "/Books", "out", opts, port.RenderTreeOptions{Jobs: 8, Incremental: true})
	if err != nil || tree.Count(port.RenderUnchanged) != len(tree.Documents) || pages != 0 || bytes != byteTotal {
		t.Fatalf("incremental %+v %d/%d bytes %v", tree, bytes, byteTotal, err)
	}
}

func TestLocalRenderProgressHasNoDownload(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive(t, f, "local.rmdoc", map[domain.ItemID]string{"a1": "Meeting"})
	var pages, total int64
	downloads := 0
	r, err := f.s.RenderFile(t.Context(), "local.rmdoc", "out.pdf", port.RenderOptions{Progress: counts(t, &pages, &total), DownloadProgress: func(int64, int64) { downloads++ }})
	if err != nil || pages != int64(r.Pages) || pages != total || downloads != 0 {
		t.Fatalf("local render %+v %d/%d pages, %d downloads %v", r, pages, total, downloads, err)
	}
}
