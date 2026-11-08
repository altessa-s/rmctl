// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/outdir"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// RecordName is the file a folder's notes keep in their directory: what
// each note was written from, and with which options.
const RecordName = ".rmctl-notes.json"

// DefaultJobs is how many documents of a folder are read at a time.
const DefaultJobs = 4

// noteExt is the extension of a note.
const noteExt = "md"

// template names the layout of domain.Note: a new layout writes every
// note again, as other options do.
const template = "note-1"

// The failures of a folder's notes.
var (
	// ErrNotFolder means NotesTree was asked of a document.
	ErrNotFolder = errors.New("not a folder: a document's note is printed on its own")
	// ErrDocumentsFailed means some documents of a folder could not be
	// read; the others were.
	ErrDocumentsFailed = errors.New("documents could not be read")
)

// Service implements port.Notes.
type Service struct {
	lib    *library.Library
	reader *pages.Reader
	files  port.LocalFiles
}

var _ port.Notes = (*Service)(nil)

// New returns the notes use case. A nil dependency panics.
func New(lib *library.Library, reader *pages.Reader, files port.LocalFiles) *Service {
	switch {
	case lib == nil:
		panic("notes: Library is required")
	case reader == nil:
		panic("notes: Reader is required")
	case files == nil:
		panic("notes: LocalFiles is required")
	}
	return &Service{lib: lib, reader: reader, files: files}
}

// Folder implements port.Notes.
func (s *Service) Folder(ctx context.Context, query string) (bool, error) {
	return outdir.Folder(ctx, s.lib, query)
}

// fingerprint names what a note is written with, which an incremental
// run must find the same to keep the note: the template, the link pattern
// and whether every document gets a note.
func fingerprint(opts port.NoteOptions, allDocs bool) string {
	f := template
	if opts.PDFLink != "" {
		f += "/link=" + opts.PDFLink
	}
	if allDocs {
		f += "/all-docs"
	}
	return f
}

// job is one planned document and what its note is written with.
type job struct {
	outdir.Planned
	// rel is the note's path below the directory, with slashes, without
	// its extension.
	rel  string
	link domain.PageLink
	// always writes a note even when the document has no highlights.
	always bool
	// keep is set for an incremental run when the record shows the
	// document as it is now, written with these options; empty when it
	// had nothing to write.
	keep, empty bool
}

// NotesTree implements port.Notes. Each document is read as the page
// commands read it, tree.Jobs at a time; a document that fails is reported
// and the others go on. Only the directories that receive a note are
// created. The directory keeps a record, [RecordName], of what each note
// was written from and with which options: an incremental run keeps a
// note only when the record shows the same document, modified at the same
// time, written with the same options, and the note still has that time;
// it skips unread a document the record shows had nothing to write. A
// record missing or unreadable keeps nothing. A document with no
// highlights gets no note, unless tree.AllDocs, or unless the record
// shows it had one: a note then follows its document. The notes the
// record lists whose document is no longer below the folder are left in
// place, or moved into [domain.NotesRemovedDir] with tree.Prune, once
// every document was gone through.
func (s *Service) NotesTree(
	ctx context.Context, query, dir string, opts port.NoteOptions, tree port.NotesTreeOptions,
) (port.NotesTreeReport, error) {
	link, err := domain.ParsePageLink(opts.PDFLink)
	if err != nil {
		return port.NotesTreeReport{}, err
	}
	lib, _, err := s.lib.Load(ctx)
	if err != nil {
		return port.NotesTreeReport{}, err
	}
	top, name, err := outdir.Top(lib, query, ErrNotFolder)
	if err != nil {
		return port.NotesTreeReport{}, err
	}
	if dir == "" {
		dir = name
	}
	if err = s.files.MkdirAll(dir); err != nil {
		return port.NotesTreeReport{}, err
	}
	plan := outdir.Plan(lib, top, dir, tree.All, noteExt)
	options := fingerprint(opts, tree.AllDocs)
	old := outdir.Read(s.files, dir, RecordName)
	recorded, dropped := outdir.Pruned(old, dir, plan, options)
	if dropped {
		if err = s.writeRecord(ctx, dir, recorded); err != nil {
			return port.NotesTreeReport{}, err
		}
	}
	jobs := tree.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	// An interrupted run reports the documents it finished.
	finished, err := outdir.Each(ctx, len(plan), jobs, func(ctx context.Context, i int) (port.NotedDocument, error) {
		key := outdir.Key(dir, plan[i].Path)
		prev, had := old[key]
		e, known := recorded[key]
		d := s.noteOne(ctx, job{
			Planned: plan[i], rel: strings.TrimSuffix(key, "."+noteExt), link: link,
			always: tree.AllDocs || (had && !prev.Empty && prev.ID == plan[i].Document.ID),
			keep:   tree.Incremental && known, empty: e.Empty,
		})
		if d.Err != nil && ctx.Err() != nil {
			return d, ctx.Err()
		}
		return d, nil
	})
	report := port.NotesTreeReport{Dir: dir, Documents: finished}
	changed := remember(recorded, dir, finished, options)
	var perr error
	if err == nil {
		var gone bool
		report.Moved, report.Left, gone, perr = s.orphans(dir, plan, recorded, tree.Prune)
		changed = changed || gone
	}
	if changed {
		if werr := s.writeRecord(ctx, dir, recorded); werr != nil {
			return report, errors.Join(err, perr, werr)
		}
	}
	if err = errors.Join(err, perr); err != nil {
		return report, err
	}
	if failed := report.Count(port.NoteFailed); failed > 0 {
		return report, fmt.Errorf("%d of %d %w", failed, len(report.Documents), ErrDocumentsFailed)
	}
	return report, nil
}

// noteOne writes the note of one planned document, or finds it unchanged
// or with nothing to write.
func (s *Service) noteOne(ctx context.Context, j job) port.NotedDocument {
	doc := j.Document
	d := port.NotedDocument{Document: doc, LibraryPath: j.LibraryPath, Path: j.Path}
	fail := func(err error) port.NotedDocument {
		d.Status, d.Err = port.NoteFailed, err
		return d
	}
	if j.keep && !doc.Modified.IsZero() {
		if j.empty {
			d.Status = port.NoteSkipped
			return d
		}
		mtime, exists, err := s.files.ModTime(j.Path)
		if err != nil {
			return fail(err)
		}
		if exists && outdir.SameTime(mtime, doc.Modified) {
			d.Status = port.NoteUnchanged
			return d
		}
	}
	// A document without a page file has no highlight and no typed text.
	if !j.always && !pages.HasPages(doc) {
		d.Status = port.NoteSkipped
		return d
	}
	read, err := s.reader.Read(ctx, doc, nil)
	if err != nil {
		return fail(err)
	}
	d.Highlights = domain.Highlights(read)
	if d.Highlights == 0 && !j.always {
		d.Status = port.NoteSkipped
		return d
	}
	note := domain.Note{Item: doc, Path: j.LibraryPath, Pages: read, Link: j.link, Rel: j.rel}
	if err = s.files.MkdirAll(filepath.Dir(j.Path)); err != nil {
		return fail(err)
	}
	if err = s.files.Produce(ctx, j.Path, func(w io.Writer) error {
		_, werr := io.WriteString(w, note.Markdown())
		return werr
	}); err != nil {
		return fail(err)
	}
	if !doc.Modified.IsZero() {
		if err = s.files.SetModTime(j.Path, doc.Modified); err != nil {
			return fail(fmt.Errorf("set the modification time of %s: %w", j.Path, err))
		}
	}
	d.Status = port.NoteWritten
	return d
}

// remember adds the documents gone through to recorded — a note written,
// or a document with nothing to write — and reports whether that changed
// it. A document with no modification time is not recorded: there is
// nothing to compare it with.
func remember(recorded map[string]outdir.Entry, dir string, docs []port.NotedDocument, opts string) bool {
	changed := false
	for _, d := range docs {
		if d.Document.Modified.IsZero() || (d.Status != port.NoteWritten && d.Status != port.NoteSkipped) {
			continue
		}
		e := outdir.EntryOf(d.Document, opts)
		e.Empty = d.Status == port.NoteSkipped
		key := outdir.Key(dir, d.Path)
		if prev, ok := recorded[key]; ok && prev.Matches(d.Document, opts) && prev.Empty == e.Empty {
			continue
		}
		recorded[key] = e
		changed = true
	}
	return changed
}

// orphans deals with the notes recorded whose document is not in plan:
// with prune each is moved into the removed directory, at its path there,
// and leaves the record; otherwise it stays where it is, and in the
// record. A document that had nothing to write leaves the record either
// way, and so does a note no longer there, or whose recorded path is not
// a note inside dir. It returns the paths moved to, those left, and
// whether the record changed.
func (s *Service) orphans(dir string, plan []outdir.Planned, recorded map[string]outdir.Entry, prune bool) (moved, left []string, changed bool, err error) {
	planned := make(map[string]bool, len(plan))
	for _, p := range plan {
		planned[outdir.Key(dir, p.Path)] = true
	}
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(recorded)) {
		if planned[key] {
			continue
		}
		local := filepath.FromSlash(key)
		if e := recorded[key]; e.Empty || !filepath.IsLocal(local) || filepath.Ext(local) != "."+noteExt {
			delete(recorded, key)
			changed = true
			continue
		}
		path := filepath.Join(dir, local)
		if !prune {
			left = append(left, path)
			continue
		}
		_, isDir, exists, serr := s.files.Stat(path)
		switch {
		case serr != nil:
			errs = append(errs, serr)
			continue
		case exists && !isDir:
			to := filepath.Join(dir, domain.NotesRemovedDir, local)
			if merr := s.files.MkdirAll(filepath.Dir(to)); merr != nil {
				errs = append(errs, merr)
				continue
			}
			if rerr := s.files.Rename(path, to); rerr != nil {
				errs = append(errs, rerr)
				continue
			}
			moved = append(moved, to)
		}
		delete(recorded, key)
		changed = true
	}
	return moved, left, changed, errors.Join(errs...)
}

// writeRecord writes the record of dir whole, or leaves the one there.
func (s *Service) writeRecord(ctx context.Context, dir string, docs map[string]outdir.Entry) error {
	if err := outdir.Write(ctx, s.files, dir, RecordName, docs); err != nil {
		return fmt.Errorf("record the notes in %s: %w", filepath.Join(dir, RecordName), err)
	}
	return nil
}
