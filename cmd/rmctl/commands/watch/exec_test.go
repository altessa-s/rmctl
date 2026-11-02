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
	"io/fs"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// call is one run the fake runner saw.
type call struct {
	argv  []string
	env   map[string]string
	input execInput
}

// fakeRunner records each run; block holds a run until it is closed or the
// run's context ends, and code is every run's exit status.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	block   chan struct{}
	started chan struct{}
	code    int
	err     error
}

func (f *fakeRunner) Run(ctx context.Context, argv, env []string, stdin []byte, out io.Writer) (int, error) {
	c := call{argv: argv, env: map[string]string{}}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		c.env[k] = v
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin), &c.input); err != nil {
		return 0, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	_, _ = io.WriteString(out, "ran "+argv[0]+"\n")
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return -1, ctx.Err()
		}
	}
	return f.code, f.err
}

func (f *fakeRunner) runs() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

var execEvents = []port.Event{
	{Kind: port.EventConnected},
	{Kind: port.EventLibrary, Name: "SyncComplete", DocumentID: clitest.PaperID, VisibleName: "Secret"},
	{Kind: port.EventLibrary, Name: "DocAdded", DocumentID: clitest.BooksID, FromThisDevice: true},
	{Kind: port.EventLibrary, Name: "DocDeleted", DocumentID: "gone", VisibleName: "Gone"},
	{Kind: port.EventUnreadable, Frame: []byte("x")},
}

func runExec(t *testing.T, r runner, args ...string) clitest.Result {
	t.Helper()
	f := &clitest.Fake{Events: execEvents, WatchErr: errors.New("socket gone")}
	build := func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{newWatch(env, r)} }
	return clitest.Exec(t, f.Services(), clitest.Options{}, build, append([]string{"watch"}, args...)...)
}

func TestExecRunsOnceForABatch(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	res := runExec(t, r, "--exec", "notify", "--debounce", "1h", "--", "a b", "$RMCTL_NAME")
	if res.Code != cliutil.CodeError || !strings.Contains(res.Err, "ran notify\n") {
		t.Fatalf("%d %v %q", res.Code, res.Error, res.Err)
	}
	runs := r.runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	c := runs[0]
	// The arguments are passed as they are: nothing is put into them.
	if !slices.Equal(c.argv, []string{"notify", "a b", "$RMCTL_NAME"}) {
		t.Fatalf("%q", c.argv)
	}
	if c.env["RMCTL_EVENT"] != "DocDeleted" || c.env["RMCTL_ID"] != "gone" || c.env["RMCTL_NAME"] != "Gone" || c.env["RMCTL_PATH"] != "" ||
		c.env["RMCTL_COUNT"] != "3" || c.env["RMCTL_PROFILE"] != "default" || c.env["RMCTL_THIS_DEVICE"] != "false" || c.env["RMCTL_TIME"] == "" {
		t.Fatalf("%v", c.env)
	}
	ev := c.input.Events
	if c.input.Profile != "default" || len(ev) != 3 || ev[0].Path != "/Books/Paper" || ev[0].Name != "Paper" || ev[0].Kind != "pdf" ||
		ev[1].Path != "/Books" || ev[1].Kind != "folder" || !ev[1].ThisDevice {
		t.Fatalf("%+v", c.input)
	}
}

func TestExecFilters(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]string{
		"--only document":                         "/Books/Paper",
		"--only folder":                           "/Books",
		"--under /Books --only document":          "/Books/Paper",
		"--under /Books/":                         "/Books/Paper,/Books",
		"--under /Elsewhere":                      "",
		"--shell --exec-timeout 1m --debounce 0s": "/Books/Paper,/Books,",
	} {
		r := &fakeRunner{}
		runExec(t, r, append([]string{"--exec", "x"}, strings.Fields(args)...)...)
		var paths []string
		for _, c := range r.runs() {
			for _, e := range c.input.Events {
				paths = append(paths, e.Path)
			}
		}
		if got := strings.Join(paths, ","); got != want {
			t.Errorf("%s: %q", args, got)
		}
	}
	r := &fakeRunner{}
	runExec(t, r, "--shell", "--exec", "echo \"$RMCTL_PATH\"", "--", "zero", "one")
	if runs := r.runs(); len(runs) != 1 || !slices.Equal(runs[0].argv, []string{"sh", "-c", "echo \"$RMCTL_PATH\"", "rmctl-watch", "zero", "one"}) {
		t.Fatalf("%v", runs)
	}
}

func TestExecRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--shell"},
		{"--only", "document"},
		{"--debounce", "1s"},
		{"--", "x"},
		{"x"},
		{"--exec", "x", "y", "--", "z"},
		{"--exec", "x", "--only", "page"},
		{"--exec", "x", "--under", "Books"},
		{"--exec", "x", "--debounce", "-1s"},
		{"--exec", "x", "--exec-timeout", "0s"},
	} {
		r := &fakeRunner{}
		if res := runExec(t, r, args...); res.Code != cliutil.CodeUsage || len(r.runs()) != 0 {
			t.Errorf("%v: %d %v", args, res.Code, res.Error)
		}
	}
}

func newTestDispatcher(r runner, opts execOptions, errOut io.Writer) *dispatcher {
	if opts.argv == nil {
		opts.argv = []string{"prog"}
	}
	if opts.timeout == 0 {
		opts.timeout = time.Minute
	}
	return newDispatcher(opts, r, nil, errOut, time.Now)
}

func library(name string) port.Event {
	return port.Event{Kind: port.EventLibrary, Name: name, DocumentID: domain.ItemID(name)}
}

// Events within the debounce make one run; a later event makes the next.
func TestExecDebounce(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{started: make(chan struct{}, 4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: 30 * time.Millisecond}, &errOut)
	go d.work(t.Context())
	d.add(library("a"))
	d.add(library("b"))
	d.add(port.Event{Kind: port.EventConnected})
	<-r.started
	d.add(library("c"))
	<-r.started
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 2 || len(runs[0].input.Events) != 2 || runs[1].input.Events[0].ID != "c" {
		t.Fatalf("%+v", runs)
	}
	d.add(library("late"))
	if len(r.runs()) != 2 {
		t.Fatal("an event after close ran")
	}
}

// Runs happen one at a time; past the bound, batches join the last one
// waiting instead of being lost.
func TestExecQueueIsBounded(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, maxQueue+4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	go d.work(t.Context())
	d.add(library("first"))
	d.flush()
	<-r.started
	for i := range maxQueue + 3 {
		d.add(library("e" + string(rune('a'+i))))
		d.flush()
	}
	close(r.block)
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 1+maxQueue || len(runs[maxQueue].input.Events) != 4 {
		t.Fatalf("%d runs, the last of %d events", len(runs), len(runs[len(runs)-1].input.Events))
	}
}

func TestExecOutcomes(t *testing.T) {
	t.Parallel()
	run := func(r runner, opts execOptions) string {
		var errOut syncBuffer
		d := newTestDispatcher(r, opts, &errOut)
		go d.work(t.Context())
		d.add(library("a"))
		d.close()
		<-d.done
		return errOut.String()
	}
	if got := run(&fakeRunner{code: 3}, execOptions{}); !strings.Contains(got, "exec: prog exited 3") {
		t.Errorf("%q", got)
	}
	if got := run(&fakeRunner{err: errors.New("no such file")}, execOptions{}); !strings.Contains(got, "exec: prog: no such file") {
		t.Errorf("%q", got)
	}
	if got := run(&fakeRunner{block: make(chan struct{})}, execOptions{timeout: 20 * time.Millisecond}); !strings.Contains(got, "timed out after 20ms") {
		t.Errorf("%q", got)
	}
	// The library for the paths cannot be read: a run goes on without them,
	// unless a filter needs them.
	broken := func(context.Context) (*domain.Tree, error) { return nil, errors.New("offline") }
	for only, runs := range map[string]int{"": 1, onlyDocument: 0} {
		r := &fakeRunner{}
		var errOut syncBuffer
		d := newDispatcher(execOptions{argv: []string{"p"}, timeout: time.Minute, only: only}, r, broken, &errOut, time.Now)
		go d.work(t.Context())
		d.add(library("a"))
		d.close()
		<-d.done
		if len(r.runs()) != runs || !strings.Contains(errOut.String(), "the library for the paths: offline") {
			t.Errorf("only %q: %d runs %q", only, len(r.runs()), errOut.String())
		}
	}
}

// Ctrl-C leaves the run under way its grace period, then stops it, and
// drops the runs still waiting.
func TestExecInterrupt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour, grace: 20 * time.Millisecond}, &errOut)
	go d.work(ctx)
	d.add(library("a"))
	d.flush()
	<-r.started
	d.add(library("b"))
	d.flush()
	cancel()
	<-d.done
	if got := errOut.String(); !strings.Contains(got, "exec: prog was stopped at interrupt") || !strings.Contains(got, "1 runs dropped at interrupt") || len(r.runs()) != 1 {
		t.Fatalf("%q %d", got, len(r.runs()))
	}
}

// The real runner passes the environment and standard input, and answers
// the exit status.
func TestExecRunner(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	var out bytes.Buffer
	code, err := execRunner{}.Run(t.Context(), []string{"sh", "-c", `cat; printf '%s\n' "$RMCTL_X"; exit 3`}, []string{"RMCTL_X=; rm -rf /"}, []byte("in\n"), &out)
	if err != nil || code != 3 || out.String() != "in\n; rm -rf /\n" {
		t.Fatalf("%d %v %q", code, err, out.String())
	}
	// A program that never starts has no exit status: -1 and the start
	// error, never a panic on the missing process state.
	if code, err := (execRunner{}).Run(t.Context(), []string{"/no/such/program"}, nil, nil, &out); code != -1 || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing program: %d %v", code, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := (execRunner{}).Run(ctx, []string{"sleep", "5"}, nil, nil, &out); err == nil {
		t.Fatal("a stopped run succeeded")
	}
}

// syncBuffer is a buffer the dispatcher and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// watch --exec with a program that is not there, run by the real runner
// without a shell: each batch reports the start error, and watch carries
// on to the next one and to its own end.
func TestExecMissingProgramWithTheRealRunner(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	const prog = "/no/such/program"
	f := &clitest.Fake{Events: execEvents, WatchErr: errors.New("socket gone")}
	build := func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{newWatch(env, execRunner{})} }
	res := clitest.Exec(t, f.Services(), clitest.Options{}, build, "watch", "--exec", prog, "--debounce", "1h")
	// The run of the one batch failed to start; watch still ended on the
	// socket's own error, not on the program.
	if res.Code != cliutil.CodeError || !strings.Contains(res.Error.Error(), "socket gone") {
		t.Fatalf("%d %v", res.Code, res.Error)
	}
	if !strings.Contains(res.Err, "exec: "+prog+": ") || !strings.Contains(res.Err, "no such file or directory") ||
		strings.Contains(res.Err, "exited") {
		t.Fatalf("%q", res.Err)
	}
}

// A burst within one debounce window keeps the newest maxBatch events and
// counts the rest; the pending batch never grows past the bound.
func TestExecBatchIsBoundedWithinOneWindow(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	const burst = 2500
	for i := range burst {
		d.add(library("e" + strconv.Itoa(i)))
		d.mu.Lock()
		n, c := len(d.pending.events), cap(d.pending.events)
		d.mu.Unlock()
		if n > maxBatch || c > 2*maxBatch {
			t.Fatalf("after %d events the pending batch holds %d (capacity %d)", i+1, n, c)
		}
	}
	go d.work(t.Context())
	d.close()
	<-d.done
	runs := r.runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	in := runs[0].input
	if len(in.Events) != maxBatch || in.Dropped != burst-maxBatch || in.Events[0].ID != "e1500" || in.Events[maxBatch-1].ID != "e2499" ||
		runs[0].env["RMCTL_DROPPED"] != "1500" || runs[0].env["RMCTL_COUNT"] != "1000" {
		t.Fatalf("%d events, dropped %d, env %v", len(in.Events), in.Dropped, runs[0].env["RMCTL_DROPPED"])
	}
	if got := errOut.String(); strings.Count(got, "older events of this batch were dropped") != 1 || !strings.Contains(got, "exec: 1500 older events") {
		t.Fatalf("%q", got)
	}
}

// Merged into the last batch of a full queue, the batch keeps the bound
// and adds up what both dropped.
func TestExecMergeRespectsTheBound(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, maxQueue+4)}
	var errOut syncBuffer
	d := newTestDispatcher(r, execOptions{debounce: time.Hour}, &errOut)
	go d.work(t.Context())
	d.add(library("first"))
	d.flush()
	<-r.started
	for i := range maxQueue {
		d.add(library("q" + strconv.Itoa(i)))
		d.flush()
	}
	// The queue is full: 1500 more events, 500 dropped while pending and
	// one more when they join the last batch waiting.
	for i := range 1500 {
		d.add(library("m" + strconv.Itoa(i)))
	}
	d.flush()
	d.mu.Lock()
	last := d.queue[len(d.queue)-1]
	d.mu.Unlock()
	if len(last.events) != maxBatch || last.dropped != 501 {
		t.Fatalf("the last batch holds %d, dropped %d", len(last.events), last.dropped)
	}
	close(r.block)
	d.close()
	<-d.done
	runs := r.runs()
	in := runs[len(runs)-1].input
	if len(runs) != 1+maxQueue || len(in.Events) != maxBatch || in.Dropped != 501 || in.Events[0].ID != "m500" || in.Events[maxBatch-1].ID != "m1499" {
		t.Fatalf("%d runs; the last of %d events, dropped %d", len(runs), len(in.Events), in.Dropped)
	}
	if in := runs[1].input; in.Dropped != 0 || runs[1].env["RMCTL_DROPPED"] != "0" {
		t.Fatalf("a whole batch reports drops: %+v", in)
	}
}

// The command gets each RMCTL_ variable once, with the event's value, even
// when rmctl's own environment already holds a stale one.
func TestExecRunnerReplacesInheritedVariables(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	r := execRunner{environ: func() []string {
		return []string{"RMCTL_EVENT=stale", "RMCTL_PATH=stale", "RMCTL_KEEP=kept", "PATH=" + os.Getenv("PATH")}
	}}
	var out bytes.Buffer
	if _, err := r.Run(t.Context(), []string{"env"}, []string{"RMCTL_EVENT=DocAdded", "RMCTL_PATH=/Books/Paper"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	seen := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		k, v, _ := strings.Cut(line, "=")
		seen[k] = append(seen[k], v)
	}
	for k, want := range map[string]string{"RMCTL_EVENT": "DocAdded", "RMCTL_PATH": "/Books/Paper", "RMCTL_KEEP": "kept"} {
		if !slices.Equal(seen[k], []string{want}) {
			t.Errorf("%s: %q, want once %q", k, seen[k], want)
		}
	}
}

func TestChildEnv(t *testing.T) {
	t.Parallel()
	base := []string{"rmctl_event=stale", "RMCTL_PATH=stale", "=C:=C:\\x", "HOME=/h", "ODD"}
	set := []string{"RMCTL_EVENT=new", "RMCTL_PATH=/p"}
	// Keys compare with case on POSIX: rmctl_event is another variable.
	if got := childEnv(base, set, false); !slices.Equal(got, []string{"rmctl_event=stale", "=C:=C:\\x", "HOME=/h", "ODD", "RMCTL_EVENT=new", "RMCTL_PATH=/p"}) {
		t.Fatalf("%q", got)
	}
	// Without case on Windows; the per-drive entry is kept.
	if got := childEnv(base, set, true); !slices.Equal(got, []string{"=C:=C:\\x", "HOME=/h", "ODD", "RMCTL_EVENT=new", "RMCTL_PATH=/p"}) {
		t.Fatalf("%q", got)
	}
	for kv, key := range map[string]string{"A=b": "A", "=C:=C:\\x": "=C:", "ODD": "ODD", "": "", "K==v": "K"} {
		if got := envKey(kv); got != key {
			t.Errorf("envKey(%q) = %q, want %q", kv, got, key)
		}
	}
}
