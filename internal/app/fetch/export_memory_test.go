//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"context"
	"crypto/sha256"
	"io"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/localfs"
	"github.com/altessa-s/rmctl/internal/adapter/rmdocs"
	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// bigBlobs serves one blob from a buffer made beforehand, without the copy
// the cloud double makes, so what an export allocates is its own.
type bigBlobs struct {
	*porttest.Cloud
	hash string
	data []byte
}

func (b bigBlobs) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	if f.Hash != b.hash {
		return b.Cloud.Download(ctx, f, w, progress)
	}
	_, err := w.Write(b.data)
	return err
}

// An export holds neither a whole file nor the archive in memory: a large
// original streams to disk, into the archive and into place. Not parallel:
// the allocation count is the process's.
func TestExportKeepsALargeFileOutOfMemory(t *testing.T) {
	const size = 64 << 20
	original := make([]byte, size)
	for i := range original {
		original[i] = byte(i)
	}
	c := porttest.NewCloud()
	c.Add(porttest.Doc{ID: "d1aaaaaa", Name: "Big", Ext: "pdf", Body: []byte("small stand-in")})
	tree, _, err := library.New(c, c, c).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	it, _ := tree.Get("d1aaaaaa")
	pdf, _ := it.Original()
	blobs := bigBlobs{Cloud: c, hash: pdf.Hash, data: original}
	archives := rmdocs.New()
	s := fetch.New(library.New(c, c, c), blobs, localfs.New(), archives, porttest.NewClock(porttest.Epoch))
	dest := filepath.Join(t.TempDir(), "big.rmdoc")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	report, err := s.Export(t.Context(), []string{"d1aaaaaa"}, dest)
	runtime.ReadMemStats(&after)
	if err != nil || report.Files != 3 {
		t.Fatalf("%+v %v", report, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Fatalf("the export allocated %d bytes for a %d-byte file", allocated, size)
	}
	arc, err := archives.OpenArchive(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arc.Close() }()
	want := sha256.Sum256(original)
	var got [sha256.Size]byte
	err = arc.Read(func(e domain.ArchiveEntry) bool { return e.Name == "d1aaaaaa.pdf" }, func(_ domain.ArchiveEntry, data []byte) error {
		got = sha256.Sum256(data)
		return nil
	})
	if err != nil || got != want {
		t.Fatalf("the original did not go into the archive whole: %v", err)
	}
}
