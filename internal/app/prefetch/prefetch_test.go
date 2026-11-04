// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package prefetch_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/prefetch"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

// fixture is a cloud with a folder of documents, one in the trash, one
// outside the folder, and a cache shared by every sync of a test: each
// sync is a new service, as each run of rmctl is.
type fixture struct {
	cloud *porttest.Cloud
	// stores are the cloud's roots and indexes read through the cache.
	stores *indexed
	cache  *porttest.SyncCache
	// blobs is the store syncs download from; the cloud unless a test
	// wraps it.
	blobs port.BlobStore
	files map[string]domain.File
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	c := porttest.NewCloud()
	pagedata := c.Put("d1aaaaaa.pagedata", []byte("Blank\n"))
	c.Add(
		porttest.Doc{ID: "f1aaaaaa", Name: "Books", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF ONE"), RM: true, Extra: []domain.File{pagedata}},
		porttest.Doc{ID: "d2aaaaaa", Name: "Notes", Parent: "f1aaaaaa", Pages: [][]byte{[]byte("page one"), []byte("page two")}},
		porttest.Doc{ID: "f2aaaaaa", Name: "Sub", Parent: "f1aaaaaa", Folder: true},
		porttest.Doc{ID: "d3aaaaaa", Name: "Deep", Parent: "f2aaaaaa", Ext: "epub", Body: []byte("EPUB DEEP")},
		porttest.Doc{ID: "d4aaaaaa", Name: "Old", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF OLD"), RM: true, Trashed: true},
		porttest.Doc{ID: "d5aaaaaa", Name: "Novel", Ext: "pdf", Body: []byte("PDF NOVEL"), RM: true},
	)
	cache := porttest.NewSyncCache(1 << 20)
	f := &fixture{cloud: c, stores: &indexed{Cloud: c, cache: cache}, cache: cache, blobs: c, files: map[string]domain.File{}}
	tree := f.tree(t)
	for _, it := range tree.Items() {
		for _, file := range it.Files {
			f.files[file.Name] = file
		}
	}
	return f
}

func (f *fixture) tree(t *testing.T) *domain.Tree {
	t.Helper()
	tree, _, err := library.New(f.cloud, f.cloud, f.cloud).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// service reads the items one at a time, so a test's hooks run in order.
func (f *fixture) service() *prefetch.Service {
	lib := library.New(f.stores, f.stores, f.cloud, library.WithCache(f.cache), library.WithConcurrency(1))
	return prefetch.New(f.stores, lib, f.blobs, f.cache, porttest.NewClock(porttest.Epoch))
}

// indexSize is what each index takes in the cache.
const indexSize = 64

// indexed reads the cloud's roots and indexes through the cache, as
// indexcache does: each index read is kept. after, when set, runs after
// each index of a document is read, as an eviction would.
type indexed struct {
	*porttest.Cloud
	cache *porttest.SyncCache
	after func(rec domain.Record)
}

func (x *indexed) Root(ctx context.Context) (port.RootState, error) {
	state, err := x.Cloud.Root(ctx)
	if err == nil && state.Hash != "" {
		x.cache.KeepIndex(state.Hash, indexSize)
	}
	return state, err
}

func (x *indexed) Files(ctx context.Context, rec domain.Record) ([]domain.File, error) {
	files, err := x.Cloud.Files(ctx, rec)
	if err != nil {
		return nil, err
	}
	x.cache.KeepIndex(rec.Hash, indexSize)
	if x.after != nil {
		x.after(rec)
	}
	return files, nil
}

// heldIndexes reports whether the cache holds the root index and the index
// of every record.
func (f *fixture) heldIndexes(t *testing.T) bool {
	t.Helper()
	state, err := f.cloud.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.cache.IndexSize(state.Hash); !ok {
		return false
	}
	for _, rec := range state.Records {
		if _, ok := f.cache.IndexSize(rec.Hash); !ok {
			return false
		}
	}
	return true
}

func (f *fixture) sync(t *testing.T, query string, opts port.SyncOptions) port.SyncReport {
	t.Helper()
	r, err := f.service().Sync(t.Context(), query, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// holds reports which of names the cache holds.
func (f *fixture) holds(t *testing.T, names ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, n := range names {
		file, ok := f.files[n]
		if !ok {
			t.Fatalf("no file %s", n)
		}
		out[n] = f.cache.Holds(file.Hash)
	}
	return out
}

func TestSyncKeepsWhatThePageCommandsRead(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := f.sync(t, "/", port.SyncOptions{})
	got := f.holds(t, "d1aaaaaa/page-1.rm", "d1aaaaaa.pagedata", "d2aaaaaa/p1.rm", "d2aaaaaa/p2.rm", "d5aaaaaa/page-1.rm",
		"d1aaaaaa.pdf", "d3aaaaaa.epub", "d5aaaaaa.pdf", "d4aaaaaa/page-1.rm", "d4aaaaaa.metadata")
	want := map[string]bool{
		"d1aaaaaa/page-1.rm": true, "d1aaaaaa.pagedata": true, "d2aaaaaa/p1.rm": true, "d2aaaaaa/p2.rm": true, "d5aaaaaa/page-1.rm": true,
		// No original without --files, and no page of the trash without
		// --all; the metadata of the trash is read for the tree anyway.
		"d1aaaaaa.pdf": false, "d3aaaaaa.epub": false, "d5aaaaaa.pdf": false, "d4aaaaaa/page-1.rm": false, "d4aaaaaa.metadata": true,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s held %v, want %v", name, got[name], w)
		}
	}
	if r.Documents != 4 || r.Fetched != 5 || r.Bytes == 0 || r.Current || r.Path != "/" || r.Budget != 1<<20 {
		t.Fatalf("report %+v", r)
	}
	// The .metadata and .content of every item were read for the tree and
	// count as cached: 7 items, 14 files, of which the two folders'
	// .content and two documents' are one blob each.
	if r.Cached != 12 || r.Needed == 0 || r.CacheBytes != f.cache.Usage() {
		t.Fatalf("report %+v", r)
	}
	if m, ok := f.cache.LastSync(); !ok || m.Root == "" || m.Scope != domain.RootID || m.Files || m.Documents != 4 || !m.At.Equal(porttest.Epoch) {
		t.Fatalf("mark %+v %v", m, ok)
	}
}

func TestSyncFilesKeepsOriginals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := f.sync(t, "/Books", port.SyncOptions{Files: true})
	got := f.holds(t, "d1aaaaaa.pdf", "d3aaaaaa.epub", "d1aaaaaa/page-1.rm", "d5aaaaaa.pdf", "d5aaaaaa/page-1.rm", "d4aaaaaa.pdf")
	want := map[string]bool{
		"d1aaaaaa.pdf": true, "d3aaaaaa.epub": true, "d1aaaaaa/page-1.rm": true,
		// Outside the path, and in the trash.
		"d5aaaaaa.pdf": false, "d5aaaaaa/page-1.rm": false, "d4aaaaaa.pdf": false,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s held %v, want %v", name, got[name], w)
		}
	}
	if r.Path != "/Books" || r.Documents != 3 {
		t.Fatalf("report %+v", r)
	}
}

func TestSyncAllTakesTheTrash(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := f.sync(t, "", port.SyncOptions{All: true, Files: true})
	if got := f.holds(t, "d4aaaaaa.pdf", "d4aaaaaa/page-1.rm"); !got["d4aaaaaa.pdf"] || !got["d4aaaaaa/page-1.rm"] {
		t.Fatalf("the trash was not synced: %v", got)
	}
	if r.Documents != 5 {
		t.Fatalf("report %+v", r)
	}
	// A document in the trash is not found without --all.
	if _, err := f.service().Sync(t.Context(), "d4aaaaaa", port.SyncOptions{Force: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if r, err := f.service().Sync(t.Context(), "d4aaaaaa", port.SyncOptions{All: true, Force: true}); err != nil || r.Documents != 1 {
		t.Fatal(r, err)
	}
}

func TestSyncOfOneDocument(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r := f.sync(t, "d5aaaaaa", port.SyncOptions{Files: true})
	if r.Documents != 1 || r.Path != "/Novel" || !f.holds(t, "d5aaaaaa.pdf")["d5aaaaaa.pdf"] || f.holds(t, "d1aaaaaa.pdf")["d1aaaaaa.pdf"] {
		t.Fatalf("report %+v", r)
	}
}

func TestSyncSkipsWhatTheCacheHolds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.sync(t, "/", port.SyncOptions{Files: true})
	downloads := f.cloud.Count("Download")
	again := f.sync(t, "/", port.SyncOptions{Files: true, Force: true})
	if f.cloud.Count("Download") != downloads || again.Fetched != 0 || again.Bytes != 0 {
		t.Fatalf("a held file was fetched again: %+v", again)
	}
	if again.Cached != first.Cached+first.Fetched || !again.Current {
		t.Fatalf("first %+v, again %+v", first, again)
	}
	// One file the cache lost is the only one fetched.
	f.cache.Drop(f.files["d1aaaaaa.pdf"].Hash)
	third := f.sync(t, "/", port.SyncOptions{Files: true, Force: true})
	if third.Fetched != 1 || third.Bytes != int64(len("PDF ONE")) || f.cloud.Count("Download") != downloads+1 {
		t.Fatalf("third %+v", third)
	}
}

func TestSyncRefusesWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.cache.Max, f.cache.Source = 64, port.BudgetFromProfile
	r, err := f.service().Sync(t.Context(), "/", port.SyncOptions{Files: true})
	over, ok := errors.AsType[*port.OverBudgetError](err)
	if !ok || over.Budget != 64 || over.Needed <= 64 || over.Needed != r.Needed || over.Source != port.BudgetFromProfile {
		t.Fatalf("%+v %v", r, err)
	}
	if f.cloud.Count("Download") != 0 || f.cache.Fills() != 0 {
		t.Fatal("a sync that does not fit downloaded something")
	}
	if _, ok := f.cache.LastSync(); ok {
		t.Fatal("a refused sync was recorded")
	}
	// The indexes count too.
	f.cache.Max = 1 << 20
	for _, rec := range f.cloud.Records() {
		f.cache.Indexes[rec.Hash] = 1 << 18
	}
	if _, err := f.service().Sync(t.Context(), "/", port.SyncOptions{}); !errors.As(err, &over) {
		t.Fatalf("the indexes were not counted: %v", err)
	}
}

// A budget of 0 is no limit: however much a sync keeps, it is never
// refused, and it reports the budget and what set it.
func TestSyncUnderNoLimitNeverRefuses(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.cache.Max, f.cache.Source = 0, port.BudgetFromEnv
	for _, rec := range f.cloud.Records() {
		f.cache.Indexes[rec.Hash] = 1 << 40
	}
	r, err := f.service().Sync(t.Context(), "/", port.SyncOptions{Files: true, All: true})
	if err != nil || r.Needed < 1<<40 || r.Fetched == 0 || r.Budget != 0 || r.BudgetSource != port.BudgetFromEnv {
		t.Fatalf("%+v %v", r, err)
	}
	again, err := f.service().Sync(t.Context(), "/", port.SyncOptions{Files: true, All: true})
	if err != nil || !again.Current || again.Budget != 0 || again.BudgetSource != port.BudgetFromEnv {
		t.Fatalf("%+v %v", again, err)
	}
}

// cancelling cancels the sync at its second download, as Ctrl-C would.
type cancelling struct {
	port.BlobStore
	cancel context.CancelFunc
	n      atomic.Int32
}

func (c *cancelling) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	if c.n.Add(1) > 1 {
		c.cancel()
		return ctx.Err()
	}
	return c.BlobStore.Download(ctx, f, w, progress)
}

func TestSyncStoppedKeepsWhatItFetched(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.blobs = &cancelling{BlobStore: f.cloud, cancel: cancel}
	r, err := f.service().Sync(ctx, "/", port.SyncOptions{Files: true, Jobs: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.Fetched != 1 || f.cache.Fills() != 1 {
		t.Fatalf("report %+v, fills %d", r, f.cache.Fills())
	}
	if _, ok := f.cache.LastSync(); ok {
		t.Fatal("a stopped sync was recorded")
	}
	// The next sync walks, and fetches only the rest.
	f.blobs = f.cloud
	next := f.sync(t, "/", port.SyncOptions{Files: true})
	if next.Current || next.Fetched == 0 || f.cache.Fills() != 1+next.Fetched {
		t.Fatalf("next %+v", next)
	}
	if _, ok := f.cache.LastSync(); !ok {
		t.Fatal("a complete sync was not recorded")
	}
}

func TestSyncFailureIsNotRecorded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.cloud.Fail["Download"] = errBoom
	if _, err := f.service().Sync(t.Context(), "/", port.SyncOptions{}); !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "download ") {
		t.Fatal(err)
	}
	if _, ok := f.cache.LastSync(); ok {
		t.Fatal("a failed sync was recorded")
	}
	delete(f.cloud.Fail, "Download")
	f.cache.FailFill = errBoom
	if _, err := f.service().Sync(t.Context(), "/", port.SyncOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	f.cloud.Fail["Root"] = errBoom
	if _, err := f.service().Sync(t.Context(), "/", port.SyncOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// gated counts the downloads in flight and lets them finish only once
// want of them are, or the wait times out.
type gated struct {
	port.BlobStore
	want     int32
	inFlight atomic.Int32
	peak     atomic.Int32
	once     sync.Once
	release  chan struct{}
}

func (g *gated) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	n := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if n >= g.want {
		g.once.Do(func() { close(g.release) })
	}
	select {
	case <-g.release:
	case <-time.After(5 * time.Second):
		return errors.New("fewer downloads at once than asked for")
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.BlobStore.Download(ctx, f, w, progress)
}

func TestSyncRunsJobsAtOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	g := &gated{BlobStore: f.cloud, want: 3, release: make(chan struct{})}
	f.blobs = g
	if _, err := f.service().Sync(t.Context(), "/", port.SyncOptions{Files: true, Jobs: 3}); err != nil {
		t.Fatal(err)
	}
	if g.peak.Load() != 3 {
		t.Fatalf("peak %d, want 3", g.peak.Load())
	}
}

func TestSyncProgress(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var (
		mu   sync.Mutex
		seen []port.SyncProgress
	)
	r := f.sync(t, "/", port.SyncOptions{Files: true, Progress: func(p port.SyncProgress) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, p)
	}})
	last := seen[len(seen)-1]
	if seen[0].Files != 0 || seen[0].Bytes != 0 || last.Files != r.Fetched || last.FilesTotal != r.Fetched || last.Bytes != r.Bytes || last.BytesTotal != r.Bytes {
		t.Fatalf("progress %+v, report %+v", seen, r)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].Bytes < seen[i-1].Bytes || seen[i].Files < seen[i-1].Files {
			t.Fatalf("progress went back: %+v", seen)
		}
	}
}

func TestSecondSyncAtTheSameRootIsAtOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.sync(t, "/", port.SyncOptions{Files: true})
	calls := len(f.cloud.Calls())
	for _, tc := range []struct {
		query string
		opts  port.SyncOptions
	}{
		{"/", port.SyncOptions{Files: true}},
		{"/", port.SyncOptions{}},
		// Narrower: any path below the whole library.
		{"/Books", port.SyncOptions{}},
		{"d5aaaaaa", port.SyncOptions{Files: true}},
	} {
		r := f.sync(t, tc.query, tc.opts)
		if !r.Current || r.Fetched != 0 || r.Documents != first.Documents || r.Path != "/" {
			t.Fatalf("%s: %+v", tc.query, r)
		}
		// One request: the root.
		if got := f.cloud.Calls()[calls:]; len(got) != 1 || got[0] != "Root" {
			t.Fatalf("%s: %v", tc.query, got)
		}
		calls++
	}
	// Wider than the last one walks: the trash.
	if r := f.sync(t, "/", port.SyncOptions{Files: true, All: true}); r.Current || r.Fetched == 0 {
		t.Fatalf("%+v", r)
	}
	// --force walks.
	before := f.cloud.Count("Files")
	if r := f.sync(t, "/", port.SyncOptions{Force: true}); !r.Current || f.cloud.Count("Files") == before {
		t.Fatalf("--force did not walk: %+v", r)
	}
}

func TestSyncOfAFolderCoversOnlyItsOwnPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sync(t, "/Books", port.SyncOptions{})
	if r := f.sync(t, "/Books", port.SyncOptions{}); !r.Current || r.Path != "/Books" {
		t.Fatalf("%+v", r)
	}
	before := f.cloud.Count("Files")
	// Below it: a walk, from the cache, that fetches nothing and keeps the
	// wider record.
	if r := f.sync(t, "/Books/Sub", port.SyncOptions{}); !r.Current || r.Fetched != 0 || f.cloud.Count("Files") == before {
		t.Fatalf("%+v", r)
	}
	if m, _ := f.cache.LastSync(); m.Path != "/Books" {
		t.Fatalf("mark %+v", m)
	}
	// Outside it: fetched.
	if r := f.sync(t, "/Novel", port.SyncOptions{}); r.Current || r.Fetched != 1 {
		t.Fatalf("%+v", r)
	}
	if m, _ := f.cache.LastSync(); m.Path != "/Novel" {
		t.Fatalf("mark %+v", m)
	}
}

func TestSyncAfterAChangeFetchesOnlyWhatChanged(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sync(t, "/", port.SyncOptions{Files: true})
	downloads := f.cloud.Count("Download")
	f.cloud.Add(porttest.Doc{ID: "d6aaaaaa", Name: "New", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("PDF NEW"), RM: true})
	r := f.sync(t, "/", port.SyncOptions{Files: true})
	// The new document's original and page; its metadata and content were
	// read for the tree.
	if r.Current || r.Fetched != 2 || f.cloud.Count("Download") != downloads+2 || r.Documents != 5 {
		t.Fatalf("%+v", r)
	}
	if m, _ := f.cache.LastSync(); m.Generation != f.cloud.Generation() {
		t.Fatalf("mark %+v", m)
	}
}

// An index the build of the tree evicted, under a budget too tight for
// all it read at once, is read again and counted: the sync that is recorded
// holds every index an offline read builds the tree from.
func TestSyncRereadsAnIndexTheBuildEvicted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	records := f.cloud.Records()
	first, last := records[0], records[len(records)-1]
	var once sync.Once
	f.stores.after = func(rec domain.Record) {
		if rec.Hash == last.Hash {
			once.Do(func() { f.cache.DropIndex(first.Hash) })
		}
	}
	before := f.cloud.Count("Files")
	r := f.sync(t, "/", port.SyncOptions{})
	if f.cloud.Count("Files") != before+len(records)+1 {
		t.Fatalf("the evicted index was not read again: %d reads", f.cloud.Count("Files")-before)
	}
	if !f.heldIndexes(t) {
		t.Fatal("an index the tree is built from is not in the cache")
	}
	if _, ok := f.cache.LastSync(); !ok {
		t.Fatal("a complete sync was not recorded")
	}
	// Every index counts: the root's and one per record.
	var blobs int64
	for _, h := range f.cacheBlobs(t) {
		blobs += h
	}
	if want := blobs + int64(len(records)+1)*indexSize; r.Needed != want {
		t.Fatalf("needed %d, want %d", r.Needed, want)
	}
}

// cacheBlobs are the sizes of the blobs the cache holds, by hash.
func (f *fixture) cacheBlobs(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, file := range f.files {
		if f.cache.Holds(file.Hash) {
			out[file.Hash] = file.Size
		}
	}
	return out
}

// The root index the build evicted is read again, at the same root.
func TestSyncRereadsTheRootIndex(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	state, err := f.cloud.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	f.stores.after = func(domain.Record) { once.Do(func() { f.cache.DropIndex(state.Hash) }) }
	roots := f.cloud.Count("Root")
	f.sync(t, "/", port.SyncOptions{})
	// The root of the sync, then its index again.
	if got := f.cloud.Count("Root") - roots; got != 2 || !f.heldIndexes(t) {
		t.Fatalf("%d roots read, indexes held %v", got, f.heldIndexes(t))
	}
	if _, ok := f.cache.LastSync(); !ok {
		t.Fatal("a complete sync was not recorded")
	}
}

// Reading an evicted index again fails as the read fails, and a root that
// moved meanwhile stops the sync: its tree is not the live one.
func TestSyncRereadFailures(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		root bool
		set  func(f *fixture)
		want error
	}{
		"index": {set: func(f *fixture) { f.cloud.Fail["Files"] = errBoom }, want: errBoom},
		"root":  {root: true, set: func(f *fixture) { f.cloud.Fail["Root"] = errBoom }, want: errBoom},
		"moved": {root: true, set: func(f *fixture) {
			f.cloud.Add(porttest.Doc{ID: "d9aaaaaa", Name: "Later", Ext: "pdf"})
		}},
	} {
		f := newFixture(t)
		state, err := f.cloud.Root(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		last := state.Records[len(state.Records)-1]
		f.stores.after = func(rec domain.Record) {
			if rec.Hash != last.Hash {
				return
			}
			f.stores.after = nil
			if c.root {
				f.cache.DropIndex(state.Hash)
			} else {
				f.cache.DropIndex(state.Records[0].Hash)
			}
			c.set(f)
		}
		_, err = f.service().Sync(t.Context(), "/", port.SyncOptions{})
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) || (c.want == nil && !strings.Contains(err.Error(), "changed")) {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := f.cache.LastSync(); ok || f.cache.Fills() != 0 {
			t.Fatalf("%s: recorded or fetched", name)
		}
	}
}

// An index that does not stay in the cache, even read again, makes the
// sync too large for the budget: nothing is downloaded or recorded.
func TestSyncWhoseIndexDoesNotStayIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	gone := f.cloud.Records()[2]
	f.stores.after = func(rec domain.Record) {
		if rec.Hash == gone.Hash {
			f.cache.DropIndex(rec.Hash)
		}
	}
	r, err := f.service().Sync(t.Context(), "/", port.SyncOptions{})
	over, ok := errors.AsType[*port.OverBudgetError](err)
	if !ok || !over.Evicted || over.Budget != 1<<20 || over.Needed != r.Needed || !strings.Contains(err.Error(), "at least") {
		t.Fatalf("%+v %v", r, err)
	}
	if f.cache.Fills() != 0 || f.cloud.Count("Download") != 0 {
		t.Fatal("a refused sync downloaded")
	}
	if _, ok := f.cache.LastSync(); ok {
		t.Fatal("a refused sync was recorded")
	}
}

// evicting drops a blob the sync keeps once the downloads are done, as a
// cache evicting under its budget would.
type evicting struct {
	port.BlobStore
	cache *porttest.SyncCache
	hash  string
}

func (e *evicting) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	if err := e.BlobStore.Download(ctx, f, w, progress); err != nil {
		return err
	}
	e.cache.Drop(e.hash)
	return nil
}

// A sync whose files did not all stay in the cache is not recorded: an
// offline read after it would not find them. Under a budget it is too
// large for it; under none, something else removed them.
func TestSyncThatDidNotStayIsNotRecorded(t *testing.T) {
	t.Parallel()
	for _, budget := range []int64{1 << 20, 0} {
		f := newFixture(t)
		f.cache.Max = budget
		f.blobs = &evicting{BlobStore: f.cloud, cache: f.cache, hash: f.files["d1aaaaaa.metadata"].Hash}
		r, err := f.service().Sync(t.Context(), "/", port.SyncOptions{Jobs: 1})
		over, ok := errors.AsType[*port.OverBudgetError](err)
		switch {
		case budget > 0 && (!ok || !over.Evicted || over.Needed != r.Needed || r.Fetched == 0):
			t.Fatalf("budget %d: %+v %v", budget, r, err)
		case budget == 0 && (ok || err == nil || !strings.Contains(err.Error(), "left the cache")):
			t.Fatalf("budget %d: %+v %v", budget, r, err)
		}
		if _, ok := f.cache.LastSync(); ok {
			t.Fatalf("budget %d: an incomplete sync was recorded", budget)
		}
		// The next sync reads it back, as the tree is built, and is recorded.
		f.blobs = f.cloud
		f.sync(t, "/", port.SyncOptions{})
		if !f.cache.Holds(f.files["d1aaaaaa.metadata"].Hash) {
			t.Fatalf("budget %d: the metadata is not back", budget)
		}
		if _, ok := f.cache.LastSync(); !ok {
			t.Fatalf("budget %d: not recorded", budget)
		}
	}
}

// An eviction after a complete sync forgets it: the next sync walks and
// fetches back what went, instead of answering that the cache is current.
func TestSyncAfterAnEvictionWalks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sync(t, "/", port.SyncOptions{Files: true})
	f.cache.Drop(f.files["d3aaaaaa.epub"].Hash)
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); r.Current || r.Fetched != 1 {
		t.Fatalf("%+v", r)
	}
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); !r.Current {
		t.Fatalf("%+v", r)
	}
	f.cache.DropIndex(f.cloud.Records()[0].Hash)
	before := f.cloud.Count("Files")
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); !r.Current || r.Fetched != 0 || f.cloud.Count("Files") == before || !f.heldIndexes(t) {
		t.Fatalf("%+v", r)
	}
	if _, ok := f.cache.LastSync(); !ok {
		t.Fatal("not recorded")
	}
}

// Another process removes an entry after a sync checked that it kept
// everything, and before it records the mark: the removal forgets the
// mark that was there, and the mark written after it names the generation
// before the removal, so the next sync at the same root walks and fetches
// back what went, instead of answering that the cache is current.
func TestSyncRacingARemovalIsNotTrusted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	epub := f.files["d3aaaaaa.epub"].Hash
	var once sync.Once
	f.cache.BeforeMark = func() { once.Do(func() { f.cache.Drop(epub) }) }
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); r.Current || r.Fetched == 0 {
		t.Fatalf("%+v", r)
	}
	if f.cache.Holds(epub) {
		t.Fatal("the removal did not happen")
	}
	if m, ok := f.cache.LastSync(); !ok || m.Removals != "0" {
		t.Fatalf("the racing sync recorded %+v %v", m, ok)
	}
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); r.Current || r.Fetched != 1 || !f.cache.Holds(epub) {
		t.Fatalf("the next sync trusted a stale mark: %+v", r)
	}
	if m, _ := f.cache.LastSync(); m.Removals != "1" {
		t.Fatalf("mark %+v", m)
	}
	if r := f.sync(t, "/", port.SyncOptions{Files: true}); !r.Current {
		t.Fatalf("%+v", r)
	}
}

// A generation the cache cannot tell vouches for no mark: every sync walks.
func TestSyncWithAnUnknownGenerationWalks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sync(t, "/", port.SyncOptions{})
	f.cache.UnknownRemovals = true
	for range 2 {
		before := f.cloud.Count("Files")
		if r := f.sync(t, "/", port.SyncOptions{}); r.Fetched != 0 || f.cloud.Count("Files") == before {
			t.Fatalf("a sync took the fast path on an unknown generation: %+v", r)
		}
	}
	f.cache.UnknownRemovals = false
	f.sync(t, "/", port.SyncOptions{})
	calls := len(f.cloud.Calls())
	if r := f.sync(t, "/", port.SyncOptions{}); !r.Current || len(f.cloud.Calls()) != calls+1 {
		t.Fatalf("%+v", r)
	}
}

func TestSyncNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, err := f.service().Sync(t.Context(), "/Nothing", port.SyncOptions{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestNewNeedsItsParts(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	lib := library.New(c, c, c)
	cache := porttest.NewSyncCache(1)
	clk := porttest.NewClock(porttest.Epoch)
	for name, build := range map[string]func(){
		"roots": func() { prefetch.New(nil, lib, c, cache, clk) },
		"lib":   func() { prefetch.New(c, nil, c, cache, clk) },
		"blobs": func() { prefetch.New(c, lib, nil, cache, clk) },
		"cache": func() { prefetch.New(c, lib, c, nil, clk) },
		"clock": func() { prefetch.New(c, lib, c, cache, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			build()
		}()
	}
}
