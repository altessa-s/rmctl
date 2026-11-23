// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// unreadableUpload fails one file's stat after Walk has already found it.
type unreadableUpload struct{ *porttest.MemFiles }

func (f unreadableUpload) Stat(path string) (int64, bool, bool, error) {
	if filepath.Base(path) == "B.epub" {
		return 0, false, false, errBoom
	}
	return f.MemFiles.Stat(path)
}

func TestUploadStatFailureOccursWhenFileIsReached(t *testing.T) {
	t.Parallel()
	for _, progress := range []bool{false, true} {
		t.Run(map[bool]string{false: "silent", true: "progress"}[progress], func(t *testing.T) {
			f := setup(t)
			f.files.Write("src/A.epub", []byte(epub))
			f.files.Write("src/B.epub", []byte(epub))
			files := unreadableUpload{f.files}
			s := write.New(library.New(f.c, f.c, f.c), f.c, f.c, files, pdfInspector{f.pdf}, porttest.NewArchiver(f.files), porttest.NewIDs("new"), porttest.NewClock(porttest.Epoch))
			opts := port.UploadOptions{Jobs: 1}
			var last port.SyncProgress
			if progress {
				opts.Progress = observeProgress(t, &last)
			}
			r, err := s.Upload(t.Context(), "src", "/Books", opts)
			if !errors.Is(err, errBoom) || r.Written != 1 || f.c.Count("UploadFile") != 1 {
				t.Fatalf("stat failure prevented the preceding upload: report %+v, uploads %d, error %v", r, f.c.Count("UploadFile"), err)
			}
			if progress && (last.Files != 1 || last.FilesTotal != 2 || last.Bytes != int64(len(epub))) {
				t.Fatalf("progress after stat failure: %+v", last)
			}
		})
	}
}

func observeProgress(t *testing.T, last *port.SyncProgress) func(port.SyncProgress) {
	t.Helper()
	return func(p port.SyncProgress) {
		if p.Bytes < last.Bytes || p.Files < last.Files || p.Bytes > p.BytesTotal || p.Files > p.FilesTotal {
			t.Errorf("progress regressed: %+v -> %+v", *last, p)
		}
		*last = p
	}
}

func TestUploadProgressConcurrentAndIncremental(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("src/A.epub", []byte(epub))
	f.files.Write("src/B.epub", []byte(epub))
	for pass := range 2 {
		var last port.SyncProgress
		r, err := f.s.Upload(t.Context(), "src", "/Books", port.UploadOptions{Jobs: 8, Incremental: pass > 0, Progress: observeProgress(t, &last)})
		if err != nil || last.Files != r.Written+r.Skipped || last.Files != 2 || last.Bytes != 2*int64(len(epub)) || last.Bytes != last.BytesTotal {
			t.Fatalf("upload %+v %+v %v", r, last, err)
		}
	}
}

func TestRestoreProgressAcrossCommitRetries(t *testing.T) {
	t.Parallel()
	f := restoreFixture(t)
	f.c.Conflicts = 2
	var last port.SyncProgress
	r, err := f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{Progress: observeProgress(t, &last)})
	if err != nil || last.Files != len(r.Documents) || last.Files != last.FilesTotal || last.Bytes <= 0 || last.Bytes != last.BytesTotal {
		t.Fatalf("restore %+v %+v %v", r, last, err)
	}
	calls := 0
	_, err = f.s.RestoreBackup(t.Context(), "bk", port.RestoreOptions{DryRun: true, Progress: func(port.SyncProgress) { calls++ }})
	if err != nil || calls != 0 {
		t.Fatalf("dry run progress %d %v", calls, err)
	}
}

func TestUploadFailureProgress(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.files.Write("src/A.epub", []byte(epub))
	f.c.Fail["UploadFile"] = errBoom
	var last port.SyncProgress
	_, err := f.s.Upload(t.Context(), "src", "/Books", port.UploadOptions{Progress: observeProgress(t, &last)})
	if err == nil || last.Files != 0 || last.FilesTotal != 1 {
		t.Fatalf("failed upload %+v %v", last, err)
	}
}
