// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package watch

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// The defaults of --exec.
const (
	// DefaultDebounce is how long events gather before one run.
	DefaultDebounce = 2 * time.Second
	// DefaultExecTimeout bounds one run.
	DefaultExecTimeout = 5 * time.Minute
	// execGrace is how long Ctrl-C waits for a running command.
	execGrace = 5 * time.Second
	// maxQueue bounds the runs waiting; past it, a batch joins the last
	// one waiting rather than being lost.
	maxQueue = 16
	// maxBatch bounds the events of one run; past it the oldest go.
	maxBatch = 1000
	// waitDelay is how long a finished command's output may still flow.
	waitDelay = time.Second
)

// The values of --only.
const (
	onlyDocument = "document"
	onlyFolder   = "folder"
)

// execOptions shape --exec.
type execOptions struct {
	// argv is the program and its arguments, as they are run: no shell
	// unless --shell put one there.
	argv     []string
	debounce time.Duration
	timeout  time.Duration
	grace    time.Duration
	// only is "", onlyDocument or onlyFolder; under a folder path the
	// item must lie below, "" for anywhere.
	only    string
	under   string
	profile string
}

// runner runs one command: argv with env added to rmctl's own environment,
// stdin on its standard input, and both its outputs on out. It answers
// the command's exit code.
type runner interface {
	Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error)
}

// execRunner runs commands as processes. ctx ending kills the process.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- the user's own command, run as argv without a shell
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && ctx.Err() == nil {
		return exitErr.ExitCode(), nil
	}
	return cmd.ProcessState.ExitCode(), err
}

// execEvent is one event as a run receives it.
type execEvent struct {
	Time       string `json:"time"`
	Event      string `json:"event"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	ThisDevice bool   `json:"this_device"`
}

// execInput is what a run reads on its standard input.
type execInput struct {
	Profile string      `json:"profile"`
	Events  []execEvent `json:"events"`
}

// dispatcher gathers events into batches and runs the command for each,
// one run at a time.
type dispatcher struct {
	opts   execOptions
	run    runner
	tree   func(ctx context.Context) (*domain.Tree, error)
	errOut io.Writer
	now    func() time.Time

	mu      sync.Mutex
	pending []port.Event
	stamps  []time.Time
	timer   *time.Timer
	queue   [][]stampedEvent
	closed  bool
	wake    chan struct{}
	done    chan struct{}
}

type stampedEvent struct {
	at time.Time
	e  port.Event
}

func newDispatcher(opts execOptions, r runner, tree func(context.Context) (*domain.Tree, error), errOut io.Writer, now func() time.Time) *dispatcher {
	return &dispatcher{
		opts: opts, run: r, tree: tree, errOut: errOut, now: now,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
}

// add takes a library event; a run follows once the debounce from the
// first event of the batch has passed.
func (d *dispatcher) add(e port.Event) {
	if e.Kind != port.EventLibrary {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.pending = append(d.pending, e)
	d.stamps = append(d.stamps, d.now())
	if d.timer == nil {
		d.timer = time.AfterFunc(d.opts.debounce, d.flush)
	}
}

func (d *dispatcher) flush() {
	d.mu.Lock()
	d.flushLocked()
	d.mu.Unlock()
	d.signal()
}

// flushLocked moves the pending events into the queue as one batch, or
// into the last batch waiting when the queue is full.
func (d *dispatcher) flushLocked() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if len(d.pending) == 0 {
		return
	}
	batch := make([]stampedEvent, len(d.pending))
	for i, e := range d.pending {
		batch[i] = stampedEvent{at: d.stamps[i], e: e}
	}
	d.pending, d.stamps = nil, nil
	if len(d.queue) < maxQueue {
		d.queue = append(d.queue, batch)
		return
	}
	last := len(d.queue) - 1
	d.queue[last] = append(d.queue[last], batch...)
	if n := len(d.queue[last]); n > maxBatch {
		d.queue[last] = d.queue[last][n-maxBatch:]
	}
}

func (d *dispatcher) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// close sends what is pending at once; the runs waiting still happen.
func (d *dispatcher) close() {
	d.mu.Lock()
	d.closed = true
	d.flushLocked()
	d.mu.Unlock()
	d.signal()
}

// work runs the batches in order until close and an empty queue, or until
// ctx ends: then the run under way has the grace period to finish, and the
// batches still waiting are dropped.
func (d *dispatcher) work(ctx context.Context) {
	defer close(d.done)
	for {
		d.mu.Lock()
		if ctx.Err() != nil {
			if n := len(d.queue); n > 0 {
				cliutil.Notef(d.errOut, "exec: %d runs dropped at interrupt", n)
			}
			d.queue = nil
			d.mu.Unlock()
			return
		}
		if len(d.queue) == 0 {
			closed := d.closed
			d.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-d.wake:
			case <-ctx.Done():
			}
			continue
		}
		batch := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()
		d.runBatch(ctx, batch)
	}
}

// runBatch runs the command once for batch, its events filtered and
// described with the library's paths.
func (d *dispatcher) runBatch(ctx context.Context, batch []stampedEvent) {
	var tree *domain.Tree
	if d.tree != nil {
		t, err := d.tree(ctx)
		if err != nil {
			cliutil.Notef(d.errOut, "exec: the library for the paths: %v", err)
			if d.opts.only != "" || d.opts.under != "" {
				return
			}
		}
		tree = t
	}
	events := make([]execEvent, 0, len(batch))
	for _, s := range batch {
		ev, ok := d.describe(tree, s)
		if ok {
			events = append(events, ev)
		}
	}
	if len(events) == 0 {
		return
	}
	last := events[len(events)-1]
	env := []string{
		"RMCTL_EVENT=" + last.Event,
		"RMCTL_ID=" + last.ID,
		"RMCTL_NAME=" + last.Name,
		"RMCTL_PATH=" + last.Path,
		"RMCTL_KIND=" + last.Kind,
		"RMCTL_THIS_DEVICE=" + strconv.FormatBool(last.ThisDevice),
		"RMCTL_TIME=" + last.Time,
		"RMCTL_COUNT=" + strconv.Itoa(len(events)),
		"RMCTL_PROFILE=" + d.opts.profile,
	}
	stdin, err := json.Marshal(execInput{Profile: d.opts.profile, Events: events}, json.Deterministic(true))
	if err != nil {
		cliutil.Notef(d.errOut, "exec: %v", err)
		return
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.opts.timeout)
	defer cancel()
	// Ctrl-C leaves the run its grace period, then ends it.
	stop := context.AfterFunc(ctx, func() {
		t := time.AfterFunc(d.opts.grace, cancel)
		<-runCtx.Done()
		t.Stop()
	})
	defer stop()
	code, err := d.run.Run(runCtx, d.opts.argv, env, append(stdin, '\n'), d.errOut)
	prog := d.opts.argv[0]
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		cliutil.Notef(d.errOut, "exec: %s timed out after %s and was stopped", prog, d.opts.timeout)
	case ctx.Err() != nil && runCtx.Err() != nil:
		cliutil.Notef(d.errOut, "exec: %s was stopped at interrupt", prog)
	case err != nil:
		cliutil.Notef(d.errOut, "exec: %s: %v", prog, err)
	case code != 0:
		cliutil.Notef(d.errOut, "exec: %s exited %d", prog, code)
	}
}

// describe is an event for the command, with the item's path and kind
// from tree; ok false when --only or --under leave it out. An item the
// library no longer holds has no path or kind, and passes only when
// neither filter is set.
func (d *dispatcher) describe(tree *domain.Tree, s stampedEvent) (execEvent, bool) {
	e := s.e
	ev := execEvent{
		Time: s.at.UTC().Format(time.RFC3339), Event: e.Name, ID: string(e.DocumentID),
		Name: e.VisibleName, ThisDevice: e.FromThisDevice,
	}
	if tree != nil {
		if it, ok := tree.Get(e.DocumentID); ok {
			ev.Name, ev.Path, ev.Kind = it.Name, tree.Path(it.ID), string(it.Kind)
		}
	}
	switch d.opts.only {
	case onlyFolder:
		if ev.Kind != string(domain.KindFolder) {
			return ev, false
		}
	case onlyDocument:
		if ev.Kind == "" || ev.Kind == string(domain.KindFolder) {
			return ev, false
		}
	}
	if u := strings.TrimSuffix(d.opts.under, "/"); u != "" {
		if ev.Path == "" || (ev.Path != u && !strings.HasPrefix(ev.Path, u+"/")) {
			return ev, false
		}
	}
	return ev, true
}

// lockedWriter serializes the writes of several goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
