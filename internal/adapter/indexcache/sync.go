// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"encoding/json/v2"
	"io"
	"log/slog"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// The blob space of the store, and the name of the last complete sync among
// the state: the entry blobcache drops whenever a blob or an index leaves
// the cache (its KeyCovered), so a mark never covers an entry that is gone.
const (
	spaceBlobs = "blobs"
	syncKey    = "sync"
)

// FillStore is one profile's cache as sync fills it; blobcache's Cache
// implements it.
type FillStore interface {
	ProfileStore
	// Size is what the entry key of space takes, counting as a use.
	Size(space, key string) (int64, bool)
	// Fill keeps the blob of hash that write streams, checked against it.
	Fill(hash string, write func(w io.Writer) error) error
	// Removals is the removal generation, which every blob or index that
	// leaves the store changes; false when it cannot be told.
	Removals() (string, bool)
}

// Syncs implements port.SyncCache over a profile's cache: the blobs and
// indexes it holds, and the last complete sync, kept among the state as the
// root pointer is.
type Syncs struct {
	store FillStore
	log   *slog.Logger
}

var _ port.SyncCache = (*Syncs)(nil)

// NewSyncs is the sync cache of store.
func NewSyncs(store FillStore, log *slog.Logger) *Syncs {
	if store == nil {
		panic("indexcache: a store is required")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Syncs{store: store, log: log}
}

// Holds implements port.SyncCache.
func (s *Syncs) Holds(hash string) bool {
	_, ok := s.store.Size(spaceBlobs, hash)
	return ok
}

// IndexSize implements port.SyncCache.
func (s *Syncs) IndexSize(hash string) (int64, bool) {
	return s.store.Size(spaceIndexes, hash)
}

// Fill implements port.SyncCache.
func (s *Syncs) Fill(hash string, write func(w io.Writer) error) error {
	return s.store.Fill(hash, write)
}

// Usage implements port.SyncCache.
func (s *Syncs) Usage() int64 {
	bytes, _ := s.store.Usage()
	return bytes
}

// Budget implements port.SyncCache.
func (s *Syncs) Budget() int64 { return s.store.Budget() }

// BudgetSource implements port.SyncCache.
func (s *Syncs) BudgetSource() string { return s.store.BudgetSource() }

// mark is a port.SyncMark on disk.
type mark struct {
	Version    int       `json:"v"`
	Root       string    `json:"root"`
	Generation int64     `json:"generation"`
	Query      string    `json:"query"`
	Path       string    `json:"path"`
	Scope      string    `json:"scope"`
	Files      bool      `json:"files"`
	All        bool      `json:"all"`
	Documents  int       `json:"documents"`
	FileCount  int       `json:"file_count"`
	Needed     int64     `json:"needed"`
	At         time.Time `json:"at"`
	// Removals is absent from a mark written before the generation was:
	// "" never matches one, so such a mark is walked past once.
	Removals string `json:"removals,omitempty"`
}

// LastSync implements port.SyncCache. A mark that does not decode is
// dropped: the next sync walks.
func (s *Syncs) LastSync() (port.SyncMark, bool) {
	data, ok := s.store.Entry(spaceState, syncKey)
	if !ok {
		return port.SyncMark{}, false
	}
	var m mark
	if err := json.Unmarshal(data, &m); err != nil || m.Version != entryVersion || m.At.IsZero() {
		s.log.Debug("sync mark dropped")
		s.store.Drop(spaceState, syncKey)
		return port.SyncMark{}, false
	}
	return port.SyncMark{
		Root: m.Root, Generation: m.Generation, Query: m.Query, Path: m.Path, Scope: domain.ItemID(m.Scope),
		Files: m.Files, All: m.All, Documents: m.Documents, FileCount: m.FileCount, Needed: m.Needed, At: m.At,
		Removals: m.Removals,
	}, true
}

// MarkSync implements port.SyncCache.
func (s *Syncs) MarkSync(sm port.SyncMark) {
	data, err := json.Marshal(mark{
		Version: entryVersion, Root: sm.Root, Generation: sm.Generation, Query: sm.Query, Path: sm.Path,
		Scope: string(sm.Scope), Files: sm.Files, All: sm.All, Documents: sm.Documents, FileCount: sm.FileCount,
		Needed: sm.Needed, At: sm.At, Removals: sm.Removals,
	})
	if err != nil {
		s.log.Debug("sync mark not encoded", "err", err)
		return
	}
	s.store.PutEntry(spaceState, syncKey, data)
}

// DropSync implements port.SyncCache.
func (s *Syncs) DropSync() { s.store.Drop(spaceState, syncKey) }

// Removals implements port.SyncCache.
func (s *Syncs) Removals() (string, bool) { return s.store.Removals() }
