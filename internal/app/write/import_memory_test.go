// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/localfs"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// bigArchive is an archive whose entries are buffers made beforehand, so
// what an import allocates is its own.
type bigArchive struct{ files []domain.ArchiveFile }

func (b bigArchive) OpenArchive(string) (port.Archive, error) { return b, nil }
func (bigArchive) Close() error                               { return nil }

func (b bigArchive) Entries() []domain.ArchiveEntry {
	out := make([]domain.ArchiveEntry, len(b.files))
	for i, f := range b.files {
		out[i] = domain.ArchiveEntry{Name: f.Name, Size: int64(len(f.Data)), DocumentID: "big"}
	}
	return out
}

func (b bigArchive) Read(keep func(domain.ArchiveEntry) bool, body func(domain.ArchiveEntry, []byte) error) error {
	for i, e := range b.Entries() {
		if keep != nil && !keep(e) {
			continue
		}
		if err := body(e, b.files[i].Data); err != nil {
			return err
		}
	}
	return nil
}

// digest hashes a local file as a stream: the cloud double keeps the
// digest instead of the file.
func digest(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- a staging file of the test's own import
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(h.Sum(nil))), nil
}

// An import holds at most one entry of an archive at a time: a large page
// goes to disk as the archive hands it over, and up from there, without a
// copy in memory. Not parallel: the allocation count is the process's.
func TestImportKeepsALargeEntryOutOfMemory(t *testing.T) {
	const size = 64 << 20
	page := make([]byte, size)
	for i := range page {
		page[i] = byte(i)
	}
	sum := sha256.Sum256(page)
	arc := bigArchive{files: []domain.ArchiveFile{
		{Name: "big.metadata", Data: []byte(`{"visibleName":"Big","type":"DocumentType"}`)},
		{Name: "big.content", Data: []byte(`{"fileType":"notebook"}`)},
		{Name: "big/p1.rm", Data: page},
	}}
	c := porttest.NewCloud()
	c.ReadLocal = digest
	s := write.New(library.New(c, c, c), c, c, localfs.New(), porttest.PDF{}, arc, porttest.NewIDs("big"), porttest.NewClock(porttest.Epoch))

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := s.Import(t.Context(), "big.rmdoc", "")
	runtime.ReadMemStats(&after)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Fatalf("the import allocated %d bytes for a %d-byte entry", allocated, size)
	}
	f, ok := got[0].Item.File("rm")
	if !ok {
		t.Fatalf("files %+v", got[0].Item.Files)
	}
	if body, ok := c.Blob(f.Hash); !ok || string(body) != hex.EncodeToString(sum[:]) {
		t.Fatal("the page did not go up whole")
	}
}
