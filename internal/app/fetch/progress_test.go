// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"context"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/port"
)

func recordProgress(t *testing.T, last *port.SyncProgress) func(port.SyncProgress) {
	t.Helper()
	return func(p port.SyncProgress) {
		if p.Bytes < last.Bytes || p.Files < last.Files || p.Bytes > p.BytesTotal || p.Files > p.FilesTotal {
			t.Errorf("progress regressed: %+v -> %+v", *last, p)
		}
		*last = p // The use case must serialize consumers, even with parallel workers.
	}
}

func TestBulkProgress(t *testing.T) {
	t.Parallel()
	s, c, _ := setup(t)
	tree, _, err := library.New(c, c, c).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var originals, source int64
	docs := 0
	for _, it := range tree.Descendants("f1aaaaaa") {
		if it.Kind.IsFolder() || tree.IsTrashed(it.ID) {
			continue
		}
		docs++
		for _, file := range it.Files {
			source += file.Size
		}
		if file, ok := it.Original(); ok {
			originals += file.Size
		}
	}
	var last port.SyncProgress
	r, err := s.Mirror(t.Context(), "/Books", "out", port.MirrorOptions{Jobs: 8, Progress: recordProgress(t, &last)})
	if err != nil || last.Files != r.Written || last.Bytes != originals || last.Files != last.FilesTotal {
		t.Fatalf("mirror %+v %+v %v", r, last, err)
	}
	last = port.SyncProgress{}
	r, err = s.Mirror(t.Context(), "/Books", "out", port.MirrorOptions{Jobs: 8, Incremental: true, Progress: recordProgress(t, &last)})
	if err != nil || r.Written != 0 || last.Bytes != originals || last.Files != last.FilesTotal {
		t.Fatalf("incremental %+v %+v %v", r, last, err)
	}
	last = port.SyncProgress{}
	exported, err := s.Export(t.Context(), []string{"/Books"}, "export.rmdoc", recordProgress(t, &last))
	if err != nil || last.Files != exported.Documents || last.Files != docs || last.Bytes != source || last.Bytes != last.BytesTotal {
		t.Fatalf("export %+v %+v %v", exported, last, err)
	}
	for range 2 {
		last = port.SyncProgress{}
		backup, err := s.Backup(t.Context(), "bk", "/Books", port.BackupOptions{Jobs: 8, Progress: recordProgress(t, &last)})
		if err != nil || last.Files != backup.Written+backup.Skipped || last.Files != backup.Documents || last.Bytes != source || last.Bytes != last.BytesTotal {
			t.Fatalf("backup %+v %+v %v", backup, last, err)
		}
	}
}

func TestCanceledProgressStaysIncomplete(t *testing.T) {
	t.Parallel()
	s, _, _ := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var last port.SyncProgress
	_, err := s.Mirror(ctx, "/Books", "out", port.MirrorOptions{Jobs: 1, Progress: func(p port.SyncProgress) { last = p; cancel() }})
	if err == nil || last.Files != 0 || last.Bytes != 0 || last.FilesTotal == 0 {
		t.Fatalf("canceled %+v %v", last, err)
	}
}
