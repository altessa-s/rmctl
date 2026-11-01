// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// The spaces of the store this adapter writes, the ones blobcache keeps,
// and the name of the root pointer among the state.
const (
	spaceIndexes = "indexes"
	spaceState   = "state"
	rootKey      = "root"
)

// entryVersion is the layout of an entry; an entry of another version is a
// miss.
const entryVersion = 1

// The kinds of index entry.
const (
	kindRoot  = "root"
	kindFiles = "files"
)

// DefaultStale is how old a root may be before an offline read warns.
const DefaultStale = 24 * time.Hour

// Source is the cloud: the stores this adapter decorates, and the reads they
// are made of, so a cached root costs one request and a cached index none.
// The cloud adapter implements it.
type Source interface {
	port.RootStore
	port.DocumentStore
	// Pointer reads the root's hash and generation, without its index; ""
	// is a library with no root yet.
	Pointer(ctx context.Context) (hash string, generation int64, err error)
	// RootIndex and Index read an index with its bytes, nil when the hash
	// that addresses it is not the hash of its bytes.
	RootIndex(ctx context.Context, hash string) ([]byte, []domain.Record, error)
	Index(ctx context.Context, rec domain.Record) ([]byte, []domain.File, error)
	// DecodeRoot and DecodeFiles parse index bytes held to hash.
	DecodeRoot(raw []byte, hash string) ([]domain.Record, error)
	DecodeFiles(raw []byte, hash string) ([]domain.File, error)
}

// Store keeps entries by space and key; blobcache implements it. A missing
// or unreadable entry is a miss, and a write is whole or absent.
type Store interface {
	Entry(space, key string) ([]byte, bool)
	PutEntry(space, key string, data []byte)
	Drop(space, key string)
}

// Mode is how a run may use the cached root.
type Mode int

const (
	// Live asks the cloud for the root every time: writes, and reads
	// without a TTL.
	Live Mode = iota
	// Fresh serves a root the cloud confirmed less than the TTL ago without
	// asking: read-only commands with a TTL.
	Fresh
	// Offline never asks the cloud.
	Offline
)

// Options shape the decorator.
type Options struct {
	// Profile is the profile whose cache the store is, named in messages.
	// Every profile has a store of its own: two accounts share nothing.
	Profile string
	Mode    Mode
	// TTL is how long a confirmed root serves reads in Fresh mode.
	TTL time.Duration
	// Stale is the age past which an offline root draws a warning; zero is
	// DefaultStale.
	Stale time.Duration
	Clock port.Clock
	Log   *slog.Logger
}

// Cache decorates the cloud's root and document stores with the indexes
// kept on disk. Everything it keeps is addressed by its content, so nothing
// goes stale: a root hash names one root index, a record hash one document
// index. Only the root pointer moves, and Live and Fresh mode keep it
// current; reads ask for it once per run (Fresh: not at all within the TTL)
// and fetch only the indexes whose hash is not kept yet.
type Cache struct {
	src   Source
	store Store
	opts  Options
}

var (
	_ port.RootStore     = (*Cache)(nil)
	_ port.DocumentStore = (*Cache)(nil)
)

// New decorates src with store. src, store and Options.Clock are required.
func New(src Source, store Store, opts Options) *Cache {
	switch {
	case src == nil:
		panic("indexcache: a source is required")
	case store == nil:
		panic("indexcache: a store is required")
	case opts.Clock == nil:
		panic("indexcache: a clock is required")
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.Stale <= 0 {
		opts.Stale = DefaultStale
	}
	return &Cache{src: src, store: store, opts: opts}
}

// Root implements port.RootStore: the cached root when the mode allows it
// and the cache holds it, else the cloud's pointer with its index from the
// cache when it is there.
func (c *Cache) Root(ctx context.Context) (port.RootState, error) {
	now := c.opts.Clock.Now()
	ptr, known := readPointer(c.store, c.opts.Log)
	switch c.opts.Mode {
	case Offline:
		if !known {
			return port.RootState{}, fmt.Errorf("profile %s has no root in the cache: %w", c.opts.Profile, port.ErrOffline)
		}
		if age := now.Sub(ptr.Confirmed); age > c.opts.Stale {
			c.opts.Log.Warn("the cached library was last confirmed "+ago(age)+" ago; run without --offline to refresh it",
				"confirmed", ptr.Confirmed.UTC().Format(time.RFC3339))
		}
		state, ok := c.cachedState(ptr)
		if !ok {
			return port.RootState{}, fmt.Errorf("the root index %s is not in the cache: %w", short(ptr.Hash), port.ErrOffline)
		}
		return state, nil
	case Fresh:
		if known && c.opts.TTL > 0 && now.Sub(ptr.Confirmed) < c.opts.TTL {
			if state, ok := c.cachedState(ptr); ok {
				return state, nil
			}
		}
	case Live:
	}
	hash, generation, err := c.src.Pointer(ctx)
	if err != nil {
		return port.RootState{}, err
	}
	state := port.RootState{Hash: hash, Generation: generation}
	if hash != "" {
		records, ok := c.rootRecords(hash)
		if !ok {
			raw, fetched, err := c.src.RootIndex(ctx, hash)
			if err != nil {
				return port.RootState{}, err
			}
			c.putIndex(hash, entry{Kind: kindRoot, Raw: raw, Records: recordViews(fetched)}, raw == nil)
			records = fetched
		}
		state.Records = records
	}
	writePointer(c.store, pointer{Hash: hash, Generation: generation, Confirmed: now}, c.opts.Log)
	return state, nil
}

// cachedState is the root ptr names, built from the cache alone.
func (c *Cache) cachedState(ptr pointer) (port.RootState, bool) {
	state := port.RootState{Hash: ptr.Hash, Generation: ptr.Generation}
	if ptr.Hash == "" {
		return state, true
	}
	records, ok := c.rootRecords(ptr.Hash)
	state.Records = records
	return state, ok
}

// CommitRoot implements port.RootStore: the cloud writes the root, and the
// root it wrote becomes the cached one. It never runs offline.
func (c *Cache) CommitRoot(ctx context.Context, base port.RootState, records []domain.Record) (port.RootState, error) {
	if c.opts.Mode == Offline {
		return port.RootState{}, fmt.Errorf("a change is not committed offline: %w", port.ErrOffline)
	}
	root, err := c.src.CommitRoot(ctx, base, records)
	if err != nil {
		return port.RootState{}, err
	}
	c.putIndex(root.Hash, entry{Kind: kindRoot, Records: recordViews(root.Records)}, true)
	writePointer(c.store, pointer{Hash: root.Hash, Generation: root.Generation, Confirmed: c.opts.Clock.Now()}, c.opts.Log)
	return root, nil
}

// Files implements port.DocumentStore: the index of rec from the cache, else
// from the cloud, kept for next time.
func (c *Cache) Files(ctx context.Context, rec domain.Record) ([]domain.File, error) {
	if files, ok := c.files(rec.Hash); ok {
		return files, nil
	}
	if c.opts.Mode == Offline {
		return nil, fmt.Errorf("the index of %s is not in the cache: %w", rec.ID, port.ErrOffline)
	}
	raw, files, err := c.src.Index(ctx, rec)
	if err != nil {
		return nil, err
	}
	c.putIndex(rec.Hash, entry{Kind: kindFiles, Raw: raw, Files: fileViews(files)}, raw == nil)
	return files, nil
}

// PutIndex implements port.DocumentStore: the cloud stores the index, and
// the cache keeps the files it lists under the record's hash.
func (c *Cache) PutIndex(ctx context.Context, id domain.ItemID, files []domain.File) (domain.Record, error) {
	if c.opts.Mode == Offline {
		return domain.Record{}, fmt.Errorf("the index of %s is not uploaded offline: %w", id, port.ErrOffline)
	}
	rec, err := c.src.PutIndex(ctx, id, files)
	if err != nil {
		return domain.Record{}, err
	}
	c.putIndex(rec.Hash, entry{Kind: kindFiles, Files: fileViews(files)}, true)
	return rec, nil
}

// entry is an index on disk: the bytes the cloud sent when they are its
// address, read back through the current parser and checked against the
// hash; else the parsed list, as this process read or wrote it.
type entry struct {
	Version int          `json:"v"`
	Kind    string       `json:"kind"`
	Hash    string       `json:"hash"`
	Raw     []byte       `json:"raw,omitempty"`
	Records []recordView `json:"records,omitempty"`
	Files   []fileView   `json:"files,omitempty"`
}

type recordView struct {
	ID       string `json:"id"`
	Hash     string `json:"hash"`
	Subfiles int    `json:"subfiles"`
	Size     int64  `json:"size"`
}

type fileView struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func recordViews(records []domain.Record) []recordView {
	out := make([]recordView, 0, len(records))
	for _, r := range records {
		out = append(out, recordView{ID: string(r.ID), Hash: r.Hash, Subfiles: r.Subfiles, Size: r.Size})
	}
	return out
}

func fileViews(files []domain.File) []fileView {
	out := make([]fileView, 0, len(files))
	for _, f := range files {
		out = append(out, fileView{Name: f.Name, Hash: f.Hash, Size: f.Size})
	}
	return out
}

// putIndex keeps e under hash: the bytes when there are any, the parsed
// list only when parsed says there are none.
func (c *Cache) putIndex(hash string, e entry, parsed bool) {
	e.Version, e.Hash = entryVersion, hash
	if !parsed {
		e.Records, e.Files = nil, nil
	}
	data, err := json.Marshal(e)
	if err != nil {
		c.opts.Log.Debug("index cache entry not encoded", "err", err)
		return
	}
	c.store.PutEntry(spaceIndexes, hash, data)
}

// index reads the entry under hash of kind. An entry that does not decode,
// names another hash, or whose bytes do not hold to it is dropped.
func (c *Cache) index(hash, kind string) (entry, bool) {
	data, ok := c.store.Entry(spaceIndexes, hash)
	if !ok {
		return entry{}, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil || e.Version != entryVersion || e.Hash != hash {
		c.drop(hash, "unreadable")
		return entry{}, false
	}
	if e.Kind != kind {
		return entry{}, false
	}
	return e, true
}

func (c *Cache) drop(hash, why string) {
	c.opts.Log.Debug("index cache entry dropped", "hash", hash, "why", why)
	c.store.Drop(spaceIndexes, hash)
}

// rootRecords are the records of the root index hash names, from the cache.
func (c *Cache) rootRecords(hash string) ([]domain.Record, bool) {
	e, ok := c.index(hash, kindRoot)
	if !ok {
		return nil, false
	}
	if e.Raw != nil {
		records, err := c.src.DecodeRoot(e.Raw, hash)
		if err != nil {
			c.drop(hash, err.Error())
			return nil, false
		}
		return records, true
	}
	records := make([]domain.Record, 0, len(e.Records))
	for _, r := range e.Records {
		records = append(records, domain.Record{ID: domain.ItemID(r.ID), Hash: r.Hash, Subfiles: r.Subfiles, Size: r.Size})
	}
	return records, true
}

// files are the files of the document index hash names, from the cache.
func (c *Cache) files(hash string) ([]domain.File, bool) {
	e, ok := c.index(hash, kindFiles)
	if !ok {
		return nil, false
	}
	if e.Raw != nil {
		files, err := c.src.DecodeFiles(e.Raw, hash)
		if err != nil {
			c.drop(hash, err.Error())
			return nil, false
		}
		return files, true
	}
	files := make([]domain.File, 0, len(e.Files))
	for _, f := range e.Files {
		files = append(files, domain.File{Name: f.Name, Hash: f.Hash, Size: f.Size})
	}
	return files, true
}

// shortHash is how many digits of a hash a message names.
const shortHash = 12

func short(hash string) string {
	if len(hash) > shortHash {
		return hash[:shortHash]
	}
	return hash
}

// ago is a duration for a person, to the hour or the minute.
func ago(d time.Duration) string {
	if d >= time.Hour {
		return d.Truncate(time.Hour).String()
	}
	return d.Truncate(time.Minute).String()
}
