// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// MirrorDisk is port.MirrorFiles over MemFiles: Scan lists the files below
// a directory with their sizes and times, Lock holds a lock per path. Fail
// makes Scan or Lock fail.
type MirrorDisk struct {
	Files *MemFiles
	mu    sync.Mutex
	held  map[string]bool
	Fail  map[string]error
}

// NewMirrorDisk returns the double over files.
func NewMirrorDisk(files *MemFiles) *MirrorDisk {
	return &MirrorDisk{Files: files, held: map[string]bool{}, Fail: map[string]error{}}
}

var _ port.MirrorFiles = (*MirrorDisk)(nil)

func (d *MirrorDisk) fail(method string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Fail[method]
}

// Scan implements port.MirrorFiles. A directory prune reports true for is
// skipped with everything below it. A dir that is no directory is
// ErrNoFile.
func (d *MirrorDisk) Scan(dir string, prune func(string) bool) ([]port.LocalEntry, error) {
	if err := d.fail("Scan"); err != nil {
		return nil, err
	}
	if _, isDir, _, _ := d.Files.Stat(dir); !isDir { //nolint:errcheck // the double's Stat fails only when told to
		return nil, ErrNoFile
	}
	rels, err := d.Files.Walk(dir)
	if err != nil {
		return nil, err
	}
	out := make([]port.LocalEntry, 0, len(rels))
	for _, rel := range rels {
		slash := filepath.ToSlash(rel)
		if pruned(slash, prune) {
			continue
		}
		path := filepath.Join(dir, rel)
		size, _, _, _ := d.Files.Stat(path)  //nolint:errcheck // see above
		mtime, _, _ := d.Files.ModTime(path) //nolint:errcheck // see above
		out = append(out, port.LocalEntry{Path: slash, Size: size, ModTime: mtime})
	}
	return out, nil
}

// pruned reports whether a directory above rel is one prune skips.
func pruned(rel string, prune func(string) bool) bool {
	if prune == nil {
		return false
	}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if prune(strings.Join(parts[:i], "/")) {
			return true
		}
	}
	return false
}

// Lock implements port.MirrorFiles: a path held is ErrMirrorBusy until
// released.
func (d *MirrorDisk) Lock(path string) (func() error, error) {
	if err := d.fail("Lock"); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held[path] {
		return nil, port.ErrMirrorBusy
	}
	d.held[path] = true
	return func() error {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.held, path)
		return nil
	}, nil
}

// Held reports whether the lock at path is held.
func (d *MirrorDisk) Held(path string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.held[path]
}

// Timer is a port.Timer a test drives: Sleep waits until Wake lets every
// sleeper go, moving the time on by the longest of their sleeps, or until
// its context ends. Jump moves the time without waking anyone, as a
// machine that slept does.
type Timer struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []sleeper
	changed  chan struct{}
}

type sleeper struct {
	d    time.Duration
	done chan struct{}
}

// NewTimer returns a timer reading now.
func NewTimer(now time.Time) *Timer { return &Timer{now: now, changed: make(chan struct{})} }

var _ port.Timer = (*Timer)(nil)

// Now implements port.Clock.
func (t *Timer) Now() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now
}

// Sleep implements port.Timer.
func (t *Timer) Sleep(ctx context.Context, d time.Duration) error {
	s := sleeper{d: d, done: make(chan struct{})}
	t.mu.Lock()
	t.sleepers = append(t.sleepers, s)
	t.signal()
	t.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		t.mu.Lock()
		for i, o := range t.sleepers {
			if o.done == s.done {
				t.sleepers = append(t.sleepers[:i], t.sleepers[i+1:]...)
				break
			}
		}
		t.signal()
		t.mu.Unlock()
		return ctx.Err()
	}
}

// signal tells WaitSleepers the sleepers changed; t.mu is held.
func (t *Timer) signal() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// Sleepers is how many goroutines sleep now.
func (t *Timer) Sleepers() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sleepers)
}

// WaitSleepers waits until at least n goroutines sleep, or ctx ends.
func (t *Timer) WaitSleepers(ctx context.Context, n int) bool {
	for {
		t.mu.Lock()
		got, changed := len(t.sleepers), t.changed
		t.mu.Unlock()
		if got >= n {
			return true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

// Wake lets every sleeper go and moves the time on by the longest sleep.
func (t *Timer) Wake() {
	t.mu.Lock()
	defer t.mu.Unlock()
	var longest time.Duration
	for _, s := range t.sleepers {
		longest = max(longest, s.d)
		close(s.done)
	}
	t.sleepers = nil
	t.now = t.now.Add(longest)
	t.signal()
}

// Jump moves the time on by d and wakes nobody.
func (t *Timer) Jump(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = t.now.Add(d)
}

// DirWatcher is a port.DirWatcher a test drives. With Err set, Watch fails
// at once; with Live, it calls ready and then changed for every Change,
// until Break makes it fail; otherwise it watches nothing and waits for its
// context, as a watcher that never gets ready.
type DirWatcher struct {
	Err  error
	Live bool

	mu      sync.Mutex
	starts  int
	changes chan struct{}
	breaks  chan error
}

// NewDirWatcher returns a watcher that is ready when live.
func NewDirWatcher(live bool) *DirWatcher {
	return &DirWatcher{Live: live, changes: make(chan struct{}, 16), breaks: make(chan error, 1)}
}

var _ port.DirWatcher = (*DirWatcher)(nil)

// Watch implements port.DirWatcher.
func (w *DirWatcher) Watch(ctx context.Context, _ string, _ func(string) bool, ready, changed func()) error {
	w.mu.Lock()
	w.starts++
	w.mu.Unlock()
	if w.Err != nil {
		return w.Err
	}
	if w.Live {
		ready()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.changes:
			changed()
		case err := <-w.breaks:
			return err
		}
	}
}

// Change reports a change to the watch under way.
func (w *DirWatcher) Change() { w.changes <- struct{}{} }

// Break makes the watch under way fail with err.
func (w *DirWatcher) Break(err error) { w.breaks <- err }

// Starts counts the watches started.
func (w *DirWatcher) Starts() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.starts
}
