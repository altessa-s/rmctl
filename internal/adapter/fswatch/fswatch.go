// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fswatch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/altessa-s/rmctl/internal/port"
)

// The failures of a watch.
var (
	// ErrLost means the system dropped events: what changed is not known.
	ErrLost = errors.New("the file watcher lost events")
	// ErrTooMany means the directory holds more than the watcher can watch
	// and leave the process the descriptors it needs.
	ErrTooMany = errors.New("too many files for the system's file watcher")
)

// Watcher implements port.DirWatcher over fsnotify: inotify on Linux,
// kqueue on macOS and the BSDs, ReadDirectoryChangesW on Windows.
type Watcher struct{}

var _ port.DirWatcher = Watcher{}

// New returns the watcher.
func New() Watcher { return Watcher{} }

// Watch implements port.DirWatcher. Directories are watched, not files; a
// directory made below dir is watched as it appears, with the directories
// already in it, and counts as a change.
func (Watcher) Watch(ctx context.Context, dir string, skip func(string) bool, ready, changed func()) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("watch %s: not a directory", dir)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}
	defer func() { _ = w.Close() }()
	t := tree{w: w, dir: dir, skip: skip, budget: handleBudget(), used: new(int)}
	if err = t.add(dir); err != nil {
		return err
	}
	ready()
	return t.serve(ctx, w.Events, w.Errors, changed)
}

// serve calls changed for each event that skip does not name, watching the
// directories made, until ctx ends or the watcher fails.
func (t tree) serve(ctx context.Context, events <-chan fsnotify.Event, errs <-chan error, changed func()) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if !ok {
				return fmt.Errorf("watch %s: %w", t.dir, fs.ErrClosed)
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				return fmt.Errorf("watch %s: %w", t.dir, ErrLost)
			}
			return fmt.Errorf("watch %s: %w", t.dir, err)
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("watch %s: %w", t.dir, fs.ErrClosed)
			}
			rel, skipped := t.rel(ev.Name)
			if skipped || rel == "" {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, serr := os.Lstat(ev.Name); serr == nil && info.IsDir() {
					if err := t.add(ev.Name); err != nil {
						return err
					}
				} else if err := t.spend(); err != nil {
					// kqueue watches each file made in a directory it watches.
					return err
				}
			}
			changed()
		}
	}
}

// tree adds the directories below dir to a watcher.
type tree struct {
	w    adder
	dir  string
	skip func(string) bool
	// budget is how many directories and files the watcher may watch, 0
	// for no limit, and used how many it watches: kqueue holds a
	// descriptor open for each, of the few the process has.
	budget int
	used   *int
}

// spend counts one more directory or file watched, or is ErrTooMany.
func (t tree) spend() error {
	if t.budget <= 0 {
		return nil
	}
	if *t.used++; *t.used > t.budget {
		return fmt.Errorf("watch %s: %w (more than %d)", t.dir, ErrTooMany, t.budget)
	}
	return nil
}

// adder is what tree needs of the watcher: fsnotify's, or a test's.
type adder interface{ Add(name string) error }

// rel is the path of name relative to the watched directory, with
// slashes, and whether skip names it or a directory above it.
func (t tree) rel(name string) (string, bool) {
	rel, err := filepath.Rel(t.dir, name)
	if err != nil || rel == "." {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if t.skip == nil {
		return rel, false
	}
	for i := range len(rel) {
		if rel[i] == '/' && t.skip(rel[:i]) {
			return rel, true
		}
	}
	return rel, t.skip(rel)
}

// add watches root and every directory below it that skip does not name.
// A directory gone meanwhile is passed over.
func (t tree) add(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case !d.IsDir():
			return t.spend()
		}
		if _, skipped := t.rel(p); skipped {
			return filepath.SkipDir
		}
		if err = t.spend(); err != nil {
			return err
		}
		if err = t.w.Add(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("watch %s: %w", p, err)
		}
		return nil
	})
}
