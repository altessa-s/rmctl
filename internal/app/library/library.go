// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultConcurrency is how many items are loaded at once.
const DefaultConcurrency = 8

// DefaultAttempts is how many times a change is replayed on a root that
// moved under it.
const DefaultAttempts = 5

// Library reads the library into a [domain.Tree] and commits changes to its
// root. It is shared by the use cases; it is not an input port itself.
type Library struct {
	roots port.RootStore
	docs  port.DocumentStore
	blobs port.BlobStore

	cache       port.BlobCache
	listings    port.ListingCache
	loads       port.LoadObserver
	log         *slog.Logger
	concurrency int
	attempts    int

	memoMu sync.Mutex
	// memo keeps each loaded item under its record hash: the hash covers
	// every file, so an unchanged hash is an unchanged item.
	memo map[string]loaded

	// commitMu serializes the commits of this process: two of its own
	// changes never race each other into a conflict.
	commitMu sync.Mutex
}

// loaded is one record as the loader found it. ok false means it is not an
// item: no metadata, metadata that does not decode, or a template.
type loaded struct {
	item domain.Item
	ok   bool
}

// Option configures a [Library].
type Option func(*Library)

// WithCache keeps read files in cache.
func WithCache(cache port.BlobCache) Option {
	return func(l *Library) { l.cache = cache }
}

// WithListings keeps the tree of every root loaded or committed in
// listings, for shell completion.
func WithListings(listings port.ListingCache) Option {
	return func(l *Library) { l.listings = listings }
}

// WithLoadObserver tells loads how every load that reads an item goes. A
// load whose items were all read before, as the one of a commit usually is,
// costs no request and tells it nothing. How long a load may run before a
// person is told is the observer's business: the library keeps no time.
func WithLoadObserver(loads port.LoadObserver) Option {
	return func(l *Library) { l.loads = loads }
}

// WithLogger logs what the loader skips.
func WithLogger(log *slog.Logger) Option {
	return func(l *Library) { l.log = log }
}

// WithConcurrency sets how many items load at once; below 1 is 1.
func WithConcurrency(n int) Option {
	return func(l *Library) { l.concurrency = max(n, 1) }
}

// WithAttempts sets how many times a change is tried; below 1 is 1.
func WithAttempts(n int) Option {
	return func(l *Library) { l.attempts = max(n, 1) }
}

// New returns a library over the cloud ports. A nil port is a programming
// error and panics.
func New(roots port.RootStore, docs port.DocumentStore, blobs port.BlobStore, opts ...Option) *Library {
	switch {
	case roots == nil:
		panic("library: RootStore is required")
	case docs == nil:
		panic("library: DocumentStore is required")
	case blobs == nil:
		panic("library: BlobStore is required")
	}
	l := &Library{
		roots:       roots,
		docs:        docs,
		blobs:       blobs,
		log:         slog.New(slog.DiscardHandler),
		concurrency: DefaultConcurrency,
		attempts:    DefaultAttempts,
		memo:        map[string]loaded{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

// Load reads the current root and builds the tree.
func (l *Library) Load(ctx context.Context) (*domain.Tree, port.RootState, error) {
	state, err := l.roots.Root(ctx)
	if err != nil {
		return nil, port.RootState{}, err
	}
	tree, err := l.Build(ctx, state)
	if err == nil && l.listings != nil {
		l.listings.PutListing(state.Hash, tree)
	}
	return tree, state, err
}

// listCommitted keeps the listing of a root just committed. Its items
// are the ones loaded before the change, and the files the change wrote,
// which the caches kept as it uploaded them, so it costs no request in
// practice; a tree it cannot build leaves the listing as it was until the
// next load.
func (l *Library) listCommitted(ctx context.Context, root port.RootState) {
	if l.listings == nil {
		return
	}
	tree, err := l.Build(ctx, root)
	if err != nil {
		l.log.Debug("listing of the committed root not kept", "err", err)
		return
	}
	l.listings.PutListing(root.Hash, tree)
}

// Build builds the tree of a root already read. Items whose record hash was
// loaded before cost no call.
func (l *Library) Build(ctx context.Context, state port.RootState) (*domain.Tree, error) {
	step, end := l.follow(state.Records)
	defer end()
	results, err := concurrency.ProcessCollect(ctx, state.Records,
		func(ctx context.Context, rec domain.Record) (loaded, error) {
			got, err := l.item(ctx, rec)
			if err == nil {
				step()
			}
			return got, err
		},
		concurrency.WithConcurrency[domain.Record](l.concurrency),
		concurrency.WithStopOnError[domain.Record](),
	)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, len(results))
	for _, r := range results {
		if r.ok {
			items = append(items, r.item)
		}
	}
	return domain.NewTree(state.Generation, items), nil
}

// follow tells the observer how the load of records goes, when there is one
// and the load reads an item: step counts one record done, and end ends the
// load. Every record counts, so the count reaches the size of the root, but
// a load of items all read before says nothing at all.
func (l *Library) follow(records []domain.Record) (step, end func()) {
	if l.loads == nil || l.memoized(records) {
		return func() {}, func() {}
	}
	var mu sync.Mutex
	done, total := 0, len(records)
	l.loads.LoadProgress(done, total)
	return func() {
		mu.Lock()
		defer mu.Unlock()
		done++
		l.loads.LoadProgress(done, total)
	}, l.loads.LoadDone
}

// memoized reports whether every one of records was loaded before.
func (l *Library) memoized(records []domain.Record) bool {
	l.memoMu.Lock()
	defer l.memoMu.Unlock()
	for _, rec := range records {
		if _, ok := l.memo[rec.Hash]; !ok {
			return false
		}
	}
	return true
}

func (l *Library) item(ctx context.Context, rec domain.Record) (loaded, error) {
	l.memoMu.Lock()
	hit, ok := l.memo[rec.Hash]
	l.memoMu.Unlock()
	if ok {
		// The record may name the same index under another id only in a
		// broken root; keep the record's own id.
		hit.item.ID = rec.ID
		return hit, nil
	}
	got, err := l.loadItem(ctx, rec)
	if err != nil {
		return loaded{}, err
	}
	l.memoMu.Lock()
	l.memo[rec.Hash] = got
	l.memoMu.Unlock()
	return got, nil
}

func (l *Library) loadItem(ctx context.Context, rec domain.Record) (loaded, error) {
	files, err := l.docs.Files(ctx, rec)
	if err != nil {
		return loaded{}, fmt.Errorf("read the index of %s: %w", rec.ID, err)
	}
	metaFile, ok := find(files, "metadata")
	if !ok {
		l.log.Warn("item without metadata skipped", "id", rec.ID)
		return loaded{}, nil
	}
	data, err := l.Read(ctx, metaFile)
	if err != nil {
		return loaded{}, fmt.Errorf("read the metadata of %s: %w", rec.ID, err)
	}
	meta, err := domain.ParseMetadata(data)
	if err != nil {
		l.log.Warn("item with malformed metadata skipped", "id", rec.ID, "err", err)
		return loaded{}, nil
	}
	if meta.Type() == domain.TypeTemplate {
		return loaded{}, nil
	}
	var tags []string
	if contentFile, ok := find(files, "content"); ok {
		data, err := l.Read(ctx, contentFile)
		if err != nil {
			return loaded{}, fmt.Errorf("read the content of %s: %w", rec.ID, err)
		}
		tags = domain.ContentTags(data)
	}
	return loaded{item: domain.NewItem(rec, files, meta, tags), ok: true}, nil
}

// Index reads the index of rec as Build does, through the document store,
// whose cache keeps it.
func (l *Library) Index(ctx context.Context, rec domain.Record) ([]domain.File, error) {
	return l.docs.Files(ctx, rec)
}

// Read reads one file through the cache.
func (l *Library) Read(ctx context.Context, f domain.File) ([]byte, error) {
	if l.cache != nil {
		if data, ok := l.cache.Get(f.Hash); ok {
			return data, nil
		}
	}
	data, err := l.blobs.Read(ctx, f)
	if err != nil {
		return nil, err
	}
	if l.cache != nil {
		l.cache.Put(f.Hash, data)
	}
	return data, nil
}

// Metadata reads and decodes an item's .metadata.
func (l *Library) Metadata(ctx context.Context, it domain.Item) (domain.Metadata, domain.File, error) {
	f, ok := find(it.Files, "metadata")
	if !ok {
		return domain.Metadata{}, domain.File{}, domain.MalformedFile("metadata", "missing from "+string(it.ID))
	}
	data, err := l.Read(ctx, f)
	if err != nil {
		return domain.Metadata{}, domain.File{}, err
	}
	meta, err := domain.ParseMetadata(data)
	return meta, f, err
}

// Change computes the new root records from the current root and the tree
// built from it. Returning the records unchanged writes nothing.
type Change func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error)

// Commit applies change to the current root and writes the result. On a
// root that moved in between, the change is replayed on the new root, up to
// the attempt limit, after which port.ErrRootConflict is returned. A change
// that leaves the records as they are writes
// nothing. Cancellation is honored up to the root write, which is the last
// point where stopping costs nothing.
func (l *Library) Commit(ctx context.Context, change Change) (port.RootState, error) {
	l.commitMu.Lock()
	defer l.commitMu.Unlock()
	for range l.attempts {
		if err := ctx.Err(); err != nil {
			return port.RootState{}, err
		}
		tree, state, err := l.Load(ctx)
		if err != nil {
			return port.RootState{}, err
		}
		records, err := change(ctx, tree, state)
		if err != nil {
			return port.RootState{}, err
		}
		if slices.Equal(records, state.Records) {
			return state, nil
		}
		if err = ctx.Err(); err != nil {
			return port.RootState{}, err
		}
		root, err := l.roots.CommitRoot(ctx, state, records)
		if errors.Is(err, port.ErrRootConflict) {
			l.log.Debug("root moved during a write; replaying the change")
			continue
		}
		if err != nil {
			return port.RootState{}, err
		}
		l.listCommitted(ctx, root)
		return root, nil
	}
	return port.RootState{}, port.ErrRootConflict
}

// Replace returns records with the record of rec.ID swapped for rec, in place.
func Replace(records []domain.Record, rec domain.Record) ([]domain.Record, bool) {
	out := slices.Clone(records)
	for i, r := range out {
		if r.ID == rec.ID {
			out[i] = rec
			return out, true
		}
	}
	return out, false
}

// ReplaceFile returns files with the file named f.Name swapped for f.
func ReplaceFile(files []domain.File, f domain.File) []domain.File {
	out := slices.Clone(files)
	for i, old := range out {
		if old.Name == f.Name {
			out[i] = f
			return out
		}
	}
	return append(out, f)
}

func find(files []domain.File, ext string) (domain.File, bool) {
	for _, f := range files {
		if f.Extension() == ext {
			return f, true
		}
	}
	return domain.File{}, false
}
