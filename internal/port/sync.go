// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// ErrNoCacheToSync means sync was asked for with the cache turned off.
var ErrNoCacheToSync = errors.New("sync fills the cache, and --no-cache turns it off")

// OverBudgetError means a sync does not fit the profile's cache: Needed
// bytes must be kept at once, and the budget is Budget, set by Source (one
// of BudgetFrom...). A budget of no limit never refuses.
//
// Measured before the download, as it is unless Evicted, no document file
// (no page, no original) was downloaded; the indexes, .metadata and
// .content the library is measured from may have been read into the cache.
// Evicted says the sync fetched what it planned, and part of it did not
// stay: the cache evicted it to stay within the budget, so Needed is at
// least what the sync takes, and the sync was not recorded as complete.
type OverBudgetError struct {
	Needed  int64
	Budget  int64
	Source  string
	Evicted bool
}

func (e *OverBudgetError) Error() string {
	if e.Evicted {
		return fmt.Sprintf("sync needs at least %d bytes in the cache, and not all of it stays within its budget of %d bytes",
			e.Needed, e.Budget)
	}
	return fmt.Sprintf("sync needs %d bytes in the cache, and its budget is %d bytes", e.Needed, e.Budget)
}

// SyncOptions shape a sync.
type SyncOptions struct {
	// Files takes every file of each document too: its original and
	// whatever else its index lists, as get, export and render need them.
	Files bool
	// All takes the documents in the trash too.
	All bool
	// Force walks the library even when the last complete sync covers this
	// one at the same root.
	Force bool
	// Jobs is how many files download at once; below 1 is the default.
	Jobs int
	// Progress, when set, is told what is done of what the sync fetches,
	// one call at a time.
	Progress func(SyncProgress)
}

// SyncProgress is what a sync has fetched of what it fetches.
type SyncProgress struct {
	Files, FilesTotal int
	Bytes, BytesTotal int64
}

// SyncReport is what a sync did.
type SyncReport struct {
	// Current says the cache already held this sync: the last complete
	// sync covers it at the live root, so nothing was walked or fetched.
	Current bool
	// Path is the folder or document synced, as the library names it.
	Path       string
	Root       string
	Generation int64
	// Documents are the documents synced.
	Documents int
	// Fetched files were downloaded, Bytes of them; Cached were already
	// there.
	Fetched int
	Cached  int
	Bytes   int64
	// Needed is what the sync keeps in the cache at once.
	Needed int64
	// CacheBytes is what the profile's cache takes now; Budget bounds it,
	// 0 for no limit, and BudgetSource says what set it.
	CacheBytes   int64
	Budget       int64
	BudgetSource string
}

// Syncer is the input port of rmctl sync: it fills the profile's cache
// ahead of time, so later reads, and --offline runs, need no download.
type Syncer interface {
	// Sync fetches into the cache what the reads of the documents below
	// query need and the cache lacks. "" and "/" are the whole library.
	Sync(ctx context.Context, query string, opts SyncOptions) (SyncReport, error)
}

// SyncMark is the last complete sync of a profile: the root it reached and
// the scope it covered.
type SyncMark struct {
	Root       string
	Generation int64
	// Query is the path or id the sync was asked for, Path the library's
	// name for it, and Scope the folder or document it named, RootID for
	// the whole library.
	Query string
	Path  string
	Scope domain.ItemID
	Files bool
	All   bool
	// Documents, FileCount and Needed are what it covered.
	Documents int
	FileCount int
	Needed    int64
	At        time.Time
	// Removals is the cache's removal generation the sync read before it
	// checked that everything it keeps is there: the mark vouches for the
	// cache only while SyncCache.Removals still reads it.
	Removals string
}

// SyncCache is the profile's cache as sync fills it. Blobs are addressed by
// their sha256; whatever this reports present counts as just used, so it
// is the last to be evicted. Whenever a blob or an index leaves the cache,
// evicted or dropped, the removal generation changes and the last complete
// sync is forgotten with it; a mark recorded by a sync that raced the
// removal names the generation before it, so a mark that names the current
// generation never vouches for an entry that is gone.
type SyncCache interface {
	// Holds reports whether the blob of hash is kept.
	Holds(hash string) bool
	// IndexSize is what the index entry addressed by hash takes, when it
	// is kept.
	IndexSize(hash string) (int64, bool)
	// Fill keeps the blob of hash that write produces, streamed to disk
	// and checked against hash; nothing is kept when write fails.
	Fill(hash string, write func(w io.Writer) error) error
	// Usage is what the cache takes now, and Budget what bounds it, 0 for
	// no limit; BudgetSource says what set the budget (BudgetFrom...).
	Usage() int64
	Budget() int64
	BudgetSource() string
	// LastSync is the last complete sync; MarkSync records one, and
	// DropSync forgets it.
	LastSync() (SyncMark, bool)
	MarkSync(m SyncMark)
	DropSync()
	// Removals is the removal generation: a value that changes whenever a
	// blob or an index leaves the cache, evicted or dropped. False means it
	// cannot be told, and no mark may be trusted.
	Removals() (string, bool)
}
