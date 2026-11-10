// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fswatch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

var errBoom = errors.New("boom")

// adds records the directories added, failing with err.
type adds struct {
	names []string
	err   error
}

func (a *adds) Add(name string) error {
	a.names = append(a.names, name)
	return a.err
}

// The watcher's failures end the watch: lost events, any other error, a
// channel closed, a directory made that cannot be watched.
func TestServeEnds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "made"), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(a *adds, ev *fsnotify.Event, err error, closeEvents, closeErrs bool) error {
		events, errs := make(chan fsnotify.Event, 1), make(chan error, 1)
		switch {
		case ev != nil:
			events <- *ev
		case err != nil:
			errs <- err
		case closeEvents:
			close(events)
		case closeErrs:
			close(errs)
		}
		return tree{w: a, dir: dir}.serve(t.Context(), events, errs, func() {})
	}
	if err := run(&adds{}, nil, fsnotify.ErrEventOverflow, false, false); !errors.Is(err, ErrLost) {
		t.Fatal(err)
	}
	if err := run(&adds{}, nil, errBoom, false, false); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if err := run(&adds{}, nil, nil, true, false); !errors.Is(err, fs.ErrClosed) {
		t.Fatal(err)
	}
	if err := run(&adds{}, nil, nil, false, true); !errors.Is(err, fs.ErrClosed) {
		t.Fatal(err)
	}
	made := fsnotify.Event{Name: filepath.Join(dir, "made"), Op: fsnotify.Create}
	if err := run(&adds{err: errBoom}, &made, nil, false, false); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// Events of the directory itself, and outside it, are no change; a
// directory gone before it is added is passed over.
func TestServeFilters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	events := make(chan fsnotify.Event, 4)
	events <- fsnotify.Event{Name: dir, Op: fsnotify.Write}
	events <- fsnotify.Event{Name: filepath.Join(dir, "gone"), Op: fsnotify.Create}
	ctx, cancel := context.WithCancel(t.Context())
	changes := 0
	a := &adds{}
	err := tree{w: a, dir: dir}.serve(ctx, events, make(chan error), func() {
		changes++
		cancel()
	})
	if err != nil || changes != 1 || len(a.names) != 0 {
		t.Fatal(err, changes, a.names)
	}
	if err = (tree{w: &adds{err: fs.ErrNotExist}, dir: dir}).add(dir); err != nil {
		t.Fatal(err)
	}
}

// add watches the directories, passes over files and the skipped ones,
// and fails on a directory it cannot read or watch.
func TestAdd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, sub := range []string{"a/b", "skip/deep"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sub)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	skip := func(rel string) bool { return rel == "skip" }
	a := &adds{}
	if err := (tree{w: a, dir: dir, skip: skip}).add(dir); err != nil || len(a.names) != 3 {
		t.Fatal(err, a.names)
	}
	if rel, skipped := (tree{dir: dir, skip: skip}).rel(filepath.Join(dir, "skip", "deep", "x")); !skipped || rel != "skip/deep/x" {
		t.Fatal(rel, skipped)
	}
	if err := (tree{w: &adds{err: errBoom}, dir: dir}).add(dir); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		return
	}
	locked := filepath.Join(dir, "a")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := (tree{w: &adds{}, dir: dir}).add(dir); err == nil && filepath.Separator == '/' {
		t.Fatal("an unreadable directory was watched")
	}
}
