// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestSyncsKeepTheMark(t *testing.T) {
	t.Parallel()
	st := store(t)
	s := indexcache.NewSyncs(st, nil)
	if _, ok := s.LastSync(); ok {
		t.Fatal("a mark on an empty cache")
	}
	want := port.SyncMark{
		Root: "r", Generation: 7, Query: "/Books", Path: "/Books", Scope: "f1", Files: true, All: true,
		Documents: 3, FileCount: 9, Needed: 1234, At: time.Unix(1_700_000_000, 0).UTC(),
	}
	s.MarkSync(want)
	// Another process reads what this one recorded.
	got, ok := indexcache.NewSyncs(st, nil).LastSync()
	if !ok || got != want {
		t.Fatalf("%+v %v", got, ok)
	}
	s.DropSync()
	if _, ok := s.LastSync(); ok {
		t.Fatal("a dropped mark was read")
	}
	st.PutEntry(blobcache.SpaceState, "sync", []byte("{not json"))
	if _, ok := s.LastSync(); ok {
		t.Fatal("a damaged mark was read")
	}
	if _, ok := st.Entry(blobcache.SpaceState, "sync"); ok {
		t.Fatal("a damaged mark was kept")
	}
	// The mark goes with the rest of the cache.
	s.MarkSync(want)
	if err := st.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastSync(); ok {
		t.Fatal("a cleared cache kept its mark")
	}
}

func TestSyncsFillAndMeasure(t *testing.T) {
	t.Parallel()
	st := store(t)
	s := indexcache.NewSyncs(st, nil)
	data := []byte("a page")
	h := hashOf(data)
	if s.Holds(h) {
		t.Fatal("held on an empty cache")
	}
	if err := s.Fill(h, func(w io.Writer) error { _, err := w.Write(data); return err }); err != nil {
		t.Fatal(err)
	}
	if !s.Holds(h) || s.Usage() != int64(len(data)) || s.Budget() != 1<<20 {
		t.Fatalf("held %v, usage %d, budget %d", s.Holds(h), s.Usage(), s.Budget())
	}
	index := []byte(`{"v":1}`)
	st.PutEntry(blobcache.SpaceIndexes, hashOf(index), index)
	if n, ok := s.IndexSize(hashOf(index)); !ok || n != int64(len(index)) {
		t.Fatalf("%d %v", n, ok)
	}
	if _, ok := s.IndexSize(h); ok {
		t.Fatal("a blob is not an index")
	}
}

func TestBlobsReadTheCacheFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store(t)
	cloud := porttest.NewCloud()
	pdf := cloud.Put("d.pdf", []byte("%PDF the original"))
	page := cloud.Put("d/p1.rm", []byte("a page"))
	if err := st.Fill(pdf.Hash, func(w io.Writer) error { _, err := w.Write([]byte("%PDF the original")); return err }); err != nil {
		t.Fatal(err)
	}
	st.Put(page.Hash, []byte("a page"))
	online := indexcache.NewBlobs(cloud, st)
	var buf bytes.Buffer
	var done, total int64
	if err := online.Download(ctx, pdf, &buf, func(d, t int64) { done, total = d, t }); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "%PDF the original" || done != pdf.Size || total != pdf.Size {
		t.Fatalf("%q %d/%d", buf.String(), done, total)
	}
	if data, err := online.Read(ctx, page); err != nil || string(data) != "a page" {
		t.Fatal(string(data), err)
	}
	if cloud.Count("Download") != 0 || cloud.Count("Read") != 0 {
		t.Fatalf("the cloud was asked for a cached file: %v", cloud.Calls())
	}
	// What the cache lacks comes from the cloud, and is not kept.
	other := cloud.Put("e.pdf", []byte("%PDF another"))
	buf.Reset()
	if err := online.Download(ctx, other, &buf, nil); err != nil || buf.String() != "%PDF another" || cloud.Count("Download") != 1 {
		t.Fatal(err)
	}
	if _, ok := st.Get(other.Hash); ok {
		t.Fatal("a download was kept")
	}

	// Offline, the cache is all there is; a missing file names itself.
	off := indexcache.NewBlobs(indexcache.OfflineBlobs{}, st)
	buf.Reset()
	if err := off.Download(ctx, pdf, &buf, nil); err != nil || buf.String() != "%PDF the original" {
		t.Fatal(err)
	}
	err := off.Download(ctx, other, io.Discard, nil)
	if !errors.Is(err, port.ErrOffline) || err.Error() != "e.pdf is not in the cache: "+port.ErrOffline.Error() {
		t.Fatal(err)
	}
	if _, err := off.Read(ctx, domain.File{Name: "x.rm", Hash: hashOf([]byte("x"))}); !errors.Is(err, port.ErrOffline) {
		t.Fatal(err)
	}
	if _, err := off.Upload(ctx, "x.metadata", []byte("{}")); !errors.Is(err, port.ErrOffline) {
		t.Fatal(err)
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errBoom }

var errBoom = errors.New("boom")

func TestBlobsDownloadFailures(t *testing.T) {
	t.Parallel()
	st := store(t)
	data := []byte("cached bytes")
	f := domain.File{Name: "a.pdf", Hash: hashOf(data), Size: int64(len(data))}
	st.Put(f.Hash, data)
	b := indexcache.NewBlobs(indexcache.OfflineBlobs{}, st)
	if err := b.Download(context.Background(), f, failingWriter{}, nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Download(ctx, f, io.Discard, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNewSyncsNeedsAStore(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	indexcache.NewSyncs(nil, nil)
}
