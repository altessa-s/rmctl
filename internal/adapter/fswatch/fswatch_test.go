// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fswatch_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/fswatch"
)

// wait is how long a test waits for the system to deliver an event.
const wait = 10 * time.Second

// watching is a watch of a temporary directory under way.
type watching struct {
	dir     string
	ready   chan struct{}
	changes chan struct{}
	done    chan error
	cancel  context.CancelFunc
}

func start(t *testing.T, skip func(string) bool) *watching {
	t.Helper()
	w := &watching{dir: t.TempDir(), ready: make(chan struct{}), changes: make(chan struct{}, 64), done: make(chan error, 1)}
	if err := os.MkdirAll(filepath.Join(w.dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.dir, "skipped"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	w.cancel = cancel
	go func() {
		w.done <- fswatch.New().Watch(ctx, w.dir, skip, func() { close(w.ready) }, func() {
			select {
			case w.changes <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-w.ready:
	case err := <-w.done:
		t.Fatalf("the watch ended: %v", err)
	case <-time.After(wait):
		t.Fatal("the watch did not get ready")
	}
	t.Cleanup(func() {
		cancel()
		if err := <-w.done; err != nil {
			t.Errorf("the watch ended in %v", err)
		}
	})
	return w
}

func (w *watching) write(t *testing.T, rel string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.dir, filepath.FromSlash(rel)), []byte(rel), 0o600); err != nil {
		t.Fatal(err)
	}
}

// heard waits for a change.
func (w *watching) heard(t *testing.T) {
	t.Helper()
	select {
	case <-w.changes:
	case <-time.After(wait):
		t.Fatal("no change heard")
	}
}

// drain forgets the changes heard so far.
func (w *watching) drain() {
	for {
		select {
		case <-w.changes:
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}

// A change in the directory, below it, and in a directory made after the
// watch started is heard; one in a directory skipped is not.
func TestWatch(t *testing.T) {
	t.Parallel()
	skip := func(rel string) bool { return rel == "skipped" || strings.HasPrefix(rel, ".") }
	w := start(t, skip)
	w.write(t, "top.pdf")
	w.heard(t)
	w.write(t, "sub/below.pdf")
	w.heard(t)
	if err := os.Mkdir(filepath.Join(w.dir, "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.heard(t)
	w.drain()
	w.write(t, "new/later.pdf")
	w.heard(t)
	w.drain()
	w.write(t, "skipped/hidden.pdf")
	w.write(t, ".journal")
	select {
	case <-w.changes:
		t.Fatal("a change in a skipped place was heard")
	case <-time.After(500 * time.Millisecond):
	}
}

// A directory that cannot be watched is an error at once: the caller falls
// back to polling.
func TestWatchRefused(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	err := fswatch.New().Watch(t.Context(), missing, nil, func() { t.Error("ready") }, func() {})
	if err == nil {
		t.Fatal("a missing directory was watched")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = fswatch.New().Watch(t.Context(), file, nil, func() { t.Error("ready") }, func() {}); err == nil {
		t.Fatal("a file was watched as a directory")
	}
}
