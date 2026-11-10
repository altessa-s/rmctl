// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Engine implements port.MirrorEngine for one profile: it reads the
// library, uploads through the write use case, draws through the render
// use case — or, in a build that draws nothing, exports through the fetch
// use case — and follows the notification socket.
type Engine struct {
	lib       *library.Library
	writer    port.Writer
	rendering port.Rendering
	fetcher   port.Fetcher
	files     port.LocalFiles
	disk      port.MirrorFiles
	notifier  port.Notifier
	dirs      port.DirWatcher
	timer     port.Timer
	renders   bool
}

var _ port.MirrorEngine = (*Engine)(nil)

// Deps are what an engine is built from. Renders says this build draws
// documents; without it the annotated documents are exported as .rmdoc.
type Deps struct {
	Library   *library.Library
	Writer    port.Writer
	Rendering port.Rendering
	Fetcher   port.Fetcher
	Files     port.LocalFiles
	Disk      port.MirrorFiles
	Notifier  port.Notifier
	Dirs      port.DirWatcher
	Timer     port.Timer
	Renders   bool
}

// NewEngine returns the mirror engine. A nil dependency panics.
func NewEngine(d Deps) *Engine {
	need(
		dependency{"Library", d.Library == nil}, dependency{"Writer", d.Writer == nil}, dependency{"Rendering", d.Rendering == nil},
		dependency{"Fetcher", d.Fetcher == nil}, dependency{"LocalFiles", d.Files == nil}, dependency{"MirrorFiles", d.Disk == nil},
		dependency{"Notifier", d.Notifier == nil}, dependency{"DirWatcher", d.Dirs == nil}, dependency{depTimer, d.Timer == nil},
	)
	return &Engine{
		lib: d.Library, writer: d.Writer, rendering: d.Rendering, fetcher: d.Fetcher, files: d.Files,
		disk: d.Disk, notifier: d.Notifier, dirs: d.Dirs, timer: d.Timer, renders: d.Renders,
	}
}

// dependency is a dependency of a constructor, and whether it is missing.
type dependency struct {
	name    string
	missing bool
}

// depTimer names the timer every constructor needs.
const depTimer = "Timer"

// need panics naming the first dependency missing: a nil dependency is a
// programming error.
func need(deps ...dependency) {
	for _, d := range deps {
		if d.missing {
			panic("mirror: " + d.name + " is required")
		}
	}
}

// Pass implements port.MirrorEngine: it takes the directory's lock, makes
// the pass and lets the lock go.
func (e *Engine) Pass(ctx context.Context, spec port.MirrorSpec, opts port.MirrorRunOptions) (port.MirrorPass, error) {
	release, err := e.lock(spec)
	if err != nil {
		p := e.started(spec, opts)
		return e.failed(p, err), err
	}
	defer func() { _ = release() }()
	p := e.pass(ctx, spec, opts)
	return p, p.Err
}

// lock takes the lock of the mirrored directory, which must be there.
func (e *Engine) lock(spec port.MirrorSpec) (func() error, error) {
	if err := checkDir(e.files, spec.Dir); err != nil {
		return nil, err
	}
	release, err := e.disk.Lock(abs(spec.Dir, domain.MirrorLockName))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.Dir, err)
	}
	return release, nil
}

// checkDir refuses a mirrored directory that is not there, as an unmounted
// drive is not: nothing is mirrored into or out of it.
func checkDir(files port.LocalFiles, dir string) error {
	_, isDir, exists, err := files.Stat(dir)
	switch {
	case err != nil:
		return err
	case !exists:
		return fmt.Errorf("%s: %w: the directory is not there; is its drive mounted?", dir, domain.ErrNotFound)
	case !isDir:
		return fmt.Errorf("%s: %w", dir, domain.ErrNotAFolder)
	}
	return nil
}

func (e *Engine) started(spec port.MirrorSpec, opts port.MirrorRunOptions) port.MirrorPass {
	return port.MirrorPass{
		Mirror: spec.Name, Dir: spec.Dir, Folder: spec.Folder, DryRun: opts.DryRun,
		Started: e.timer.Now(), Lite: !e.renders,
	}
}

func (e *Engine) failed(p port.MirrorPass, err error) port.MirrorPass {
	p.Err, p.Ended = err, e.timer.Now()
	return p
}

// run is one pass under way.
type run struct {
	e    *Engine
	spec port.MirrorSpec
	opts port.MirrorRunOptions
	j    *journal
	snap *snapshot
	pass port.MirrorPass
	// outputs are the drawings claimed in this pass, by path, so two
	// documents never draw into one file.
	outputs map[string]domain.ItemID
}

// pass makes one pass, the lock taken: it reads the journal, the cloud
// folder and the files, plans, guards the deletions, carries the steps out
// and draws, and writes the journal.
func (e *Engine) pass(ctx context.Context, spec port.MirrorSpec, opts port.MirrorRunOptions) port.MirrorPass {
	r := &run{e: e, spec: spec, opts: opts, pass: e.started(spec, opts), outputs: map[string]domain.ItemID{}}
	j, err := readJournal(e.files, spec.Dir)
	if err != nil {
		return e.failed(r.pass, err)
	}
	r.j = j
	if err = r.load(ctx); err != nil {
		return r.finish(ctx, err)
	}
	files, err := scan(e.disk, spec.Dir)
	if err != nil {
		return r.finish(ctx, err)
	}
	pl := &planner{
		j: j, snap: r.snap, local: files, now: r.pass.Started, spec: spec,
		hash: func(f *localFile) (string, error) { return hash(e.files, spec.Dir, f) },
	}
	steps := r.guard(pl.plan(), files)
	// A document one step of which failed takes no further step: a refresh
	// of the journal after a move that failed would record the move.
	failed := map[domain.ItemID]bool{}
	for _, s := range steps {
		if ctx.Err() != nil {
			return r.finish(ctx, ctx.Err())
		}
		if s.id != "" && failed[s.id] {
			continue
		}
		if !r.apply(ctx, s) && s.id != "" {
			failed[s.id] = true
		}
	}
	r.drawAll(ctx)
	return r.finish(ctx, nil)
}

// load reads the library and finds the mirrored folder: the one the
// journal names, still at the spec's path. A directory never mirrored
// takes the folder there, created when missing.
func (r *run) load(ctx context.Context) error {
	tree, _, err := r.e.lib.Load(ctx)
	if err != nil {
		return err
	}
	top, made, err := r.folder(ctx, tree)
	if err != nil {
		return err
	}
	if made {
		if tree, _, err = r.e.lib.Load(ctx); err != nil {
			return err
		}
	}
	if top == "" && r.spec.Folder != "/" && r.spec.Folder != "" {
		// A dry run of a folder still to create: nothing in it yet.
		r.snap = &snapshot{tree: tree, docs: map[domain.ItemID]cloudDoc{}, folders: map[string]domain.ItemID{}, paths: map[string]string{}}
		return nil
	}
	if r.j.Folder != "" && r.j.Folder != top {
		return fmt.Errorf("%w: %s mirrored another folder (%s) than the one at %s now; move %s aside to start over",
			port.ErrMirrorRefused, r.spec.Dir, r.j.Folder, r.spec.Folder, domain.MirrorJournalName)
	}
	r.j.Folder = top
	r.j.Profile = r.spec.Profile
	r.snap = snapshotOf(tree, top)
	return nil
}

// folder is the folder the spec names. One the journal knows that is gone
// from that path is refused, naming where it went; a folder missing for a
// directory never mirrored is created (made), or reported as "" in a dry
// run.
func (r *run) folder(ctx context.Context, tree *domain.Tree) (top domain.ItemID, made bool, err error) {
	if r.spec.Folder == "" || r.spec.Folder == "/" {
		return domain.RootID, false, nil
	}
	it, found, err := tree.Resolve(r.spec.Folder, false)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}
	switch {
	case found && !it.Kind.IsFolder():
		return "", false, fmt.Errorf("%s: %w", r.spec.Folder, domain.ErrNotAFolder)
	case found:
		return it.ID, false, nil
	}
	if r.j.Folder != "" {
		if known, ok := tree.Get(r.j.Folder); ok && !tree.IsTrashed(known.ID) {
			return "", false, fmt.Errorf("%w: the folder %s is now at %s; define the mirror again with that path",
				port.ErrMirrorRefused, r.spec.Folder, tree.Path(known.ID))
		}
		return "", false, fmt.Errorf("%w: the folder %s is gone from the library or in the trash; nothing is deleted locally",
			port.ErrMirrorRefused, r.spec.Folder)
	}
	if r.opts.DryRun {
		return "", false, nil
	}
	folder, err := r.e.writer.Mkdir(ctx, r.spec.Folder, true)
	if err != nil {
		return "", false, err
	}
	return folder.ID, true, nil
}

// guard refuses the deletions of a side the mass-deletion guard stops,
// unless ForceDelete, and turns every deletion into a detachment when the
// spec carries no deletions.
func (r *run) guard(steps []step, files local) []step {
	var up, down int
	for _, s := range steps {
		if deletes, isDown := s.deletion(); deletes && isDown {
			down++
		} else if deletes {
			up++
		}
	}
	tracked, trackedLocal := r.j.tracked()
	refuseUp, refuseDown := "", ""
	if !r.opts.ForceDelete && !r.spec.NoDelete {
		refuseUp = guard(up, trackedLocal, files.all == 0 && trackedLocal > 0, "local directory")
		refuseDown = guard(down, tracked, len(r.snap.docs) == 0 && tracked > 0, "cloud folder")
	}
	out := steps[:0:0]
	for _, s := range steps {
		deletes, isDown := s.deletion()
		switch {
		case !deletes:
			out = append(out, s)
		case r.spec.NoDelete:
			out = append(out, step{kind: stepDetach, id: s.id, path: s.path, detail: "deleted on the other side; --no-delete keeps it"})
		case isDown && refuseDown != "":
			r.refuse(s, refuseDown)
		case !isDown && refuseUp != "":
			r.refuse(s, refuseUp)
		default:
			out = append(out, s)
		}
	}
	return out
}

// refuse reports a deletion the guard stopped: it stays pending.
func (r *run) refuse(s step, why string) {
	r.pass.Pending++
	r.pass.Actions = append(r.pass.Actions, port.MirrorAction{
		Kind: port.MirrorRefuse, Path: s.path, Document: s.id,
		Detail: kindOf(s).verb() + " refused: " + why + "; --force-delete carries it out",
	})
}

// finish records the pass in the journal, unless it was dry, and ends it.
func (r *run) finish(ctx context.Context, err error) port.MirrorPass {
	r.pass.Ended = r.e.timer.Now()
	if err == nil {
		err = stepsFailed(r.pass.Actions)
	}
	r.pass.Err = err
	if r.opts.DryRun || r.j == nil {
		return r.pass
	}
	r.j.LastPass, r.j.Pending, r.j.LastError = r.pass.Ended, r.pass.Pending, ""
	if err != nil {
		r.j.LastError = err.Error()
	}
	if werr := r.j.write(ctx, r.e.files, r.spec.Dir); werr != nil {
		r.pass.Err = errors.Join(r.pass.Err, werr)
	}
	return r.pass
}

// errSteps means steps of a pass failed; the others were carried out. A
// watch waits for the next change, or the next rescan, to try them again.
var errSteps = errors.New("steps of the pass failed")

// stepsFailed is errSteps naming the first action that failed and how many
// did, or nil.
func stepsFailed(actions []port.MirrorAction) error {
	var first *port.MirrorAction
	failed := 0
	for i, a := range actions {
		if a.Err == nil {
			continue
		}
		if first == nil {
			first = &actions[i]
		}
		failed++
	}
	if first == nil {
		return nil
	}
	return fmt.Errorf("%w: %d of %d; %s %s: %w", errSteps, failed, len(actions), first.Kind, first.Path, first.Err)
}

// record appends an action; a failed one stays pending.
func (r *run) record(a port.MirrorAction) {
	if a.Err != nil || a.Kind == port.MirrorSkip {
		r.pass.Pending++
	}
	r.pass.Actions = append(r.pass.Actions, a)
}

// cloudFolder is the folder the local directory dir stands for, created
// with its parents when missing.
func (r *run) cloudFolder(ctx context.Context, dir string) (domain.ItemID, error) {
	if dir == "." {
		dir = ""
	}
	if id, ok := r.snap.folders[dir]; ok {
		return id, nil
	}
	parent := path.Dir(dir)
	if _, err := r.cloudFolder(ctx, parent); err != nil {
		return "", err
	}
	if parent == "." {
		parent = ""
	}
	made, err := r.e.writer.Mkdir(ctx, r.snap.paths[parent]+"/"+path.Base(dir), true)
	if err != nil {
		return "", err
	}
	r.snap.folders[dir] = made.ID
	r.snap.paths[dir] = strings.TrimSuffix(r.snap.paths[parent], "/") + "/" + made.Name
	return made.ID, nil
}
