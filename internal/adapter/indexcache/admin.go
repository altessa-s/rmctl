// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// ProfileStore is one profile's cache: its entries, what it takes, and a
// way to empty it. blobcache's Cache implements it.
type ProfileStore interface {
	Store
	Dir() string
	// Budget bounds the blobs and indexes, 0 for no limit, and
	// BudgetSource says what set it.
	Budget() int64
	BudgetSource() string
	Usage() (bytes int64, entries map[string]int)
	Clear() error
}

// Profiles opens the cache of any profile; blobcache's Root implements it
// through [Profiles.Open].
type Profiles interface {
	// Dir is the cache directory of all profiles.
	Dir() string
	// Names are the profiles that have a cache.
	Names() []string
	// Open is the cache of profile.
	Open(profile string) (ProfileStore, error)
}

// Settings are what a run was told about the cache, for the report.
type Settings struct {
	Profile  string
	TTL      time.Duration
	Offline  bool
	Disabled bool
}

// Admin implements port.Cache over every profile's cache.
type Admin struct {
	profiles Profiles
	settings Settings
	log      *slog.Logger
}

var _ port.Cache = (*Admin)(nil)

// NewAdmin reports and clears the caches profiles holds.
func NewAdmin(profiles Profiles, settings Settings, log *slog.Logger) *Admin {
	if profiles == nil {
		panic("indexcache: profiles are required")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Admin{profiles: profiles, settings: settings, log: log}
}

// names are the current profile, or every profile with a cache and the
// current one.
func (a *Admin) names(all bool) []string {
	if !all {
		return []string{a.settings.Profile}
	}
	names := a.profiles.Names()
	if slices.Contains(names, a.settings.Profile) {
		return names
	}
	return append(names, a.settings.Profile)
}

// Status implements port.Cache.
func (a *Admin) Status(_ context.Context, all bool) (port.CacheReport, error) {
	report := port.CacheReport{
		Dir: a.profiles.Dir(), Profile: a.settings.Profile, TTL: a.settings.TTL,
		Offline: a.settings.Offline, Disabled: a.settings.Disabled,
	}
	for _, name := range a.names(all) {
		pc, err := status(a.profiles, name, a.log)
		if err != nil {
			return port.CacheReport{}, err
		}
		report.Profiles = append(report.Profiles, pc)
	}
	return report, nil
}

// status is what the cache of profile holds.
func status(profiles Profiles, name string, log *slog.Logger) (port.ProfileCache, error) {
	store, err := profiles.Open(name)
	if err != nil {
		return port.ProfileCache{}, err
	}
	bytes, entries := store.Usage()
	pc := port.ProfileCache{Profile: name, Dir: store.Dir(), Bytes: bytes, Budget: store.Budget(), BudgetSource: store.BudgetSource(), Entries: entries}
	if p, ok := readPointer(store, log); ok {
		pc.RootHash, pc.RootGeneration, pc.Confirmed = p.Hash, p.Generation, p.Confirmed
	}
	return pc, nil
}

// Clear implements port.Cache.
func (a *Admin) Clear(_ context.Context, all bool) ([]string, error) {
	names := a.names(all)
	errs := make([]error, 0, len(names))
	for _, name := range names {
		store, err := a.profiles.Open(name)
		if err == nil {
			err = store.Clear()
		}
		errs = append(errs, err)
	}
	return names, errors.Join(errs...)
}
