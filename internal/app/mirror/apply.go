// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// action is the kind of action a step reports.
type action port.MirrorActionKind

// kindOf is the action a step reports.
func kindOf(s step) action {
	switch s.kind {
	case stepMoveLocal:
		return action(port.MirrorMoveLocal)
	case stepMoveCloud:
		return action(port.MirrorMoveCloud)
	case stepRemove, stepRemoveDrawing:
		return action(port.MirrorRemove)
	case stepTrash:
		return action(port.MirrorTrash)
	case stepUpload:
		return action(port.MirrorUpload)
	case stepUpdate:
		switch {
		case s.replace:
			return action(port.MirrorReplace)
		case s.conflict:
			return action(port.MirrorConflict)
		default:
			return action(port.MirrorUpdate)
		}
	case stepAdopt:
		return action(port.MirrorAdopt)
	case stepDetach:
		return action(port.MirrorDetach)
	case stepSkip:
		return action(port.MirrorSkip)
	default:
		return ""
	}
}

// verb names a deletion for a person.
func (a action) verb() string {
	if port.MirrorActionKind(a) == port.MirrorTrash {
		return "moving the document to the trash"
	}
	return "moving the file into " + domain.MirrorRemovedDir
}

// apply carries one step out, or, in a dry run, only reports it, and
// records in the journal what it did; false is a step that failed.
func (r *run) apply(ctx context.Context, s step) bool {
	a := port.MirrorAction{Kind: port.MirrorActionKind(kindOf(s)), Path: s.path, To: s.to, Document: s.id, Detail: s.detail}
	switch {
	case s.kind == stepUpdate && s.replace:
		a.Detail = "an EPUB changed locally, without annotations on the tablet: replaced whole"
	case s.kind == stepUpdate && s.conflict:
		a.Detail = "changed on both sides: the local PDF goes up, the annotations stay"
	}
	switch s.kind {
	case stepSkip, stepDetach:
		if s.kind == stepDetach && !r.opts.DryRun {
			e := r.j.Items[s.id]
			e.Detached = true
			r.j.Items[s.id] = e
		}
		r.record(a)
		return true
	case stepRefresh, stepForget, stepPull:
		// Kept in the journal only; a pull is reported by its drawing.
		if !r.opts.DryRun {
			r.bookkeep(s)
		}
		return true
	default:
	}
	if s.kind == stepTrash && !r.opts.DryRun && r.exists(s.path) {
		// An application that saves by deleting the file and writing it anew
		// leaves it missing for a moment: the listing saw that moment.
		r.record(port.MirrorAction{Kind: port.MirrorSkip, Path: s.path, Document: s.id, Detail: "back since the directory was listed: not deleted"})
		return false
	}
	if !r.opts.DryRun {
		a.Err = r.carry(ctx, s, &a)
		if a.Err == nil && s.changesCloud() {
			r.checkpoint(ctx)
		}
	}
	r.record(a)
	return a.Err == nil
}

// changesCloud reports whether the step changes the cloud: what a pass cut
// short must find in the journal, not do again.
func (s step) changesCloud() bool {
	switch s.kind {
	case stepUpload, stepUpdate, stepMoveCloud, stepTrash:
		return true
	default:
		return false
	}
}

// bookkeep records a step that changes nothing but the journal.
func (r *run) bookkeep(s step) {
	switch s.kind {
	case stepForget:
		delete(r.j.Items, s.id)
	case stepPull:
		r.j.Items[s.id] = entry{Name: s.doc.item.Name, Parent: s.doc.item.Parent, Record: s.doc.item.Hash, Content: domain.DrawnFingerprint(s.doc.item)}
	case stepRefresh:
		e := r.j.Items[s.id]
		e = inStep(e, s.path, s.file, s.doc.item)
		r.j.Items[s.id] = e
	default:
	}
}

// inStep is e once its file at rel and its document it are in step.
func inStep(e entry, rel string, f *localFile, it domain.Item) entry {
	e.Path = rel
	if f != nil {
		e.Size, e.ModTime = f.Size, f.ModTime
		if f.sha != "" {
			e.SHA256 = f.sha
		}
	}
	e.Name, e.Parent, e.Record, e.Content = it.Name, it.Parent, it.Hash, domain.DrawnFingerprint(it)
	return e
}

// carry carries a step out and records it in the journal.
func (r *run) carry(ctx context.Context, s step, a *port.MirrorAction) error {
	switch s.kind {
	case stepMoveLocal:
		return r.moveLocal(s, a)
	case stepMoveCloud:
		return r.moveCloud(ctx, s)
	case stepRemove, stepRemoveDrawing:
		return r.remove(s, a)
	case stepTrash:
		if _, err := r.e.writer.Trash(ctx, []string{string(s.id)}); err != nil {
			return err
		}
		r.asideDrawing(s.id)
		delete(r.j.Items, s.id)
		return nil
	case stepUpload:
		return r.upload(ctx, s, a)
	case stepUpdate:
		return r.update(ctx, s, a)
	case stepAdopt:
		r.j.Items[s.id] = inStep(entry{}, s.path, s.file, s.doc.item)
		return nil
	default:
		return nil
	}
}

// moveLocal renames the local file after its document, and its drawing
// with it.
func (r *run) moveLocal(s step, a *port.MirrorAction) error {
	if err := r.move(s.path, s.to); err != nil {
		return err
	}
	e := r.j.Items[s.id]
	if e.Output != "" {
		out := r.output(s.id, s.to, false)
		if err := r.move(e.Output, out); err == nil {
			e.Output = out
		}
	}
	r.j.Items[s.id] = inStep(e, s.to, s.file, s.doc.item)
	a.Detail = "renamed or moved on the tablet"
	return nil
}

// moveCloud renames or moves the document after its local file: into the
// folder of the file's directory, under the file's name.
func (r *run) moveCloud(ctx context.Context, s step) error {
	folder, err := r.cloudFolder(ctx, path.Dir(s.to))
	if err != nil {
		return err
	}
	it := s.doc.item
	if folder != it.Parent {
		if it, err = r.e.writer.Move(ctx, string(s.id), string(folder)); err != nil {
			return err
		}
	}
	if name := domain.MirrorStem(s.to); name != it.Name {
		edited, rerr := r.e.writer.Rename(ctx, string(s.id), name)
		if rerr != nil {
			return rerr
		}
		it = edited.Item
	}
	e := r.j.Items[s.id]
	if e.Output != "" {
		out := r.output(s.id, s.to, false)
		if merr := r.move(e.Output, out); merr == nil {
			e.Output = out
		}
	}
	r.j.Items[s.id] = inStep(e, s.to, s.file, it)
	r.refreshDoc(it)
	return nil
}

// remove moves the local file of a document gone from the folder into the
// removed directory, and its drawing with it.
func (r *run) remove(s step, a *port.MirrorAction) error {
	if s.kind == stepRemove {
		to, err := r.aside(s.path)
		if err != nil {
			return err
		}
		a.To = to
	}
	r.asideDrawing(s.id)
	delete(r.j.Items, s.id)
	if s.kind == stepRemoveDrawing {
		a.Detail = "the drawing of a document gone from the folder"
	} else {
		a.Detail = "deleted on the tablet or moved out of the folder"
	}
	return nil
}

// asideDrawing moves the drawing of id, if it has one, into the removed
// directory.
func (r *run) asideDrawing(id domain.ItemID) {
	if out := r.j.Items[id].Output; out != "" {
		_, _ = r.aside(out) //nolint:errcheck // the drawing is the mirror's own; one that is gone is no loss
	}
}

// aside moves the file at rel into the removed directory, at the same path
// there; a name taken there gets a number. It returns where it went.
func (r *run) aside(rel string) (string, error) {
	if _, _, exists, err := r.e.files.Stat(abs(r.spec.Dir, rel)); err != nil || !exists {
		return "", err
	}
	to := path.Join(domain.MirrorRemovedDir, rel)
	for n := 2; r.exists(to); n++ {
		if n > maxAside {
			return "", fmt.Errorf("%s: %w", to, errTaken)
		}
		ext := path.Ext(rel)
		to = path.Join(domain.MirrorRemovedDir, strings.TrimSuffix(rel, ext)+" ("+strconv.Itoa(n)+")"+ext)
	}
	return to, r.move(rel, to)
}

// move renames the file at rel to to, both relative to the directory,
// creating the directories to needs; it never replaces a file, not even
// one written there since the directory was listed.
func (r *run) move(rel, to string) error {
	err := r.e.disk.Move(abs(r.spec.Dir, rel), abs(r.spec.Dir, to))
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s: %w", to, errTaken)
	}
	return err
}

// maxAside bounds the numbers tried for a name taken in the removed
// directory.
const maxAside = 999

// errTaken means a file is already where another would go.
var errTaken = errors.New("a file is already there")

func (r *run) exists(rel string) bool {
	_, _, exists, err := r.e.files.Stat(abs(r.spec.Dir, rel))
	return err != nil || exists
}

// upload sends a new local file into the folder of its directory, created
// when missing, under its name.
func (r *run) upload(ctx context.Context, s step, a *port.MirrorAction) error {
	folder, err := r.cloudFolder(ctx, path.Dir(s.path))
	if err != nil {
		return err
	}
	it, err := r.e.writer.Put(ctx, abs(r.spec.Dir, s.path), string(folder), port.PutOptions{Name: domain.MirrorStem(s.path)}, nil)
	if err != nil {
		return err
	}
	a.Document = it.ID
	r.j.Items[it.ID] = inStep(entry{}, s.path, s.file, it)
	return nil
}

// update replaces the PDF of the document with the changed local file,
// keeping the annotations of its pages; a page count that changed is told,
// and pages whose annotations would go are refused.
func (r *run) update(ctx context.Context, s step, a *port.MirrorAction) error {
	if s.replace {
		return r.replace(ctx, s)
	}
	opts := port.PutOptions{Name: s.doc.item.Name, ContentOnly: true, OnContentPlan: func(cp port.ContentPlan) {
		if cp.OldOriginal != cp.NewOriginal {
			a.Detail = strings.TrimPrefix(a.Detail+"; ", "; ") +
				fmt.Sprintf("the PDF has %d pages, it had %d: %d kept, %d added, %d dropped", cp.NewOriginal, cp.OldOriginal, cp.Kept, cp.Appended, cp.Dropped)
		}
	}}
	it, err := r.e.writer.Put(ctx, abs(r.spec.Dir, s.path), string(s.doc.item.Parent), opts, nil)
	if errors.Is(err, domain.ErrAnnotationsDropped) {
		return fmt.Errorf("%w; the mirror never drops annotations: rmctl put --content-only --drop-annotations does, by hand", err)
	}
	if err != nil {
		return err
	}
	r.j.Items[s.id] = inStep(r.j.Items[s.id], s.path, s.file, it)
	r.refreshDoc(it)
	return nil
}

// replace replaces the document whole with the changed local file, as put
// --replace does: it is the one document of its name in its folder, and
// carries no annotation to lose.
func (r *run) replace(ctx context.Context, s step) error {
	it, err := r.e.writer.Put(ctx, abs(r.spec.Dir, s.path), string(s.doc.item.Parent), port.PutOptions{Name: s.doc.item.Name, Replace: true}, nil)
	if err != nil {
		return err
	}
	if it.ID != s.id {
		return fmt.Errorf("%s: %w", s.path, domain.ErrReplaceTargetChanged)
	}
	r.j.Items[s.id] = inStep(r.j.Items[s.id], s.path, s.file, it)
	r.refreshDoc(it)
	return nil
}

// refreshDoc keeps the snapshot's document up to date after a change of
// it, for the drawing that follows.
func (r *run) refreshDoc(it domain.Item) {
	if c, ok := r.snap.docs[it.ID]; ok {
		c.item = it
		r.snap.docs[it.ID] = c
	}
}

// drawAll draws, or exports in a build that draws nothing, every document
// followed whose pages changed since its last drawing: one created on the
// tablet always, one mirrored from a file once it carries annotations.
func (r *run) drawAll(ctx context.Context) {
	ids := slices.Sorted(maps.Keys(r.j.Items))
	for _, id := range ids {
		if out := r.j.Items[id].Output; out != "" {
			r.outputs[strings.ToLower(out)] = id
		}
	}
	if r.opts.DryRun {
		ids = r.dryDraws(ids)
	}
	order := map[domain.ItemID]int{}
	for i, id := range r.snap.sorted() {
		order[id] = i
	}
	slices.SortStableFunc(ids, func(a, b domain.ItemID) int { return cmp.Compare(order[a], order[b]) })
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e, ok := r.j.Items[id]
		c, inCloud := r.snap.docs[id]
		if !ok || !inCloud || e.Detached {
			continue
		}
		fp := domain.DrawnFingerprint(c.item)
		if fp == e.Drawn || (e.Path != "" && !domain.Annotated(c.item) && e.Output == "") {
			continue
		}
		r.draw(ctx, id, e, c, fp)
	}
}

// dryDraws are the documents a dry run would draw: those followed, those it
// would adopt or pull, whose journal entry it does not write.
func (r *run) dryDraws(ids []domain.ItemID) []domain.ItemID {
	for _, a := range r.pass.Actions {
		if a.Kind == port.MirrorAdopt && a.Document != "" {
			if _, ok := r.j.Items[a.Document]; !ok {
				r.j.Items[a.Document] = entry{Path: a.Path}
				ids = append(ids, a.Document)
			}
		}
	}
	if r.spec.PullNew {
		for _, id := range r.snap.sorted() {
			if _, ok := r.j.Items[id]; !ok {
				r.j.Items[id] = entry{}
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// draw writes the drawing of the document id into the annotated directory.
func (r *run) draw(ctx context.Context, id domain.ItemID, e entry, c cloudDoc, fp string) {
	src := e.Path
	if src == "" {
		src = path.Join(c.dir, domain.DerivedFileName(c.item.Name, "pdf"))
	}
	out := r.output(id, src, true)
	kind := port.MirrorDraw
	if !r.e.renders {
		kind = port.MirrorExport
	}
	a := port.MirrorAction{Kind: kind, Path: e.Path, To: out, Document: id}
	if e.Path == "" {
		a.Detail = "created on the tablet"
	}
	if r.opts.DryRun {
		r.record(a)
		return
	}
	target := abs(r.spec.Dir, out)
	if err := r.e.files.MkdirAll(filepath.Dir(target)); err != nil {
		a.Err = err
		r.record(a)
		return
	}
	if r.e.renders {
		_, a.Err = r.e.rendering.Render(ctx, string(id), target, port.RenderOptions{})
	} else {
		_, a.Err = r.e.fetcher.Export(ctx, []string{string(id)}, target)
	}
	if a.Err == nil {
		if !c.item.Modified.IsZero() {
			_ = r.e.files.SetModTime(target, c.item.Modified) //nolint:errcheck // the time is a courtesy to file browsers
		}
		if e.Output != "" && e.Output != out {
			_ = r.e.files.Remove(abs(r.spec.Dir, e.Output)) //nolint:errcheck // the old drawing is the mirror's own
		}
		e.Drawn, e.Output = fp, out
		r.j.Items[id] = e
	}
	r.record(a)
}

// output is where the drawing of id, mirrored from the file at rel, goes: a
// PDF, or an .rmdoc in a build that draws nothing, under the annotated
// directory, told apart by identifier from another document's.
func (r *run) output(id domain.ItemID, rel string, claim bool) string {
	ext := "pdf"
	if !r.e.renders {
		ext = "rmdoc"
	}
	out := domain.MirrorAnnotatedPath(rel, ext)
	if owner, taken := r.outputs[strings.ToLower(out)]; taken && owner != id {
		dir, base := path.Split(out)
		out = path.Join(dir, fetch.Disambiguated(base, id, ext))
	}
	if claim {
		r.outputs[strings.ToLower(out)] = id
	}
	return out
}
