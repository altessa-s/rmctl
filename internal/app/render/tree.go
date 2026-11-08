// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/outdir"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultJobs is how many documents of a folder are drawn at a time.
const DefaultJobs = fetch.DefaultJobs

// TopDir names the directory the whole library is drawn into without one.
const TopDir = outdir.TopDir

// The failures of a folder drawing.
var (
	// ErrNotFolder means RenderTree was asked of a document.
	ErrNotFolder = errors.New("not a folder: a document is drawn on its own")
	// ErrDocumentsFailed means some documents of a folder could not be
	// drawn; the others were.
	ErrDocumentsFailed = errors.New("documents could not be drawn")
)

// Folder implements port.Rendering.
func (s *Service) Folder(ctx context.Context, query string) (bool, error) {
	return outdir.Folder(ctx, s.lib, query)
}

// TreeOptions is opts for drawing a folder, with the defaults filled in,
// or ErrBadOptions: every document of a folder is drawn whole, as a PDF,
// so a page, an image format, a scale or a quality is refused, and so is
// the document of a local archive.
func TreeOptions(opts port.RenderOptions) (port.RenderOptions, error) {
	switch {
	case opts.Page != 0:
		return opts, fmt.Errorf("%w: a folder's documents are drawn whole: a page is for one document", ErrBadOptions)
	case opts.Format != "" && opts.Format != port.FormatPDF:
		return opts, fmt.Errorf("%w: a folder's documents are drawn as PDFs, not as %s", ErrBadOptions, opts.Format)
	case opts.Scale != 0 || opts.Quality != 0:
		return opts, fmt.Errorf("%w: a scale or a quality is for a page drawn as an image", ErrBadOptions)
	case opts.Document != "":
		return opts, fmt.Errorf("%w: a document of a local archive is not a folder", ErrBadOptions)
	}
	return Normalized(opts)
}

// TreeFileName is the name of a document's PDF in a folder drawn: its safe
// name, a .pdf or .epub it ends with replaced by .pdf.
func TreeFileName(name string) string { return domain.DerivedFileName(name, "pdf") }

// Planned is one document of a folder drawing and the PDF it becomes.
type Planned = outdir.Planned

// TreePlan is the documents below the folder top of tree, each with the
// PDF it becomes under dir, in library path order, laid out as
// [outdir.Plan] lays them out: a directory per folder, names that collide
// told apart by identifier, the trash left out unless all, or unless top
// itself is in the trash.
func TreePlan(tree *domain.Tree, top domain.ItemID, dir string, all bool) []Planned {
	return outdir.Plan(tree, top, dir, all, "pdf")
}

// RenderTree implements port.Rendering. Each document is downloaded,
// packed and drawn as Render draws one, opts.Jobs at a time; a document
// that fails is reported and the others go on. Only the directories that
// receive a PDF are created. The directory keeps a manifest, [ManifestName],
// of what each PDF was drawn from and with which options: an incremental
// drawing keeps a PDF only when it records the same document, modified at
// the same time, drawn with the same options, and the PDF still has that
// time. A manifest missing or unreadable keeps nothing.
func (s *Service) RenderTree(
	ctx context.Context, query, dir string, opts port.RenderOptions, tree port.RenderTreeOptions,
) (port.RenderTreeReport, error) {
	opts, err := TreeOptions(opts)
	if err != nil {
		return port.RenderTreeReport{}, err
	}
	if err = s.renderer.Available(); err != nil {
		return port.RenderTreeReport{}, err
	}
	lib, _, err := s.lib.Load(ctx)
	if err != nil {
		return port.RenderTreeReport{}, err
	}
	top, name, err := outdir.Top(lib, query, ErrNotFolder)
	if err != nil {
		return port.RenderTreeReport{}, err
	}
	if dir == "" {
		dir = name
	}
	if err = s.files.MkdirAll(dir); err != nil {
		return port.RenderTreeReport{}, err
	}
	plan := TreePlan(lib, top, dir, tree.All)
	options := fingerprint(opts)
	recorded, dropped := outdir.Pruned(outdir.Read(s.files, dir, ManifestName), dir, plan, options)
	if dropped {
		if err = writeManifest(ctx, s.files, dir, recorded); err != nil {
			return port.RenderTreeReport{}, err
		}
	}
	jobs := tree.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	// An interrupted drawing reports the documents it finished.
	finished, err := outdir.Each(ctx, len(plan), jobs, func(ctx context.Context, i int) (port.RenderedDocument, error) {
		// What is left recorded of the plan was drawn from the document as it
		// is now, with these options: only its PDF's time is left to check.
		_, known := recorded[outdir.Key(dir, plan[i].Path)]
		d := s.renderOne(ctx, plan[i], opts, tree.Incremental && known)
		if d.Err != nil && ctx.Err() != nil {
			return d, ctx.Err()
		}
		return d, nil
	})
	report := port.RenderTreeReport{Dir: dir, Documents: finished}
	if werr := s.record(ctx, dir, recorded, finished, options); werr != nil {
		return report, errors.Join(err, werr)
	}
	if err != nil {
		return report, err
	}
	if failed := report.Count(port.RenderFailed); failed > 0 {
		return report, fmt.Errorf("%d of %d %w", failed, len(report.Documents), ErrDocumentsFailed)
	}
	return report, nil
}

// record adds the documents drawn to the manifest of dir, recorded, and
// writes it, if any was. A PDF holding a page that did not parse is left
// out, its record dropped: what stands in that page's place is not the
// document as it is, and an incremental drawing draws it again, which a
// later rmctl may read. So the policy for such pages is no part of the
// options recorded: it changes no PDF that is recorded.
func (s *Service) record(ctx context.Context, dir string, recorded map[string]outdir.Entry, docs []port.RenderedDocument, opts string) error {
	drawn := false
	for _, d := range docs {
		if d.Status != port.RenderRendered || d.Document.Modified.IsZero() {
			continue
		}
		key := outdir.Key(dir, d.Report.Path)
		if d.Report.UnreadablePages > 0 {
			delete(recorded, key)
		} else {
			recorded[key] = outdir.EntryOf(d.Document, opts)
		}
		drawn = true
	}
	if !drawn {
		return nil
	}
	return writeManifest(ctx, s.files, dir, recorded)
}

// renderOne draws one planned document, or finds it unchanged.
func (s *Service) renderOne(ctx context.Context, p Planned, opts port.RenderOptions, incremental bool) port.RenderedDocument {
	d := port.RenderedDocument{Document: p.Document, LibraryPath: p.LibraryPath, Report: port.RenderReport{Path: p.Path}}
	fail := func(err error) port.RenderedDocument {
		d.Status, d.Err = port.RenderFailed, err
		return d
	}
	modified := p.Document.Modified
	if incremental && !modified.IsZero() {
		mtime, exists, err := s.files.ModTime(p.Path)
		if err != nil {
			return fail(err)
		}
		if exists && outdir.SameTime(mtime, modified) {
			d.Status = port.RenderUnchanged
			return d
		}
	}
	for _, f := range p.Document.Files {
		if err := s.writer.CheckName(f.Name); err != nil {
			return fail(err)
		}
	}
	if err := s.files.MkdirAll(filepath.Dir(p.Path)); err != nil {
		return fail(err)
	}
	archive, err := s.download(ctx, p.Document)
	if err != nil {
		return fail(err)
	}
	report, err := s.draw(ctx, archive, p.Document, p.Path, opts)
	if err != nil {
		return fail(err)
	}
	d.Report = report
	if !modified.IsZero() {
		if err = s.files.SetModTime(p.Path, modified); err != nil {
			return fail(fmt.Errorf("set the modification time of %s: %w", p.Path, err))
		}
	}
	d.Status = port.RenderRendered
	return d
}
