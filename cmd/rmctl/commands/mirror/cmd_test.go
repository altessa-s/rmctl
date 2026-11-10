// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/mirror"
	"github.com/altessa-s/rmctl/internal/port"
)

var errBoom = errors.New("boom")

// when is the time of every pass the doubles report.
var when = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test instant

// mirrors is port.Mirrors in memory: it records what it was asked, and
// answers with what a test set.
type mirrors struct {
	mu      sync.Mutex
	added   port.MirrorSpec
	states  []port.MirrorState
	removed port.MirrorSpec
	sel     port.MirrorSelection
	opts    port.MirrorRunOptions
	passes  []port.MirrorPass
	err     error
	// block makes Run wait for its context, as a watch does.
	block bool
}

func (m *mirrors) Add(_ context.Context, spec port.MirrorSpec) (port.MirrorSpec, error) {
	m.added = spec
	return spec, m.err
}

func (m *mirrors) List(context.Context) ([]port.MirrorState, error) { return m.states, m.err }

func (m *mirrors) Remove(_ context.Context, name string) (port.MirrorSpec, error) {
	m.removed = port.MirrorSpec{Name: name, Dir: "/d", Folder: "/F"}
	return m.removed, m.err
}

func (m *mirrors) Run(ctx context.Context, sel port.MirrorSelection, opts port.MirrorRunOptions, on func(port.MirrorPass)) error {
	m.mu.Lock()
	m.sel, m.opts = sel, opts
	m.mu.Unlock()
	for _, p := range m.passes {
		on(p)
	}
	if m.block {
		<-ctx.Done()
		return nil
	}
	return m.err
}

// service is port.MirrorService in memory.
type service struct {
	info  port.ServiceInfo
	lines []string
	log   *strings.Builder
	err   error
}

func (s *service) Install(context.Context) (port.ServiceInfo, error)   { return s.info, s.err }
func (s *service) Uninstall(context.Context) (port.ServiceInfo, error) { return s.info, s.err }
func (s *service) Status(context.Context) (port.ServiceInfo, error)    { return s.info, s.err }

func (s *service) Log(ctx context.Context, lines int, follow bool, on func(string)) error {
	for _, l := range s.lines[max(len(s.lines)-lines, 0):] {
		on(l)
	}
	if follow && s.err == nil {
		<-ctx.Done()
	}
	return s.err
}

func (s *service) OpenLog() (io.WriteCloser, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nopCloser{s.log}, nil
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func run(t *testing.T, m *mirrors, s *service, opts clitest.Options, args ...string) clitest.Result {
	t.Helper()
	svc := cliutil.Services{Mirrors: m, MirrorService: s}
	return clitest.Exec(t, svc, opts, func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{mirror.New(env)} }, args...)
}

func TestAdd(t *testing.T) {
	t.Parallel()
	m := &mirrors{}
	r := run(t, m, &service{}, clitest.Options{}, "-P", "work", "mirror", "add", "papers", "rel/dir", "/Papers", "--pull-new", "--no-delete")
	if r.Code != 0 || !strings.Contains(r.Out, "name\tpapers\n") || !strings.Contains(r.Out, "profile\twork\n") || !strings.Contains(r.Err, "mirror run papers") {
		t.Fatalf("%+v", r)
	}
	if !filepath.IsAbs(m.added.Dir) || !strings.HasSuffix(m.added.Dir, filepath.Join("rel", "dir")) || !m.added.PullNew || !m.added.NoDelete || m.added.Profile != "work" {
		t.Fatalf("%+v", m.added)
	}
	r = run(t, &mirrors{}, &service{}, clitest.Options{}, "mirror", "add", "papers", "/d", "/Papers", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"profile": ""`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: port.ErrMirrorExists}, &service{}, clitest.Options{}, "mirror", "add", "a", "/d", "/F"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: errBoom}, &service{}, clitest.Options{}, "mirror", "add", "a", "/d", "/F"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, &service{}, clitest.Options{}, "mirror", "add", "a"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%+v", r)
	}
}

func TestLs(t *testing.T) {
	t.Parallel()
	m := &mirrors{states: []port.MirrorState{
		{Spec: port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"}, Tracked: 3, LastPass: when, Pending: 1, LocalChanges: 2},
		{Spec: port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/B", Profile: "work"}},
		{Spec: port.MirrorSpec{Name: "c", Dir: "/c", Folder: "/C"}, LastPass: when, LastError: "it broke"},
		{Spec: port.MirrorSpec{Name: "d", Dir: "/d", Folder: "/D"}, Err: errBoom},
	}}
	r := run(t, m, &service{}, clitest.Options{}, "mirror", "ls")
	for _, want := range []string{"a\t/a\t/A\t(the run's)\t3\t2026-11-10T12:00:00Z\t1\t2\tok", "b\t/b\t/B\twork\t0\t-\t0\t0\tnever run", "it broke", "d\t/d\t/D\t(the run's)\t0\t-\t0\t0\tboom"} {
		if !strings.Contains(r.Out, want) {
			t.Errorf("missing %q in\n%s", want, r.Out)
		}
	}
	r = run(t, m, &service{}, clitest.Options{}, "mirror", "ls", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"local_changes": 2`) || !strings.Contains(r.Out, `"error": "boom"`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: errBoom}, &service{}, clitest.Options{}, "mirror", "ls"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
}

func TestRm(t *testing.T) {
	t.Parallel()
	m := &mirrors{}
	r := run(t, m, &service{}, clitest.Options{}, "mirror", "rm", "papers")
	if r.Code != 0 || m.removed.Name != "papers" || !strings.Contains(r.Err, "nothing was deleted: /d and the cloud folder /F stay") {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: port.ErrNoMirror}, &service{}, clitest.Options{}, "mirror", "rm", "x"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: errBoom}, &service{}, clitest.Options{}, "mirror", "rm", "x"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
}

// A pass prints a line per action, then one that sums the pass up.
func TestRun(t *testing.T) {
	t.Parallel()
	m := &mirrors{passes: []port.MirrorPass{
		{Mirror: "a", Ended: when, Pending: 1, Lite: true, Actions: []port.MirrorAction{
			{Kind: port.MirrorUpload, Path: "New.pdf", Document: "d1"},
			{Kind: port.MirrorDraw, Path: "Paper.pdf", To: "_annotated/Paper.pdf"},
			{Kind: port.MirrorTrash, Path: "Old.pdf", Err: errBoom},
		}},
		{Dir: "/adhoc", Ended: when},
		{Mirror: "c", Ended: when, Err: errBoom},
		{Mirror: "d", Ended: when, DryRun: true, Actions: []port.MirrorAction{{Kind: port.MirrorRemove, Path: "x.pdf"}}},
	}}
	r := run(t, m, &service{}, clitest.Options{}, "mirror", "run", "a", "b", "--dry-run", "--force-delete")
	for _, want := range []string{
		"2026-11-10T12:00:00Z\ta\tupload\tNew.pdf\t-\t-\n",
		"a\tdraw\tPaper.pdf\t_annotated/Paper.pdf\t-\n",
		"a\ttrash\tOld.pdf\t-\tfailed: boom\n",
		"a\tpass\t-\t-\t3 actions; 1 pending; rmctl-lite: annotated documents are exported as .rmdoc\n",
		"/adhoc\tpass\t-\t-\tin step\n",
		"c\tpass\t-\t-\tfailed: boom\n",
		"d\tpass\t-\t-\tdry run: 1 actions\n",
	} {
		if !strings.Contains(r.Out, want) {
			t.Errorf("missing %q in\n%s", want, r.Out)
		}
	}
	if strings.Join(m.sel.Names, " ") != "a b" || !m.opts.DryRun || !m.opts.ForceDelete || m.opts.Watch {
		t.Fatalf("%+v %+v", m.sel, m.opts)
	}
	r = run(t, &mirrors{passes: m.passes[:1]}, &service{}, clitest.Options{}, "mirror", "run", "--all", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"action": "upload"`) || !strings.Contains(r.Out, `"document": "d1"`) || !strings.Contains(r.Out, `"pending": 1`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{err: errBoom}, &service{}, clitest.Options{}, "mirror", "run", "--all"); r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "mirror run: boom") {
		t.Fatalf("%+v", r)
	}
}

// An ad-hoc mirror asks first, unless --yes or --dry-run.
func TestRunAdHoc(t *testing.T) {
	t.Parallel()
	m := &mirrors{}
	r := run(t, m, &service{}, clitest.Options{}, "mirror", "run", "--dir", "rel", "--folder", "/F", "--pull-new", "--no-delete")
	if r.Code != cliutil.CodeUsage || !strings.Contains(r.Error.Error(), "--yes") {
		t.Fatalf("%+v", r)
	}
	r = run(t, m, &service{}, clitest.Options{}, "-P", "work", "mirror", "run", "--dir", "rel", "--folder", "/F", "--pull-new", "--no-delete", "--yes")
	if r.Code != 0 || m.sel.AdHoc == nil || !filepath.IsAbs(m.sel.AdHoc.Dir) || m.sel.AdHoc.Profile != "work" || !m.sel.AdHoc.PullNew || !m.sel.AdHoc.NoDelete {
		t.Fatalf("%+v %+v", r, m.sel.AdHoc)
	}
	if r = run(t, m, &service{}, clitest.Options{}, "mirror", "run", "--dir", "rel", "--folder", "/F", "--dry-run"); r.Code != 0 {
		t.Fatalf("%+v", r)
	}
	r = run(t, m, &service{}, clitest.Options{Terminal: true, Stdin: "n\n"}, "mirror", "run", "--dir", "rel", "--folder", "/F")
	if !errors.Is(r.Error, cliutil.ErrDeclined) {
		t.Fatalf("%+v", r)
	}
}

func TestRunRefusesFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"mirror", "run"},
		{"mirror", "run", "a", "--all"},
		{"mirror", "run", "--dir", "/d"},
		{"mirror", "run", "--dir", "/d", "--folder", "/F", "a"},
		{"mirror", "run", "--pull-new", "a"},
		{"mirror", "run", "--service", "a"},
		{"mirror", "run", "--service", "--dry-run"},
		{"mirror", "run", "a", "--poll", "1s"},
		{"mirror", "run", "a", "--watch", "--debounce", "-1s"},
	} {
		if r := run(t, &mirrors{}, &service{}, clitest.Options{}, args...); r.Code != cliutil.CodeUsage {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

// A watch runs until it is stopped, which is an interruption.
func TestRunWatch(t *testing.T) {
	t.Parallel()
	m := &mirrors{block: true}
	ctx, cancel := context.WithCancelCause(t.Context())
	go func() {
		for {
			m.mu.Lock()
			watching := m.opts.Watch
			m.mu.Unlock()
			if watching {
				cancel(cliutil.ErrInterrupted)
				return
			}
			time.Sleep(time.Millisecond) //nolint:forbidigo // waits for the command to start its run
		}
	}()
	r := run(t, m, &service{}, clitest.Options{Context: ctx}, "mirror", "run", "--all", "--watch", "--poll", "1s", "--debounce", "0s", "--rescan", "1h")
	if r.Code != cliutil.CodeInterrupted || m.opts.Poll != time.Second || m.opts.Rescan != time.Hour {
		t.Fatalf("%+v %+v", r, m.opts)
	}
}

// As the service, the run goes into the service's log, as plain lines.
func TestRunAsTheService(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	m := &mirrors{passes: []port.MirrorPass{{Mirror: "a", Ended: when}}}
	r := run(t, m, &service{log: &log}, clitest.Options{OutTerminal: true}, "mirror", "run", "--service")
	if r.Code != 0 || r.Out != "" || !strings.Contains(log.String(), "a\tpass\t-\t-\tin step") || !m.sel.All || !m.opts.Watch {
		t.Fatalf("%+v %q %+v", r, log.String(), m.sel)
	}
	if r = run(t, m, &service{err: errBoom}, clitest.Options{}, "mirror", "run", "--service"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
}

func TestService(t *testing.T) {
	t.Parallel()
	s := &service{info: port.ServiceInfo{Manager: "launchd", Name: "io.altessa.rmctl.mirror", File: "/p.plist", Installed: true, Running: true,
		Log: "/l/mirror.log", Notes: []string{"a note"}}}
	for _, action := range []string{"install", "uninstall", "status"} {
		r := run(t, &mirrors{}, s, clitest.Options{}, "mirror", "service", action)
		if r.Code != 0 || !strings.Contains(r.Out, "manager\tlaunchd\n") || !strings.Contains(r.Out, "running\ttrue\n") || !strings.Contains(r.Err, "a note") {
			t.Fatalf("%s: %+v", action, r)
		}
	}
	r := run(t, &mirrors{}, &service{}, clitest.Options{}, "mirror", "service", "status", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"notes": []`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, &service{err: errBoom}, clitest.Options{}, "mirror", "service", "install"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
}

func TestLogs(t *testing.T) {
	t.Parallel()
	s := &service{lines: []string{"one", "two", "three"}}
	r := run(t, &mirrors{}, s, clitest.Options{}, "mirror", "service", "logs", "-n", "2")
	if r.Code != 0 || r.Out != "two\nthree\n" {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, s, clitest.Options{}, "mirror", "service", "logs", "--json"); !strings.Contains(r.Out, `"lines": [`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, &service{}, clitest.Options{}, "mirror", "service", "logs", "--json"); !strings.Contains(r.Out, `"lines": []`) {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, s, clitest.Options{}, "mirror", "service", "logs", "-n", "0"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, &service{err: errBoom}, clitest.Options{}, "mirror", "service", "logs"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cliutil.ErrInterrupted)
	r = run(t, &mirrors{}, s, clitest.Options{Context: ctx}, "mirror", "service", "logs", "-f", "-n", "1")
	if r.Code != cliutil.CodeInterrupted || r.Out != "three\n" {
		t.Fatalf("%+v", r)
	}
	if r = run(t, &mirrors{}, &service{err: errBoom}, clitest.Options{}, "mirror", "service", "logs", "-f"); r.Code != cliutil.CodeError {
		t.Fatalf("%+v", r)
	}
}

// Without the services the build lacks, the commands say so.
func TestNoService(t *testing.T) {
	t.Parallel()
	r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{mirror.New(env)} },
		"mirror", "ls")
	if !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatalf("%+v", r)
	}
	r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, func(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{mirror.New(env)} },
		"mirror", "service", "status")
	if !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatalf("%+v", r)
	}
}
