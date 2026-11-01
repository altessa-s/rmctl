// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"time"
)

// ErrOffline means a read under --offline needed something the cache does
// not hold, or a change was asked of the cloud under --offline.
var ErrOffline = errors.New("run without --offline to read it from the cloud")

// ProfileCache is what the cache on disk holds for one profile.
type ProfileCache struct {
	Profile string
	// Dir is the profile's cache directory.
	Dir string
	// Bytes is what its blobs and indexes take; Budget bounds them.
	Bytes  int64
	Budget int64
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
