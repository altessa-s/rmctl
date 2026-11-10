// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// notifier delivers the events a test sends, until its context ends.
type notifier struct{ events chan port.Event }

func (n notifier) Listen(ctx context.Context, on func(port.Event)) error {
	for {
		select {
		case e := <-n.events:
			on(e)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// watching is a watch under way over a fixture.
type watching struct {
	f      *fixture
	timer  *porttest.Timer
	events chan port.Event
	passes chan port.MirrorPass
	cancel context.CancelFunc
	done   chan error
	ctx    context.Context
}

// watch starts a watch of the fixture's mirror on a timer the test drives.
func watch(t *testing.T, f *fixture, opts port.MirrorRunOptions) *watching {
	t.Helper()
	return watchWith(t, f, opts, porttest.NewDirWatcher(false))
}

// watchWith is watch with the file watcher dirs.
func watchWith(t *testing.T, f *fixture, opts port.MirrorRunOptions, dirs port.DirWatcher) *watching {
	t.Helper()
	w := &watching{
		f: f, timer: porttest.NewTimer(now), events: make(chan port.Event, 8),
		passes: make(chan port.MirrorPass, 32), done: make(chan error, 1),
	}
	d := f.deps(true, w.timer)
	d.Notifier = notifier{events: w.events}
	d.Dirs = dirs
	engine := mirror.NewEngine(d)
	w.ctx, w.cancel = context.WithCancel(t.Context())
	go func() { w.done <- engine.Watch(w.ctx, f.spec, opts, func(p port.MirrorPass) { w.passes <- p }) }()
	t.Cleanup(w.stop)
	return w
}

func (w *watching) stop() {
	w.cancel()
	<-w.done
	w.done <- nil
}

// next is the next pass reported.
func (w *watching) next(t *testing.T) port.MirrorPass {
	t.Helper()
	select {
	case p := <-w.passes:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("no pass")
		return port.MirrorPass{}
	}
}

// drive wakes the sleepers until a pass is reported, for a watch whose
// goroutines race each other to the next pass.
func (w *watching) drive(t *testing.T) port.MirrorPass {
	t.Helper()
	for range 500 {
		select {
		case p := <-w.passes:
			return p
		case <-time.After(10 * time.Millisecond):
		}
		w.timer.Wake()
	}
	t.Fatal("no pass")
	return port.MirrorPass{}
}

// sleepers waits until n goroutines of the watch sleep.
func (w *watching) sleepers(t *testing.T, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	if !w.timer.WaitSleepers(ctx, n) {
		t.Fatalf("%d sleepers, want %d", w.timer.Sleepers(), n)
	}
}

// A watch makes a pass at start, then one after each local change, each
// library event, each reconnection and each wake from sleep, once the
// changes settle.
func TestWatch(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one")})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	w := watch(t, f, port.MirrorRunOptions{Watch: true})
	want(t, w.next(t), "adopt Paper.pdf")
	if !f.disk.Held(filepath.Join(dir, domain.MirrorLockName)) {
		t.Fatal("the watch does not hold the lock")
	}

	// A local change: the poll sees it, and a pass follows once it settles.
	f.pdfFile("New.pdf", "%PDF new", 1, time10)
	w.sleepers(t, 1)
	w.timer.Wake()
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t), "upload New.pdf")

	// The first connection is no change; a library event is, and so is a
	// reconnection.
	w.events <- port.Event{Kind: port.EventConnected}
	w.events <- port.Event{Kind: port.EventDropped}
	w.events <- port.Event{Kind: port.EventLibrary, SyncComplete: true}
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t))
	w.events <- port.Event{Kind: port.EventConnected}
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t))

	// The machine slept: a full pass at the next look.
	w.sleepers(t, 1)
	w.timer.Jump(time.Hour)
	w.timer.Wake()
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t))
}

// A rescan comes on its own every interval.
func TestWatchRescans(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	w := watch(t, f, port.MirrorRunOptions{Watch: true, Poll: time.Minute, Rescan: 2 * time.Minute, Debounce: -1})
	want(t, w.next(t))
	w.sleepers(t, 1)
	w.timer.Wake()
	w.sleepers(t, 1)
	w.timer.Wake()
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t))
}

// A pass that fails as a whole is tried again after a wait.
func TestWatchBacksOff(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.cloud.Fail["Root"] = errBoom
	w := watch(t, f, port.MirrorRunOptions{Watch: true})
	if p := w.next(t); !errors.Is(p.Err, errBoom) {
		t.Fatal(p.Err)
	}
	w.sleepers(t, 2)
	f.cloud.Fail["Root"] = nil
	w.timer.Wake()
	if p := w.next(t); p.Err != nil {
		t.Fatal(p.Err)
	}
}

// A watch waits for the directory, and for another process's lock.
func TestWatchWaitsForTheDirectory(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	release, err := f.disk.Lock(filepath.Join(dir, domain.MirrorLockName))
	if err != nil {
		t.Fatal(err)
	}
	w := watch(t, f, port.MirrorRunOptions{Watch: true})
	if p := w.next(t); !errors.Is(p.Err, port.ErrMirrorBusy) {
		t.Fatal(p.Err)
	}
	w.sleepers(t, 1)
	_ = release()
	w.timer.Wake()
	if p := w.next(t); p.Err != nil {
		t.Fatal(p.Err)
	}
	// Stopped while waiting for the lock: it ends.
	f2 := setup(t, true)
	f2.spec.Dir = "/gone"
	w2 := watch(t, f2, port.MirrorRunOptions{Watch: true})
	if p := w2.next(t); p.Err == nil || !strings.Contains(p.Err.Error(), "mounted") {
		t.Fatal(p.Err)
	}
	w2.sleepers(t, 1)
	w2.cancel()
}

// A watch with no callback runs all the same.
func TestWatchWithoutCallback(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	timer := porttest.NewTimer(now)
	engine := f.build(true, timer)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- engine.Watch(ctx, f.spec, port.MirrorRunOptions{}, nil) }()
	if !timer.WaitSleepers(ctx, 1) {
		t.Fatal("no sleeper")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// With the file watcher live, a change it hears is a pass; the directory
// is not looked at.
func TestWatchHearsTheFileWatcher(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	dirs := porttest.NewDirWatcher(true)
	w := watchWith(t, f, port.MirrorRunOptions{Watch: true}, dirs)
	want(t, w.next(t))
	f.pdfFile("New.pdf", "%PDF new", 1, time10)
	w.sleepers(t, 1)
	dirs.Change()
	w.sleepers(t, 2)
	w.timer.Wake()
	want(t, w.next(t), "upload New.pdf")
}

// A watcher that cannot start, or stops, falls back to the polling: a full
// pass at once, and the watcher tried again every rescan.
func TestWatchFallsBackToPolling(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	dirs := porttest.NewDirWatcher(true)
	w := watchWith(t, f, port.MirrorRunOptions{Watch: true}, dirs)
	want(t, w.next(t))
	w.sleepers(t, 1)
	dirs.Break(errBoom)
	want(t, w.drive(t))
	// The watcher is tried again once its wait is over.
	for i := 0; dirs.Starts() < 2; i++ {
		if i == 500 {
			t.Fatalf("the watcher was started %d times", dirs.Starts())
		}
		w.timer.Wake()
		time.Sleep(10 * time.Millisecond) //nolint:forbidigo // gives the watch's goroutines their turn
	}

	f2 := setup(t, true)
	none := porttest.NewDirWatcher(true)
	none.Err = errBoom
	w2 := watchWith(t, f2, port.MirrorRunOptions{Watch: true}, none)
	want(t, w2.next(t))
	f2.pdfFile("New.pdf", "%PDF new", 1, time10)
	// The poll sees the file.
	for {
		if p := w2.drive(t); len(p.Actions) > 0 {
			want(t, p, "upload New.pdf")
			return
		}
	}
}
