// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ConfigPoll is how often a watch of every mirror reads config.toml again
// for mirrors added, changed and removed.
const ConfigPoll = 30 * time.Second

// Service implements port.Mirrors: the definitions in config.toml, the
// state in each directory's journal, and the runs, each mirror on the
// engine of its profile.
type Service struct {
	config  port.MirrorConfig
	engines port.MirrorEngines
	files   port.LocalFiles
	disk    port.MirrorFiles
	timer   port.Timer
}

var _ port.Mirrors = (*Service)(nil)

// NewService returns the mirrors use case. A nil dependency panics.
func NewService(config port.MirrorConfig, engines port.MirrorEngines, files port.LocalFiles, disk port.MirrorFiles, timer port.Timer) *Service {
	need(
		dependency{"MirrorConfig", config == nil}, dependency{"MirrorEngines", engines == nil}, dependency{"LocalFiles", files == nil},
		dependency{"MirrorFiles", disk == nil}, dependency{depTimer, timer == nil},
	)
	return &Service{config: config, engines: engines, files: files, disk: disk, timer: timer}
}

// Check is spec cleaned, or ErrMirrorSpec: a name a mirror may take (when
// named), an absolute directory, a folder path from the top.
func Check(spec port.MirrorSpec) (port.MirrorSpec, error) {
	if spec.Name != "" && !domain.ValidMirrorName(spec.Name) {
		return spec, fmt.Errorf("%w: %q: a name is 1-64 characters of a-z, 0-9, _ and -, starting with a letter or digit", port.ErrMirrorSpec, spec.Name)
	}
	if !filepath.IsAbs(spec.Dir) {
		return spec, fmt.Errorf("%w: %q: the directory is an absolute path", port.ErrMirrorSpec, spec.Dir)
	}
	spec.Dir = filepath.Clean(spec.Dir)
	if !strings.HasPrefix(spec.Folder, "/") {
		return spec, fmt.Errorf("%w: %q: the cloud folder is a path from the top, such as /Papers", port.ErrMirrorSpec, spec.Folder)
	}
	spec.Folder = path.Clean(spec.Folder)
	return spec, nil
}

// Add implements port.Mirrors. The directory must be there; a mirror of
// the same name, or whose directory contains this one or lies inside it,
// or of the same profile whose folder does, is refused: two mirrors over
// one file would each take the other's changes for their own.
func (s *Service) Add(_ context.Context, spec port.MirrorSpec) (port.MirrorSpec, error) {
	if spec.Name == "" {
		return spec, fmt.Errorf("%w: a mirror in config.toml has a name", port.ErrMirrorSpec)
	}
	spec, err := Check(spec)
	if err != nil {
		return spec, err
	}
	if err = checkDir(s.files, spec.Dir); err != nil {
		return spec, err
	}
	existing, err := s.config.Mirrors()
	if err != nil {
		return spec, err
	}
	for _, m := range existing {
		if m.Name == spec.Name {
			return spec, fmt.Errorf("%w: %s", port.ErrMirrorExists, m.Name)
		}
		if err = overlap(m, spec); err != nil {
			return spec, err
		}
	}
	return spec, s.config.AddMirror(spec)
}

// overlap is why spec cannot be mirrored beside m, or nil: a directory
// that contains the other one or lies inside it, or a folder of the same
// profile that does.
func overlap(m, spec port.MirrorSpec) error {
	switch {
	case nested(m.Dir, spec.Dir, filepath.Separator):
		return fmt.Errorf("%w: %s mirrors %s, which overlaps %s", port.ErrMirrorExists, m.Name, m.Dir, spec.Dir)
	case m.Profile == spec.Profile && nested(m.Folder, spec.Folder, '/'):
		return fmt.Errorf("%w: %s mirrors the folder %s, which overlaps %s", port.ErrMirrorExists, m.Name, m.Folder, spec.Folder)
	default:
		return nil
	}
}

// nested reports whether a and b are the same path or one lies inside the
// other, compared without case: on the file systems of macOS and Windows
// they are one directory.
func nested(a, b string, sep byte) bool {
	a, b = strings.ToLower(strings.TrimSuffix(a, string(sep))), strings.ToLower(strings.TrimSuffix(b, string(sep)))
	return a == b || a == "" || b == "" || strings.HasPrefix(a, b+string(sep)) || strings.HasPrefix(b, a+string(sep))
}

// List implements port.Mirrors.
func (s *Service) List(context.Context) ([]port.MirrorState, error) {
	specs, err := s.config.Mirrors()
	if err != nil {
		return nil, err
	}
	out := make([]port.MirrorState, 0, len(specs))
	for _, spec := range specs {
		out = append(out, s.state(spec))
	}
	return out, nil
}

// state is what the journal of spec's directory says, and the local
// changes since: files new, deleted, or of another size or time.
func (s *Service) state(spec port.MirrorSpec) port.MirrorState {
	st := port.MirrorState{Spec: spec}
	if st.Err = checkDir(s.files, spec.Dir); st.Err != nil {
		return st
	}
	j, err := readJournal(s.files, spec.Dir)
	if err != nil {
		st.Err = err
		return st
	}
	st.Tracked, _ = j.tracked()
	st.LastPass, st.LastError, st.Pending = j.LastPass, j.LastError, j.Pending
	files, err := scan(s.disk, spec.Dir)
	if err != nil {
		st.Err = err
		return st
	}
	known := map[string]bool{}
	for _, e := range j.Items {
		if e.Path == "" || e.Detached {
			continue
		}
		f, ok := files.find(e.Path)
		if ok {
			known[f.Path] = true
		}
		if !ok || !f.sameStat(e) {
			st.LocalChanges++
		}
	}
	for p := range files.files {
		if !known[p] {
			st.LocalChanges++
		}
	}
	return st
}

// Remove implements port.Mirrors.
func (s *Service) Remove(_ context.Context, name string) (port.MirrorSpec, error) {
	specs, err := s.config.Mirrors()
	if err != nil {
		return port.MirrorSpec{}, err
	}
	for _, m := range specs {
		if m.Name != name {
			continue
		}
		removed, rerr := s.config.RemoveMirror(name)
		if rerr == nil && !removed {
			rerr = fmt.Errorf("%s: %w", name, port.ErrNoMirror)
		}
		return m, rerr
	}
	return port.MirrorSpec{}, fmt.Errorf("%s: %w", name, port.ErrNoMirror)
}

// selected are the mirrors sel names.
func (s *Service) selected(sel port.MirrorSelection) ([]port.MirrorSpec, error) {
	if sel.AdHoc != nil {
		spec, err := Check(*sel.AdHoc)
		return []port.MirrorSpec{spec}, err
	}
	specs, err := s.config.Mirrors()
	if err != nil {
		return nil, err
	}
	if sel.All {
		return specs, nil
	}
	out := make([]port.MirrorSpec, 0, len(sel.Names))
	for _, name := range sel.Names {
		found := false
		for _, m := range specs {
			if m.Name == name {
				out, found = append(out, m), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%s: %w", name, port.ErrNoMirror)
		}
	}
	return out, nil
}

// refusal is a mirror a run leaves out, and why.
type refusal struct {
	spec port.MirrorSpec
	err  error
}

// disjoint are the specs that overlap none before them, and the others
// refused: config.toml edited by hand can define two mirrors over one
// directory or one folder, which Add refuses.
func disjoint(specs []port.MirrorSpec) (ok []port.MirrorSpec, refused []refusal) {
next:
	for _, spec := range specs {
		for _, m := range ok {
			if err := overlap(m, spec); err != nil {
				refused = append(refused, refusal{spec: spec, err: fmt.Errorf("%w; it is not run", err)})
				continue next
			}
		}
		ok = append(ok, spec)
	}
	return ok, refused
}

// Run implements port.Mirrors. on hears every pass, one at a time. Of two
// mirrors that overlap, the one defined later is not run.
func (s *Service) Run(ctx context.Context, sel port.MirrorSelection, opts port.MirrorRunOptions, on func(port.MirrorPass)) error {
	specs, err := s.selected(sel)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	report := func(p port.MirrorPass) {
		if on == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		on(p)
	}
	engines := &openEngines{engines: s.engines, open: map[string]openEngine{}}
	defer engines.release()
	if !opts.Watch {
		var errs []error
		run, refused := disjoint(specs)
		for _, r := range refused {
			report(failedPass(r.spec, opts, s.timer.Now(), r.err))
			errs = append(errs, fmt.Errorf("%s: %w", label(r.spec), r.err))
		}
		for _, spec := range run {
			if err = ctx.Err(); err != nil {
				return errors.Join(append(errs, err)...)
			}
			p, perr := s.pass(ctx, engines, spec, opts)
			report(p)
			if perr != nil {
				errs = append(errs, fmt.Errorf("%s: %w", label(spec), perr))
			}
		}
		return errors.Join(errs...)
	}
	s.supervise(ctx, sel, specs, opts, engines, report)
	return nil
}

// pass makes one pass of spec on its profile's engine.
func (s *Service) pass(ctx context.Context, engines *openEngines, spec port.MirrorSpec, opts port.MirrorRunOptions) (port.MirrorPass, error) {
	eng, err := engines.get(spec.Profile)
	if err != nil {
		return failedPass(spec, opts, s.timer.Now(), err), err
	}
	return eng.Pass(ctx, spec, opts)
}

// failedPass is a pass that could not start.
func failedPass(spec port.MirrorSpec, opts port.MirrorRunOptions, now time.Time, err error) port.MirrorPass {
	return port.MirrorPass{Mirror: spec.Name, Dir: spec.Dir, Folder: spec.Folder, DryRun: opts.DryRun, Started: now, Ended: now, Err: err}
}

// label names a mirror in an error: its name, or its directory.
func label(spec port.MirrorSpec) string {
	if spec.Name != "" {
		return spec.Name
	}
	return spec.Dir
}

// watching is one mirror kept in step by a watch.
type watching struct {
	spec   port.MirrorSpec
	cancel context.CancelFunc
	done   chan struct{}
}

// supervise watches every mirror of specs, each on its own, until ctx
// ends; with sel.All it reads config.toml again every ConfigPoll and
// starts, restarts or stops the mirrors added, changed and removed.
func (s *Service) supervise(
	ctx context.Context, sel port.MirrorSelection, specs []port.MirrorSpec, opts port.MirrorRunOptions,
	engines *openEngines, report func(port.MirrorPass),
) {
	active := map[string]*watching{}
	stop := func(w *watching) {
		w.cancel()
		<-w.done
	}
	defer func() {
		for _, w := range active {
			stop(w)
		}
	}()
	said := map[string]bool{}
	for {
		run, refused := disjoint(specs)
		for _, r := range refused {
			// Said once, not at every reading of config.toml.
			if key := label(r.spec) + "\x00" + r.err.Error(); !said[key] {
				said[key] = true
				report(failedPass(r.spec, opts, s.timer.Now(), r.err))
			}
		}
		wanted := map[string]port.MirrorSpec{}
		for _, spec := range run {
			wanted[label(spec)] = spec
		}
		for key, w := range active {
			if spec, ok := wanted[key]; !ok || spec != w.spec {
				stop(w)
				delete(active, key)
			}
		}
		for key, spec := range wanted {
			if _, ok := active[key]; !ok {
				active[key] = s.start(ctx, spec, opts, engines, report)
			}
		}
		if !sel.All {
			<-ctx.Done()
			return
		}
		if s.timer.Sleep(ctx, ConfigPoll) != nil {
			return
		}
		if again, err := s.config.Mirrors(); err == nil {
			specs = again
		} else {
			report(failedPass(port.MirrorSpec{Name: "config.toml"}, opts, s.timer.Now(), err))
		}
	}
}

// start watches one mirror in a goroutine of its own.
func (s *Service) start(
	ctx context.Context, spec port.MirrorSpec, opts port.MirrorRunOptions, engines *openEngines, report func(port.MirrorPass),
) *watching {
	ctx, cancel := context.WithCancel(ctx)
	w := &watching{spec: spec, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		// An engine that cannot open yet (the keychain locked at login, a
		// profile not signed in) is tried again with the growing wait.
		for backoff := time.Duration(0); ; {
			eng, err := engines.get(spec.Profile)
			if err == nil {
				err = eng.Watch(ctx, spec, opts, report)
			}
			if err == nil || ctx.Err() != nil {
				return
			}
			report(failedPass(spec, opts, s.timer.Now(), err))
			backoff = min(max(backoff*backoffGrowth, firstBackoff), maxBackoff)
			if s.timer.Sleep(ctx, backoff) != nil {
				return
			}
		}
	}()
	return w
}

// openEngine is an engine opened for a profile, and how to let it go.
type openEngine struct {
	engine  port.MirrorEngine
	release func() error
}

// openEngines opens each profile's engine once.
type openEngines struct {
	mu      sync.Mutex
	engines port.MirrorEngines
	open    map[string]openEngine
}

func (o *openEngines) get(profile string) (port.MirrorEngine, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if e, ok := o.open[profile]; ok {
		return e.engine, nil
	}
	engine, release, err := o.engines.Open(profile)
	if err != nil {
		if profile == "" {
			return nil, err
		}
		return nil, fmt.Errorf("profile %s: %w", profile, err)
	}
	o.open[profile] = openEngine{engine: engine, release: release}
	return engine, nil
}

func (o *openEngines) release() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, e := range o.open {
		if e.release != nil {
			_ = e.release()
		}
	}
	o.open = map[string]openEngine{}
}
