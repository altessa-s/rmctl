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
		Removals: "3-0123456789abcdef",
	}
	if g, ok := s.Removals(); !ok || g != blobcache.NoRemovals {
		t.Fatalf("a fresh cache: %q %v", g, ok)
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

// The sync cache reports the profile's own budget and what set it, as sync
// answers them.
func TestSyncsReportTheProfilesBudget(t *testing.T) {
	t.Parallel()
	root := blobcache.NewRoot(t.TempDir(), func(profile string) blobcache.Limit {
		if profile == "archive" {
			return blobcache.Limit{Source: port.BudgetFromProfile}
		}
		return blobcache.Limit{Bytes: 64 << 20, Source: port.BudgetFromConfig}
	}, porttest.NewClock(time.Unix(1_700_000_000, 0)), nil)
	for profile, want := range map[string]struct {
		bytes  int64
		source string
	}{
		"archive": {0, port.BudgetFromProfile},
		"work":    {64 << 20, port.BudgetFromConfig},
	} {
		st, err := root.Profile(profile)
		if err != nil {
			t.Fatal(err)
		}
		s := indexcache.NewSyncs(st, nil)
		if s.Budget() != want.bytes || s.BudgetSource() != want.source {
			t.Errorf("%s: %d %q", profile, s.Budget(), s.BudgetSource())
		}
	}
}

// The mark is the entry the cache drops whenever it evicts: after an
// eviction, the last complete sync is forgotten, and the next sync walks.
// Reopened under a lower budget, the cache is within it, and forgets it
// too, before any command reads it.
func TestSyncsForgetTheMarkOnEviction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	open := func(budget int64) (*blobcache.Cache, *indexcache.Syncs) {
		t.Helper()
		st, err := blobcache.NewRoot(dir, blobcache.Fixed(budget), clk, nil).Profile("work")
		if err != nil {
			t.Fatal(err)
		}
		return st, indexcache.NewSyncs(st, nil)
	}
	st, s := open(100)
	mark := port.SyncMark{Root: "r", Query: "/", Path: "/", Scope: domain.RootID, At: time.Unix(1_700_000_000, 0).UTC()}
	keep := func(data []byte) {
		t.Helper()
		clk.Advance(time.Second)
		if err := s.Fill(hashOf(data), func(w io.Writer) error { _, err := w.Write(data); return err }); err != nil {
			t.Fatal(err)
		}
	}
	keep(bytes.Repeat([]byte("a"), 60))
	s.MarkSync(mark)
	keep(bytes.Repeat([]byte("b"), 30))
	if _, ok := s.LastSync(); !ok {
		t.Fatal("a fill within the budget forgot the mark")
	}
	keep(bytes.Repeat([]byte("c"), 30))
	if _, ok := s.LastSync(); ok || s.Usage() > st.Budget() {
		t.Fatalf("an eviction kept the mark, or the cache takes %d", s.Usage())
	}
	if g, ok := s.Removals(); !ok || g == blobcache.NoRemovals {
		t.Fatalf("an eviction left the generation: %q %v", g, ok)
	}
	s.MarkSync(mark)
	st, s = open(40)
	if _, ok := s.LastSync(); ok || s.Usage() > 40 || s.Usage() != 30 {
		t.Fatalf("the reopened cache takes %d", s.Usage())
	}
	// The report agrees.
	report, err := indexcache.NewAdmin(profiles{blobcache.NewRoot(dir, blobcache.Fixed(40), clk, nil)},
		indexcache.Settings{Profile: "work"}, nil).Status(t.Context(), false)
	if err != nil || report.Profiles[0].Bytes > report.Profiles[0].Budget || st.Budget() != 40 {
		t.Fatalf("%+v %v", report, err)
	}
}
