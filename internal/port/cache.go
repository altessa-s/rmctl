// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// ErrOffline means a read under --offline needed something the cache does
// not hold, or a change was asked of the cloud under --offline.
var ErrOffline = errors.New("run without --offline to read it from the cloud")

// What set a profile's cache budget: RMCTL_CACHE_BUDGET, the profile's
// [profiles.<name>.cache] table, the configuration file's [cache] table, or
// nothing (the built-in 512 MiB).
const (
	BudgetFromEnv     = "env"
	BudgetFromProfile = "profile"
	BudgetFromConfig  = "config"
	BudgetFromDefault = "default"
)

// ProfileCache is what the cache on disk holds for one profile.
type ProfileCache struct {
	Profile string
	// Dir is the profile's cache directory.
	Dir string
	// Bytes is what its blobs and indexes take; Budget bounds them, 0 for
	// no limit, and BudgetSource says what set it (BudgetFrom...).
	Bytes        int64
	Budget       int64
	BudgetSource string
	// Entries counts its entries by kind: blobs, indexes, state.
	Entries map[string]int
	// RootHash and RootGeneration are its last known root, and Confirmed
	// when the cloud last confirmed it; a zero Confirmed is none known.
	RootHash       string
	RootGeneration int64
	Confirmed      time.Time
}

// CacheReport is the cache of the current profile, or of every profile.
type CacheReport struct {
	// Dir is the cache directory of all profiles.
	Dir string
	// Profile is the current profile.
	Profile string
	// TTL is how long a confirmed root serves reads without asking the
	// cloud; zero is never. Offline and Disabled are --offline and
	// --no-cache as they apply to this run.
	TTL      time.Duration
	Offline  bool
	Disabled bool
	// Profiles are the caches reported, by name.
	Profiles []ProfileCache
}

// Cache is the input port of rmctl debug cache. It has no rule of its own
// to apply: it reports and clears what the cache adapter keeps, so the
// adapter implements it directly.
type Cache interface {
	// Status reports the current profile's cache, or every profile's with
	// all.
	Status(ctx context.Context, all bool) (CacheReport, error)
	// Clear removes the current profile's cache, or every profile's with
	// all, and names the profiles it cleared.
	Clear(ctx context.Context, all bool) ([]string, error)
}

// ErrNoListing means the profile's cache holds no listing of the library:
// no command has read the library into it yet, or the cache is off.
var ErrNoListing = errors.New("the cache holds no listing of the library")

// ListingCache keeps the library as the last command that read it saw
// it, for shell completion: the tree of the root hash names, replacing
// the listing kept before. It never fails a command.
type ListingCache interface {
	PutListing(hash string, tree *domain.Tree)
}

// Completion is the input port of shell completion. Completion runs on
// every Tab, so it has no rule of its own to apply and asks nothing of the
// cloud: the cache adapter implements it directly over what
// [ListingCache] kept.
type Completion interface {
	// Listing is the library as the profile's cache last kept it, or
	// [ErrNoListing]. It never reaches the cloud, asks for credentials or
	// writes the cache. Its items carry no files, times or tags.
	Listing(ctx context.Context) (*domain.Tree, error)
}
