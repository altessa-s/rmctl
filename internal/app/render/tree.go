// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultJobs is how many documents of a folder are drawn at a time.
const DefaultJobs = fetch.DefaultJobs

// TopDir names the directory the whole library is drawn into without one.
const TopDir = "remarkable"

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
	if query == "" || query == "/" {
		return true, nil
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return false, err
	}
	it, found, err := tree.Resolve(query, true)
	switch {
	case err != nil:
		return false, err
	case !found:
		return false, fmt.Errorf("%s: %w", query, domain.ErrNotFound)
	}
	return it.Kind.IsFolder(), nil
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
func TreeFileName(name string) string {
	base := fetch.SafeName(name)
	for _, ext := range []string{".pdf", ".epub"} {
		if cut := len(base) - len(ext); cut > 0 && strings.EqualFold(base[cut:], ext) {
			base = base[:cut]
			break
		}
	}
	return base + ".pdf"
}

// Planned is one document of a folder drawing and the PDF it becomes.
type Planned struct {
	Document domain.Item
	// LibraryPath is the document's path in the library, Path its PDF's.
	LibraryPath, Path string
}

// TreePlan is the documents below the folder top of tree, each with the
// PDF it becomes under dir, in library path order. Every folder below top
// is a directory under dir, named by its safe name. Documents and folders
// in the trash stay out unless all, or unless top itself is in the trash.
// Two names that collide in one directory, compared without case, are
// told apart by identifier: in order of creation, then of identifier, the
// first keeps its name and the later ones get " (<first 8 characters of
// the identifier>)" before .pdf, or at the end of a folder's name.
func TreePlan(tree *domain.Tree, top domain.ItemID, dir string, all bool) []Planned {
	trash := all || (top != domain.RootID && tree.IsTrashed(top))
	var out []Planned
	seen := map[domain.ItemID]bool{top: true}
	var walk func(folder domain.ItemID, local string)
	walk = func(folder domain.ItemID, local string) {
		children := slices.Clone(tree.Children(folder, trash))
		slices.SortFunc(children, func(a, b domain.Item) int {
			return cmp.Or(a.Created.Compare(b.Created), strings.Compare(string(a.ID), string(b.ID)))
		})
		taken := map[string]bool{}
		for _, it := range children {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			name, ext := TreeFileName(it.Name), "pdf"
			if it.Kind.IsFolder() {
				name, ext = fetch.SafeName(it.Name), ""
			}
			if taken[strings.ToLower(name)] {
				name = fetch.Disambiguated(name, it.ID, ext)
			}
			taken[strings.ToLower(name)] = true
			path := filepath.Join(local, name)
			if it.Kind.IsFolder() {
				walk(it.ID, path)
				continue
			}
			out = append(out, Planned{Document: it, LibraryPath: tree.Path(it.ID), Path: path})
		}
	}
	walk(top, dir)
	slices.SortFunc(out, func(a, b Planned) int {
		return cmp.Or(strings.Compare(a.LibraryPath, b.LibraryPath), strings.Compare(string(a.Document.ID), string(b.Document.ID)))
	})
	return out
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
	top, name, err := topFolder(lib, query)
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
	recorded, dropped := pruned(readManifest(s.files, dir), dir, plan, options)
	if dropped {
		if err = writeManifest(ctx, s.files, dir, recorded); err != nil {
			return port.RenderTreeReport{}, err
		}
	}
	report := port.RenderTreeReport{Dir: dir, Documents: make([]port.RenderedDocument, len(plan))}
	done := make([]bool, len(plan))
	indexes := make([]int, len(plan))
	for i := range indexes {
		indexes[i] = i
	}
	jobs := tree.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	err = concurrency.Process(ctx, indexes, func(ctx context.Context, i int) error {
		// What is left recorded of the plan was drawn from the document as it
		// is now, with these options: only its PDF's time is left to check.
		_, known := recorded[manifestKey(dir, plan[i].Path)]
		d := s.renderOne(ctx, plan[i], opts, tree.Incremental && known)
		if d.Err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		report.Documents[i], done[i] = d, true
		return nil
	}, concurrency.WithConcurrency[int](jobs), concurrency.WithStopOnError[int]())
	// An interrupted drawing reports the documents it finished.
	finished := report.Documents[:0]
	for i, d := range report.Documents {
		if done[i] {
			finished = append(finished, d)
		}
	}
	report.Documents = finished
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
// writes it, if any was.
func (s *Service) record(ctx context.Context, dir string, recorded map[string]manifestEntry, docs []port.RenderedDocument, opts string) error {
	drawn := false
	for _, d := range docs {
		if d.Status == port.RenderRendered && !d.Document.Modified.IsZero() {
			recorded[manifestKey(dir, d.Report.Path)] = entryOf(d.Document, opts)
			drawn = true
		}
	}
	if !drawn {
		return nil
	}
	return writeManifest(ctx, s.files, dir, recorded)
}

// topFolder is the folder query names and the directory name it is drawn
// into by default.
func topFolder(tree *domain.Tree, query string) (domain.ItemID, string, error) {
	if query == "" || query == "/" {
		return domain.RootID, TopDir, nil
	}
	it, found, err := tree.Resolve(query, true)
	switch {
	case err != nil:
		return "", "", err
	case !found:
		return "", "", fmt.Errorf("%s: %w", query, domain.ErrNotFound)
	case !it.Kind.IsFolder():
		return "", "", fmt.Errorf("%s: %w", it.Name, ErrNotFolder)
	}
	return it.ID, fetch.SafeName(it.Name), nil
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
		if exists && sameTime(mtime, modified) {
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

// sameTime compares a file's modification time with a document's to the
// second: the coarsest a file system the PDFs go to keeps.
func sameTime(file, document time.Time) bool {
	return file.Truncate(time.Second).Equal(document.Truncate(time.Second))
}
