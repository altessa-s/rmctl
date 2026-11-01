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

// Root is the cache directory of all profiles: one subdirectory per
// profile, each a [Cache] with its own budget. Profiles share nothing: not
// a root, not an index, not a blob, though blobs are addressed by content,
// because they are different accounts' documents.
type Root struct {
	// base is a cache of the root directory itself, never written: it
	// carries the budget, the clock and the log every profile's gets.
	base *Cache
}

// NewRoot returns the cache directory dir, each profile bounded by
// maxBytes. dir and clock are required.
func NewRoot(dir string, maxBytes int64, clock port.Clock, log *slog.Logger) *Root {
	return &Root{base: New(dir, maxBytes, clock, log)}
}

// Dir is the cache directory.
func (r *Root) Dir() string { return r.base.dir }

// Profile is the cache of profile, which must be a safe directory name.
func (r *Root) Profile(profile string) (*Cache, error) {
	if !nameKey.MatchString(profile) {
		return nil, ErrProfileName
	}
	return New(filepath.Join(r.base.dir, profile), r.base.maxBytes, r.base.clock, r.base.log), nil
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
