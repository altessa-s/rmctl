// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package prefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultJobs is how many files a sync downloads at once, as many as mget.
const DefaultJobs = 4

// rootPath is the query of the whole library.
const rootPath = "/"

// Service implements port.Syncer.
type Service struct {
	roots port.RootStore
	lib   *library.Library
	blobs port.BlobStore
	cache port.SyncCache
	clock port.Clock
}

var _ port.Syncer = (*Service)(nil)

// New returns the sync use case. roots must read the live root; blobs is
// the cloud's store the files are downloaded from. A nil dependency panics.
func New(roots port.RootStore, lib *library.Library, blobs port.BlobStore, cache port.SyncCache, clk port.Clock) *Service {
	for _, dep := range []struct {
		name    string
		missing bool
	}{
		{"RootStore", roots == nil}, {"Library", lib == nil}, {"BlobStore", blobs == nil},
		{"SyncCache", cache == nil}, {"Clock", clk == nil},
	} {
		if dep.missing {
			panic("prefetch: " + dep.name + " is required")
		}
	}
	return &Service{roots: roots, lib: lib, blobs: blobs, cache: cache, clock: clk}
}

// Sync implements port.Syncer.
func (s *Service) Sync(ctx context.Context, query string, opts port.SyncOptions) (port.SyncReport, error) {
	if query == "" {
		query = rootPath
	}
	state, err := s.roots.Root(ctx)
	if err != nil {
		return port.SyncReport{}, err
	}
	mark, marked := s.cache.LastSync()
	sameRoot := marked && mark.Root == state.Hash
	if sameRoot && !opts.Force && s.unremoved(mark) && covers(mark, query, opts) {
		return s.current(state, mark), nil
	}
	if marked && !sameRoot {
		// What the cache held of another root may go while this one is
		// fetched: no answer may rest on it any more.
		s.cache.DropSync()
	}
	tree, err := s.lib.Build(ctx, state)
	if err != nil {
		return port.SyncReport{}, err
	}
	scope, docs, err := documents(tree, query, opts.All)
	if err != nil {
		return port.SyncReport{}, err
	}
	p, err := s.plan(ctx, state, tree, docs, opts.Files)
	if err != nil {
		return port.SyncReport{}, err
	}
	report := port.SyncReport{
		Path: displayPath(tree, scope), Root: state.Hash, Generation: state.Generation,
		Documents: len(docs), Cached: p.cached, Needed: p.needed,
		Budget: s.cache.Budget(), BudgetSource: s.cache.BudgetSource(),
	}
	// A budget of 0 is no limit: it never refuses.
	if report.Budget > 0 && (p.lost || p.needed > report.Budget) {
		report.CacheBytes = s.cache.Usage()
		return report, s.overBudget(p.needed, p.lost)
	}
	fetched, bytes, err := s.fetch(ctx, p, opts)
	report.Fetched, report.Bytes = fetched, bytes
	report.CacheBytes = s.cache.Usage()
	if err != nil {
		return report, err
	}
	// The removal generation is read before the check: whatever leaves
	// the cache after the check changes it, so a mark recorded with it
	// stops vouching then, even when the removal raced the mark.
	removals, _ := s.cache.Removals()
	// What the cache evicted while the sync ran is not there for a read
	// to find: the sync is not complete.
	if !s.kept(p) {
		report.CacheBytes = s.cache.Usage()
		return report, s.overBudget(p.needed, true)
	}
	report.Current = len(p.fetch) == 0
	// An eviction since this sync began may have forgotten the last mark,
	// and a mark of another generation vouches for nothing any more.
	if mark, marked = s.cache.LastSync(); !marked || mark.Root != state.Hash || mark.Removals != removals ||
		!coversTree(mark, tree, scope, opts) {
		s.cache.MarkSync(port.SyncMark{
			Root: state.Hash, Generation: state.Generation, Query: query, Path: report.Path, Scope: scope,
			Files: opts.Files, All: opts.All, Documents: len(docs), FileCount: p.files, Needed: p.needed,
			At: s.clock.Now(), Removals: removals,
		})
	}
	return report, nil
}

// unremoved reports whether nothing left the cache since mark was recorded:
// the cache's removal generation is still the one mark names. A generation
// that cannot be told vouches for no mark.
func (s *Service) unremoved(mark port.SyncMark) bool {
	removals, ok := s.cache.Removals()
	return ok && mark.Removals == removals
}

// current is the answer of a sync the last complete one covers: what that
// one kept, and what the cache takes now.
func (s *Service) current(state port.RootState, mark port.SyncMark) port.SyncReport {
	return port.SyncReport{
		Current: true, Path: mark.Path, Root: state.Hash, Generation: state.Generation,
		Documents: mark.Documents, Cached: mark.FileCount, Needed: mark.Needed,
		CacheBytes: s.cache.Usage(), Budget: s.cache.Budget(), BudgetSource: s.cache.BudgetSource(),
	}
}

// covers reports whether the last complete sync, at the same root, kept
// everything a sync of query with opts keeps, without building the tree:
// the same query, or the whole library, with as many files and as much of
// the trash. At one root a query names one item, so the same query is the
// same scope.
func covers(mark port.SyncMark, query string, opts port.SyncOptions) bool {
	return (mark.Files || !opts.Files) && (mark.All || !opts.All) &&
		(mark.Query == query || mark.Scope == domain.RootID)
}

// coversTree is covers for a scope resolved in tree.
func coversTree(mark port.SyncMark, tree *domain.Tree, scope domain.ItemID, opts port.SyncOptions) bool {
	return (mark.Files || !opts.Files) && (mark.All || !opts.All) && tree.IsWithin(scope, mark.Scope)
}

// documents are the documents below query, the scope query names
// (RootID for the whole library), and the documents in the trash only with
// all.
func documents(tree *domain.Tree, query string, all bool) (domain.ItemID, []domain.Item, error) {
	scope := domain.RootID
	var below []domain.Item
	if query != rootPath {
		it, found, err := tree.Resolve(query, all)
		switch {
		case err != nil:
			return "", nil, fmt.Errorf("%s: %w", query, err)
		case !found:
			// "/" spelled another way, such as "//".
		case !it.Kind.IsFolder():
			return it.ID, []domain.Item{it}, nil
		default:
			scope = it.ID
		}
	}
	if scope == domain.RootID {
		for _, top := range tree.Children(domain.RootID, true) {
			below = append(below, top)
			below = append(below, tree.Descendants(top.ID)...)
		}
	} else {
		below = tree.Descendants(scope)
	}
	var docs []domain.Item
	for _, it := range below {
		if !it.Kind.IsFolder() && (all || !tree.IsTrashed(it.ID)) {
			docs = append(docs, it)
		}
	}
	return scope, docs, nil
}

func displayPath(tree *domain.Tree, scope domain.ItemID) string {
	if scope == domain.RootID {
		return rootPath
	}
	return tree.Path(scope)
}

// errLost means entries a sync kept left the cache while it ran, under no
// budget: something else removed them.
var errLost = errors.New("files the sync kept left the cache while it ran; run it again")

// errMoved means the library's root moved while a sync read it.
var errMoved = errors.New("the library changed while the sync read it; run it again")

// overBudget is the refusal of a sync that needs needed bytes; evicted says
// part of it did not stay in the cache. Under no budget nothing is
// evicted: what left the cache was removed by something else.
func (s *Service) overBudget(needed int64, evicted bool) error {
	budget := s.cache.Budget()
	if budget == 0 {
		return errLost
	}
	return &port.OverBudgetError{Needed: needed, Budget: budget, Source: s.cache.BudgetSource(), Evicted: evicted}
}

// kept reports whether the cache holds every index and blob p keeps.
func (s *Service) kept(p plan) bool {
	for _, hash := range p.indexes {
		if _, ok := s.cache.IndexSize(hash); !ok {
			return false
		}
	}
	for _, hash := range p.blobs {
		if !s.cache.Holds(hash) {
			return false
		}
	}
	return true
}

// pageKinds are the extensions of the files the page commands read beyond
// .metadata and .content, which every document keeps anyway.
var pageKinds = map[string]bool{"pagedata": true, "rm": true} //nolint:gochecknoglobals // a constant set

// plan is what a sync keeps: the files it downloads, how many files it
// keeps and how many of them the cache already holds, what they and the
// indexes take together, and the hashes of every index and blob it keeps.
// lost says an index the tree is read from does not stay in the cache.
type plan struct {
	fetch   []domain.File
	total   int64
	files   int
	cached  int
	needed  int64
	indexes []string
	blobs   []string
	lost    bool
}

// plan lists what the reads of docs need, as the cache holds it now: every
// index and every .metadata and .content of the library, which every read
// builds the tree from, and the page files of docs, or with files every
// file of their indexes. Each counts once; whatever the cache holds is
// marked used, so it outlives what the cache held before. An index the
// cache lost since the tree was built, evicted by what the build itself
// kept, is read again, as the build reads it, to be kept and measured.
func (s *Service) plan(ctx context.Context, state port.RootState, tree *domain.Tree, docs []domain.Item, files bool) (plan, error) {
	var p plan
	if err := s.planIndexes(ctx, state, &p); err != nil {
		return plan{}, err
	}
	seen := map[string]bool{}
	blob := func(f domain.File) {
		if seen[f.Hash] {
			return
		}
		seen[f.Hash] = true
		p.files++
		p.needed += f.Size
		p.blobs = append(p.blobs, f.Hash)
		if s.cache.Holds(f.Hash) {
			p.cached++
			return
		}
		p.fetch = append(p.fetch, f)
		p.total += f.Size
	}
	for _, it := range tree.Items() {
		for _, ext := range []string{"metadata", "content"} {
			if f, ok := it.File(ext); ok {
				blob(f)
			}
		}
	}
	for _, it := range docs {
		for _, f := range it.Files {
			if files || pageKinds[f.Extension()] {
				blob(f)
			}
		}
	}
	return p, nil
}

// planIndexes adds to p the root index of state and the index of each of
// its records, with what each takes in the cache, reading again those the
// cache no longer holds. One that still is not held does not stay: p is
// lost.
func (s *Service) planIndexes(ctx context.Context, state port.RootState, p *plan) error {
	seen := map[string]bool{}
	var missing []domain.Record
	rootMissing := false
	measure := func(hash string) bool {
		size, ok := s.cache.IndexSize(hash)
		if ok {
			p.needed += size
		}
		return ok
	}
	if state.Hash != "" {
		seen[state.Hash] = true
		p.indexes = append(p.indexes, state.Hash)
		rootMissing = !measure(state.Hash)
	}
	for _, rec := range state.Records {
		if rec.Hash == "" || seen[rec.Hash] {
			continue
		}
		seen[rec.Hash] = true
		p.indexes = append(p.indexes, rec.Hash)
		if !measure(rec.Hash) {
			missing = append(missing, rec)
		}
	}
	if rootMissing {
		again, err := s.roots.Root(ctx)
		if err != nil {
			return err
		}
		if again.Hash != state.Hash {
			return errMoved
		}
		p.lost = !measure(state.Hash)
	}
	for _, rec := range missing {
		if _, err := s.lib.Index(ctx, rec); err != nil {
			return fmt.Errorf("read the index of %s: %w", rec.ID, err)
		}
		if !measure(rec.Hash) {
			p.lost = true
		}
	}
	return nil
}

// fetch downloads what p lists into the cache, opts.Jobs at a time, and
// returns how many files and bytes it kept. Each file is kept as soon as it
// is whole, so a sync that stops keeps what it fetched.
func (s *Service) fetch(ctx context.Context, p plan, opts port.SyncOptions) (int, int64, error) {
	if len(p.fetch) == 0 {
		return 0, 0, nil
	}
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	var (
		mu       sync.Mutex
		progress = port.SyncProgress{FilesTotal: len(p.fetch), BytesTotal: p.total}
		files    atomic.Int64
		bytes    atomic.Int64
	)
	tell := func(file int, delta int64) {
		if opts.Progress == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		progress.Files += file
		progress.Bytes += delta
		opts.Progress(progress)
	}
	tell(0, 0)
	err := concurrency.Process(ctx, p.fetch, func(ctx context.Context, f domain.File) error {
		var got int64
		err := s.cache.Fill(f.Hash, func(w io.Writer) error {
			return s.blobs.Download(ctx, f, w, func(done, _ int64) {
				tell(0, done-got)
				got = done
			})
		})
		if err != nil {
			tell(0, -got)
			return fmt.Errorf("download %s: %w", f.Name, err)
		}
		files.Add(1)
		bytes.Add(f.Size)
		tell(1, f.Size-got)
		return nil
	}, concurrency.WithConcurrency[domain.File](jobs), concurrency.WithStopOnError[domain.File]())
	return int(files.Load()), bytes.Load(), err
}
