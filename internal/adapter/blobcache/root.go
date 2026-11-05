// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package blobcache

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/altessa-s/rmctl/internal/port"
)

// ErrProfileName means a profile name could not name a directory safely.
var ErrProfileName = errors.New("blobcache: a profile name is 1-64 characters of a-z, 0-9, _ and -")

// legacyDir is where every profile once shared one blob directory.
const legacyDir = "blobs"

// Limit is a profile's budget: Bytes of blobs and indexes, 0 for no limit,
// and Source, what set it, which the cache only repeats in reports.
type Limit struct {
	Bytes  int64
	Source string
}

// Limits is the budget of each profile, by name.
type Limits func(profile string) Limit

// Fixed is the same budget for every profile.
func Fixed(maxBytes int64) Limits {
	return func(string) Limit { return Limit{Bytes: maxBytes} }
}

// Root is the cache directory of all profiles: one subdirectory per
// profile, each a [Cache] with its own budget. Profiles share nothing: not
// a root, not an index, not a blob, though blobs are addressed by content,
// because they are different accounts' documents.
type Root struct {
	// base is a cache of the root directory itself, never written: it
	// carries the clock and the log every profile's gets.
	base   *Cache
	limits Limits
}

// NewRoot returns the cache directory dir, each profile bounded by its
// budget in limits. dir, limits and clock are required.
func NewRoot(dir string, limits Limits, clock port.Clock, log *slog.Logger) *Root {
	if limits == nil {
		panic("blobcache: the budgets are required")
	}
	return &Root{base: New(dir, 0, clock, log), limits: limits}
}

// Dir is the cache directory.
func (r *Root) Dir() string { return r.base.dir }

// Profile is the cache of profile, which must be a safe directory name,
// evicted down to its budget when it takes more: a budget lowered since
// the cache was filled holds from the first command that opens it.
func (r *Root) Profile(profile string) (*Cache, error) {
	if !nameKey.MatchString(profile) {
		return nil, ErrProfileName
	}
	limit := r.limits(profile)
	c := New(filepath.Join(r.base.dir, profile), limit.Bytes, r.base.clock, r.base.log)
	c.source = limit.Source
	c.Reconcile()
	return c, nil
}

// Profiles are the profiles that have a cache directory, by name.
func (r *Root) Profiles() []string {
	entries, err := os.ReadDir(r.base.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			r.base.log.Debug("cache scan failed", "err", err)
		}
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !nameKey.MatchString(e.Name()) || !r.isProfile(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// isProfile reports whether the directory name holds one of the spaces.
func (r *Root) isProfile(name string) bool {
	for _, space := range Spaces() {
		if info, err := os.Stat(filepath.Join(r.base.dir, name, space)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// RemoveLegacy removes the blobs every profile once shared, at the top of
// the cache directory (as RMCTL_CACHE_DIR held them) and in its blobs
// directory (as the default directory did). They cannot be moved into a
// profile: nothing says whose they were, and a profile must never be given
// another account's documents. Only files named by a hash go, and the
// blobs directory only when that leaves it empty. It is cheap when there is
// nothing left, so it runs on every start.
func (r *Root) RemoveLegacy() {
	for _, dir := range []string{r.base.dir, filepath.Join(r.base.dir, legacyDir)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type().IsRegular() && validHash(e.Name()) {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
					r.base.log.Debug("legacy cache entry not removed", "err", err)
				}
			}
		}
	}
	_ = os.Remove(filepath.Join(r.base.dir, legacyDir))
}
