// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"

	"github.com/altessa-s/rmctl/internal/port"
)

// Caches implements port.ProfileCaches over every profile's cache: the
// profile commands report and remove any profile's, by name.
type Caches struct {
	profiles Profiles
	log      *slog.Logger
}

var _ port.ProfileCaches = (*Caches)(nil)

// NewCaches reports and removes the caches profiles holds.
func NewCaches(profiles Profiles, log *slog.Logger) *Caches {
	if profiles == nil {
		panic("indexcache: profiles are required")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Caches{profiles: profiles, log: log}
}

// Names implements port.ProfileCaches.
func (c *Caches) Names() []string {
	names := slices.Clone(c.profiles.Names())
	slices.Sort(names)
	return names
}

// Status implements port.ProfileCaches: the report of debug cache status
// for one profile.
func (c *Caches) Status(name string) (port.ProfileCache, error) {
	return status(c.profiles, name, c.log)
}

// Remove implements port.ProfileCaches. The cache removes only what it
// wrote; a directory that still holds anything else is kept.
func (c *Caches) Remove(name string) (port.RemovedDir, error) {
	store, err := c.profiles.Open(name)
	if err != nil {
		return port.RemovedDir{}, err
	}
	dir := port.RemovedDir{Path: store.Dir()}
	if dir.Existed, err = exists(dir.Path); err != nil || !dir.Existed {
		return dir, err
	}
	if err = store.Clear(); err != nil {
		return dir, err
	}
	dir.Kept, err = exists(dir.Path)
	return dir, err
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}
