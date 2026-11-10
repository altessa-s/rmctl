// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// mirrorConfig is config.toml's mirrors in memory.
type mirrorConfig struct {
	mu      sync.Mutex
	specs   []port.MirrorSpec
	fail    error
	removed bool
}

func (c *mirrorConfig) Mirrors() ([]port.MirrorSpec, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.specs), c.fail
}

func (c *mirrorConfig) AddMirror(spec port.MirrorSpec) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specs = append(c.specs, spec)
	return nil
}

func (c *mirrorConfig) RemoveMirror(name string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.specs)
	c.specs = slices.DeleteFunc(c.specs, func(s port.MirrorSpec) bool { return s.Name == name })
	return len(c.specs) < n && !c.removed, nil
}

func (c *mirrorConfig) set(specs ...port.MirrorSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specs = specs
}

// engine records the passes and watches asked of it.
type engine struct {
	mu      sync.Mutex
	profile string
	passes  []string
	fail    map[string]error
	watched chan string
	stopped chan string
}

func (e *engine) Pass(_ context.Context, spec port.MirrorSpec, _ port.MirrorRunOptions) (port.MirrorPass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.passes = append(e.passes, e.profile+":"+spec.Name)
	err := e.fail[spec.Name]
	return port.MirrorPass{Mirror: spec.Name, Err: err}, err
}

func (e *engine) Watch(ctx context.Context, spec port.MirrorSpec, _ port.MirrorRunOptions, on func(port.MirrorPass)) error {
	if err := e.fail[spec.Name]; err != nil {
		return err
	}
	on(port.MirrorPass{Mirror: spec.Name})
	e.watched <- spec.Name + "@" + spec.Folder
	<-ctx.Done()
	e.stopped <- spec.Name
	return nil
}

// engines opens an engine per profile, failing for the profile "bad",
// and for any the first failOpens times.
type engines struct {
	mu        sync.Mutex
	open      map[string]*engine
	released  int
	fail      map[string]error
	failOpens int
	watched   chan string
	stopped   chan string
}

func newEngines() *engines {
	return &engines{open: map[string]*engine{}, watched: make(chan string, 16), stopped: make(chan string, 16)}
}

func (e *engines) Open(profile string) (port.MirrorEngine, func() error, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if profile == "bad" {
		return nil, nil, errBoom
	}
	if e.failOpens > 0 {
		e.failOpens--
		return nil, nil, errBoom
	}
	eng := &engine{profile: profile, fail: e.fail, watched: e.watched, stopped: e.stopped}
	e.open[profile] = eng
	return eng, func() error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.released++
		return nil
	}, nil
}

// service is the mirrors use case over doubles.
type service struct {
	s       *mirror.Service
	config  *mirrorConfig
	engines *engines
	files   *porttest.MemFiles
	timer   *porttest.Timer
}

func newService(t *testing.T) service {
	t.Helper()
	files := porttest.NewMemFiles()
	for _, d := range []string{"/a/sub", "/b", "/c"} {
		if err := files.MkdirAll(d); err != nil {
			t.Fatal(err)
		}
	}
	cfg, eng, timer := &mirrorConfig{}, newEngines(), porttest.NewTimer(now)
	return service{
		s: mirror.NewService(cfg, eng, files, porttest.NewMirrorDisk(files), timer), config: cfg, engines: eng, files: files, timer: timer,
	}
}

func TestNewServicePanicsOnNil(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	cases := map[string]func(){
		"MirrorConfig": func() {
			mirror.NewService(nil, newEngines(), files, porttest.NewMirrorDisk(files), porttest.NewTimer(now))
		},
		"MirrorEngines": func() {
			mirror.NewService(&mirrorConfig{}, nil, files, porttest.NewMirrorDisk(files), porttest.NewTimer(now))
		},
		"LocalFiles": func() {
			mirror.NewService(&mirrorConfig{}, newEngines(), nil, porttest.NewMirrorDisk(files), porttest.NewTimer(now))
		},
		"MirrorFiles":            func() { mirror.NewService(&mirrorConfig{}, newEngines(), files, nil, porttest.NewTimer(now)) },
		"Timer":                  func() { mirror.NewService(&mirrorConfig{}, newEngines(), files, porttest.NewMirrorDisk(files), nil) },
		"Autostart":              func() { mirror.NewAutostart(nil, "rmctl", nil, porttest.NewTimer(now)) },
		"Timer of the autostart": func() { mirror.NewAutostart(&autostart{}, "rmctl", nil, nil) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), strings.Fields(name)[0]) {
					t.Fatalf("%v", r)
				}
			}()
			build()
		})
	}
}

// Add checks a definition, and refuses one that overlaps another mirror.
func TestAdd(t *testing.T) {
	t.Parallel()
	s := newService(t)
	ctx := t.Context()
	added, err := s.s.Add(ctx, port.MirrorSpec{Name: "a", Dir: "/a/", Folder: "/A/"})
	if err != nil || added.Dir != "/a" || added.Folder != "/A" {
		t.Fatalf("%+v %v", added, err)
	}
	for name, c := range map[string]struct {
		spec port.MirrorSpec
		want error
	}{
		"no name":         {port.MirrorSpec{Dir: "/b", Folder: "/B"}, port.ErrMirrorSpec},
		"bad name":        {port.MirrorSpec{Name: "B!", Dir: "/b", Folder: "/B"}, port.ErrMirrorSpec},
		"relative dir":    {port.MirrorSpec{Name: "b", Dir: "b", Folder: "/B"}, port.ErrMirrorSpec},
		"relative folder": {port.MirrorSpec{Name: "b", Dir: "/b", Folder: "B"}, port.ErrMirrorSpec},
		"missing dir":     {port.MirrorSpec{Name: "b", Dir: "/nowhere", Folder: "/B"}, domain.ErrNotFound},
		"same name":       {port.MirrorSpec{Name: "a", Dir: "/b", Folder: "/B"}, port.ErrMirrorExists},
		"inner dir":       {port.MirrorSpec{Name: "b", Dir: "/a/sub", Folder: "/B"}, port.ErrMirrorExists},
		"inner folder":    {port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/a/sub"}, port.ErrMirrorExists},
		"outer folder":    {port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/"}, port.ErrMirrorExists},
	} {
		if _, err := s.s.Add(ctx, c.spec); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.s.Add(ctx, port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/A", Profile: "work"}); err != nil {
		t.Fatalf("another profile's folder: %v", err)
	}
	s.config.fail = errBoom
	if _, err := s.s.Add(ctx, port.MirrorSpec{Name: "c", Dir: "/c", Folder: "/C"}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// List reads each directory's journal and counts the local changes.
func TestList(t *testing.T) {
	t.Parallel()
	s := newService(t)
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a")})
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.pdfFile("B.pdf", "%PDF b", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.pdfFile("A.pdf", "%PDF a changed", 1, time10)
	f.pdfFile("C.pdf", "%PDF c", 1, time10)
	listed := mirror.NewService(&mirrorConfig{specs: []port.MirrorSpec{
		f.spec, {Name: "gone", Dir: "/gone", Folder: "/G"},
	}}, newEngines(), f.files, f.disk, porttest.NewTimer(now))
	states, err := listed.List(t.Context())
	if err != nil || len(states) != 2 {
		t.Fatalf("%+v %v", states, err)
	}
	st := states[0]
	if st.Tracked != 2 || st.LocalChanges != 2 || st.LastPass.IsZero() || st.Err != nil {
		t.Fatalf("%+v", st)
	}
	if !errors.Is(states[1].Err, domain.ErrNotFound) {
		t.Fatalf("%+v", states[1])
	}
	f.files.Write(filepath.Join(dir, domain.MirrorJournalName), []byte("{"))
	if states, _ = listed.List(t.Context()); !errors.Is(states[0].Err, mirror.ErrJournal) {
		t.Fatalf("%+v", states[0])
	}
	f.files.Write(filepath.Join(dir, domain.MirrorJournalName), []byte(`{"v": 1}`))
	f.disk.Fail["Scan"] = errBoom
	if states, _ = listed.List(t.Context()); !errors.Is(states[0].Err, errBoom) {
		t.Fatalf("%+v", states[0])
	}
	s.config.fail = errBoom
	if _, err = s.s.List(t.Context()); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// Remove forgets a definition, and names one that is not there.
func TestRemove(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"})
	spec, err := s.s.Remove(t.Context(), "a")
	if err != nil || spec.Dir != "/a" {
		t.Fatalf("%+v %v", spec, err)
	}
	if _, err = s.s.Remove(t.Context(), "a"); !errors.Is(err, port.ErrNoMirror) {
		t.Fatal(err)
	}
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"})
	s.config.removed = true
	if _, err = s.s.Remove(t.Context(), "a"); !errors.Is(err, port.ErrNoMirror) {
		t.Fatal(err)
	}
	s.config.fail = errBoom
	if _, err = s.s.Remove(t.Context(), "a"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// Run makes a pass of each mirror selected, each on its profile's engine;
// one that fails stops no other.
func TestRun(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(
		port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"},
		port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/B", Profile: "work"},
		port.MirrorSpec{Name: "c", Dir: "/c", Folder: "/C", Profile: "bad"},
	)
	s.engines.fail = map[string]error{"a": errBoom}
	var reported []string
	err := s.s.Run(t.Context(), port.MirrorSelection{All: true}, port.MirrorRunOptions{}, func(p port.MirrorPass) { reported = append(reported, p.Mirror) })
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "a: boom") || !strings.Contains(err.Error(), "c: profile bad: boom") {
		t.Fatal(err)
	}
	if strings.Join(reported, " ") != "a b c" || s.engines.released != 2 {
		t.Fatalf("%v %d", reported, s.engines.released)
	}
	if got := s.engines.open[""].passes; len(got) != 1 || got[0] != ":a" {
		t.Fatal(got)
	}
	if err = s.s.Run(t.Context(), port.MirrorSelection{Names: []string{"b"}}, port.MirrorRunOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.s.Run(t.Context(), port.MirrorSelection{Names: []string{"z"}}, port.MirrorRunOptions{}, nil); !errors.Is(err, port.ErrNoMirror) {
		t.Fatal(err)
	}
	adHoc := port.MirrorSpec{Dir: "relative", Folder: "/X"}
	if err = s.s.Run(t.Context(), port.MirrorSelection{AdHoc: &adHoc}, port.MirrorRunOptions{}, nil); !errors.Is(err, port.ErrMirrorSpec) {
		t.Fatal(err)
	}
	adHoc.Dir = "/a"
	if err = s.s.Run(t.Context(), port.MirrorSelection{AdHoc: &adHoc}, port.MirrorRunOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.s.Run(ctx, port.MirrorSelection{All: true}, port.MirrorRunOptions{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.config.fail = errBoom
	if err = s.s.Run(t.Context(), port.MirrorSelection{All: true}, port.MirrorRunOptions{}, nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// A watch of every mirror reads config.toml again, and starts, restarts
// and stops the mirrors added, changed and removed.
func TestRunWatchesEveryMirror(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"}, port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/B"},
		port.MirrorSpec{Name: "bad", Dir: "/c", Folder: "/C", Profile: "bad"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failed := make(chan string, 16)
	done := make(chan error, 1)
	go func() {
		done <- s.s.Run(ctx, port.MirrorSelection{All: true}, port.MirrorRunOptions{Watch: true}, func(p port.MirrorPass) {
			if p.Err != nil {
				failed <- p.Mirror
			}
		})
	}()
	if got := recv(t, failed); got != "bad" {
		t.Fatal(got)
	}
	got := []string{recv(t, s.engines.watched), recv(t, s.engines.watched)}
	slices.Sort(got)
	if strings.Join(got, " ") != "a@/A b@/B" {
		t.Fatal(got)
	}
	// b changes its folder, a goes, c comes; bad, which waits to try its
	// engine again, goes too.
	s.config.set(port.MirrorSpec{Name: "b", Dir: "/b", Folder: "/B2"}, port.MirrorSpec{Name: "c", Dir: "/c", Folder: "/C"})
	if !s.timer.WaitSleepers(ctx, 2) {
		t.Fatal("no sleeper")
	}
	s.timer.Wake()
	stopped := []string{recv(t, s.engines.stopped), recv(t, s.engines.stopped)}
	slices.Sort(stopped)
	if strings.Join(stopped, " ") != "a b" {
		t.Fatal(stopped)
	}
	got = []string{recv(t, s.engines.watched), recv(t, s.engines.watched)}
	slices.Sort(got)
	if strings.Join(got, " ") != "b@/B2 c@/C" {
		t.Fatal(got)
	}
	// config.toml unreadable: the mirrors keep running, and it is said.
	s.config.fail = errBoom
	if !s.timer.WaitSleepers(ctx, 1) {
		t.Fatal("no sleeper")
	}
	s.timer.Wake()
	got1 := recv(t, failed)
	for got1 == "bad" {
		got1 = recv(t, failed)
	}
	if got1 != "config.toml" {
		t.Fatal(got1)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A watch of named mirrors runs them until the context ends.
func TestRunWatchesNamedMirrors(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"})
	s.engines.fail = map[string]error{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- s.s.Run(ctx, port.MirrorSelection{Names: []string{"a"}}, port.MirrorRunOptions{Watch: true}, nil)
	}()
	recv(t, s.engines.watched)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	recv(t, s.engines.stopped)
}

func recv(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("nothing came")
		return ""
	}
}

// autostart is the service manager in memory.
type autostart struct {
	spec    port.ServiceSpec
	log     []string
	reads   int
	fail    error
	openErr error
}

func (a *autostart) Install(_ context.Context, spec port.ServiceSpec) (port.ServiceInfo, error) {
	a.spec = spec
	return port.ServiceInfo{Installed: true}, a.fail
}

func (a *autostart) Uninstall(context.Context) (port.ServiceInfo, error) {
	return port.ServiceInfo{}, a.fail
}
func (a *autostart) Status(context.Context) (port.ServiceInfo, error) {
	return port.ServiceInfo{Running: true}, a.fail
}

func (a *autostart) OpenLog() (io.WriteCloser, error) { return nil, a.openErr }

func (a *autostart) ReadLog(lines int) ([]string, int64, error) {
	return a.log[max(len(a.log)-lines, 0):], int64(len(a.log)), a.fail
}

func (a *autostart) ReadLogFrom(offset int64) ([]string, int64, error) {
	a.reads++
	if a.reads > 1 {
		return nil, offset, a.fail
	}
	a.log = append(a.log, "new")
	return a.log[offset:], int64(len(a.log)), nil
}

// The service runs this rmctl as the service, with the environment kept.
func TestAutostart(t *testing.T) {
	t.Parallel()
	a := &autostart{log: []string{"one", "two", "three"}}
	timer := porttest.NewTimer(now)
	s := mirror.NewAutostart(a, "/bin/rmctl", map[string]string{"RMCTL_CONFIG_DIR": "/c"}, timer)
	info, err := s.Install(t.Context())
	if err != nil || !info.Installed || a.spec.Program != "/bin/rmctl" || strings.Join(a.spec.Args, " ") != "mirror run --service" ||
		a.spec.Env["RMCTL_CONFIG_DIR"] != "/c" {
		t.Fatalf("%+v %+v %v", info, a.spec, err)
	}
	if info, err = s.Status(t.Context()); err != nil || !info.Running {
		t.Fatal(info, err)
	}
	if _, err = s.Uninstall(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.openErr = errBoom
	if _, err = s.OpenLog(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	var lines []string
	if err = s.Log(t.Context(), 2, false, func(l string) { lines = append(lines, l) }); err != nil || strings.Join(lines, " ") != "two three" {
		t.Fatal(lines, err)
	}
	// Following: the lines added come, until the log fails.
	lines = nil
	a.fail = nil
	done := make(chan error, 1)
	go func() { done <- s.Log(t.Context(), 1, true, func(l string) { lines = append(lines, l) }) }()
	if !timer.WaitSleepers(t.Context(), 1) {
		t.Fatal("no sleeper")
	}
	a.fail = errBoom
	timer.Wake()
	if !timer.WaitSleepers(t.Context(), 1) {
		t.Fatal("no sleeper")
	}
	timer.Wake()
	if err = <-done; !errors.Is(err, errBoom) || strings.Join(lines, " ") != "three new" {
		t.Fatal(lines, err)
	}
	a.fail = errBoom
	if err = s.Log(t.Context(), 1, false, nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}
