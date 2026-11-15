// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

// The timing of a watch.
const (
	// DefaultPoll is how often a watch looks at the local directory when
	// the system's file watcher is not watching it: a listing of names,
	// sizes and times, nothing read.
	DefaultPoll = 3 * time.Second
	// DefaultDebounce is how long a watch waits for changes to settle
	// before a pass: the changes of that while make one pass.
	DefaultDebounce = 2 * time.Second
	// DefaultRescan is how often a watch makes a full pass in any case, for
	// a notification missed.
	DefaultRescan = 15 * time.Minute
	// firstBackoff and maxBackoff bound the wait after a pass that failed
	// as a whole: it doubles from the first to the most.
	firstBackoff = 5 * time.Second
	maxBackoff   = 5 * time.Minute
	// backoffGrowth is what each failure multiplies the wait by.
	backoffGrowth = 2
	// jumpSlack is how much later than due a look at the directory must
	// come for the machine to have slept in between.
	jumpSlack = 30 * time.Second
	// maxQuiet is the longest changes that never stop hold a pass back: a
	// file written to all the time keeps nothing else waiting.
	maxQuiet = 30 * time.Second
)

// timing is opts with the defaults filled in.
func timing(opts port.MirrorRunOptions) (poll, debounce, rescan time.Duration) {
	poll, debounce, rescan = opts.Poll, opts.Debounce, opts.Rescan
	if poll <= 0 {
		poll = DefaultPoll
	}
	if debounce < 0 {
		debounce = 0
	} else if debounce == 0 {
		debounce = DefaultDebounce
	}
	if rescan <= 0 {
		rescan = DefaultRescan
	}
	return poll, debounce, rescan
}

// Watch implements port.MirrorEngine. It holds the directory's lock the
// whole time, waiting for it, and for the directory to be there, as long
// as it must. A pass follows every change the notification socket
// announces and every change of the local files, once they settle (or
// after maxQuiet of changes that do not): the system's file watcher hears
// of them at once, and while it cannot watch the directory, a look at it
// every poll interval does. A file a pass found still being written is
// taken by another pass once it has settled. A full pass follows a
// reconnection of the socket, a watcher that stopped watching, a wake from
// sleep (a look at the clock much later than due) and every rescan
// interval. A pass that fails as a whole is tried again after a wait that
// doubles up to five minutes; a step that fails waits for the next pass.
func (e *Engine) Watch(ctx context.Context, spec port.MirrorSpec, opts port.MirrorRunOptions, on func(port.MirrorPass)) error {
	if on == nil {
		on = func(port.MirrorPass) {}
	}
	release, ok := e.waitLock(ctx, spec, opts, on)
	if !ok {
		return nil
	}
	defer func() { _ = release() }()
	poll, debounce, rescan := timing(opts)
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	wake := make(chan struct{}, 1)
	trigger := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	var watched atomic.Bool
	wg.Go(func() { e.listen(ctx, trigger) })
	wg.Go(func() { e.watchDir(ctx, spec.Dir, rescan, &watched, trigger) })
	// The directory as it is before the first pass: looked at here, not by
	// the polling goroutine once it gets to run, a change made right after
	// that pass would already be part of what the polling compares with,
	// and go unseen until the next rescan.
	seen := e.look(spec.Dir)
	wg.Go(func() { e.poll(ctx, spec.Dir, seen, poll, rescan, &watched, trigger) })
	backoff := time.Duration(0)
	for {
		p := e.pass(ctx, spec, opts)
		if ctx.Err() != nil {
			return nil
		}
		on(p)
		if p.Err != nil && !errors.Is(p.Err, errSteps) {
			backoff = min(max(backoff*backoffGrowth, firstBackoff), maxBackoff)
			if e.timer.Sleep(ctx, backoff) != nil {
				return nil
			}
			continue
		}
		backoff = 0
		if writing(p) {
			// A file still being written when the pass looked makes no change
			// once it is done: another pass takes it once it has settled.
			if e.timer.Sleep(ctx, settle) != nil {
				return nil
			}
			continue
		}
		if !e.settle(ctx, wake, debounce) {
			return nil
		}
	}
}

// writing reports whether the pass skipped a file still being written.
func writing(p port.MirrorPass) bool {
	return slices.ContainsFunc(p.Actions, func(a port.MirrorAction) bool { return a.Kind == port.MirrorSkip && a.Detail == stillWriting })
}

// settle waits for a change, then for the changes to stop coming for
// debounce, or maxQuiet at the most; false is ctx ended.
func (e *Engine) settle(ctx context.Context, wake <-chan struct{}, debounce time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-wake:
	}
	first := e.timer.Now()
	for {
		if e.timer.Sleep(ctx, debounce) != nil {
			return false
		}
		select {
		case <-wake:
		default:
			return true
		}
		if e.timer.Now().Sub(first) >= maxQuiet {
			return true
		}
	}
}

// waitLock takes the directory's lock, trying again with the growing wait
// while the directory is not there or another process holds it.
func (e *Engine) waitLock(ctx context.Context, spec port.MirrorSpec, opts port.MirrorRunOptions, on func(port.MirrorPass)) (func() error, bool) {
	backoff := time.Duration(0)
	for {
		release, err := e.lock(spec)
		if err == nil {
			return release, true
		}
		on(e.failed(e.started(spec, opts), err))
		backoff = min(max(backoff*backoffGrowth, firstBackoff), maxBackoff)
		if e.timer.Sleep(ctx, backoff) != nil {
			return nil, false
		}
	}
}

// listen calls trigger on every library event, and on every connection of
// the socket after the first: what happened while it was down is unknown.
func (e *Engine) listen(ctx context.Context, trigger func()) {
	connected := false
	_ = e.notifier.Listen(ctx, func(ev port.Event) { //nolint:errcheck // without notifications the polling and rescans keep the mirror in step
		switch ev.Kind {
		case port.EventConnected:
			if connected {
				trigger()
			}
			connected = true
		case port.EventLibrary:
			trigger()
		case port.EventDropped, port.EventUnreadable:
		}
	})
}

// watchDir has the system's file watcher call trigger on every change
// below the directory, watched says while it watches. A watcher that
// cannot start, or stops, calls trigger, since it may have missed changes,
// and is tried again every rescan interval; meanwhile poll looks at the
// directory.
func (e *Engine) watchDir(ctx context.Context, dir string, rescan time.Duration, watched *atomic.Bool, trigger func()) {
	for {
		_ = e.dirs.Watch(ctx, dir, pruned, func() { watched.Store(true) }, trigger) //nolint:errcheck // polling stands in
		watched.Store(false)
		if ctx.Err() != nil {
			return
		}
		trigger()
		if e.timer.Sleep(ctx, rescan) != nil {
			return
		}
	}
}

// poll calls trigger when a look at the clock comes much later than due
// (the machine slept, and the socket and the watcher may have missed
// events), and every rescan; while watched is false it also looks at the
// directory every interval, and calls trigger when its files are not what
// last, and then its own previous look, saw.
func (e *Engine) poll(ctx context.Context, dir, last string, interval, rescan time.Duration, watched *atomic.Bool, trigger func()) {
	// Wall time, not the monotonic clock, which stops while the machine
	// sleeps.
	tick := e.timer.Now().Round(0)
	full := tick
	for e.timer.Sleep(ctx, interval) == nil {
		now := e.timer.Now().Round(0)
		if now.Sub(tick) > interval+jumpSlack || now.Sub(full) >= rescan {
			full = now
			trigger()
		}
		tick = now
		if watched.Load() {
			continue
		}
		if sig := e.look(dir); sig != last {
			last = sig
			trigger()
		}
	}
}

// look is what the files below dir are now, as a string that differs when
// they do.
func (e *Engine) look(dir string) string {
	l, err := scan(e.disk, dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	return signature(l)
}
