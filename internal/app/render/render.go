// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultScale is a PNG's or a JPEG's pixels per .rm unit when none is
// asked for: a page as wide as the tablet's screen, 1404 pixels.
const DefaultScale = 1.0

// MaxScale bounds the scale: a page sixteen times the tablet's screen.
const MaxScale = 16.0

// MaxQuality is the best a JPEG can be asked for; 0 is the renderer's
// default.
const MaxQuality = 100

// The failures of the options and of the document asked for.
var (
	// ErrBadOptions means options that cannot be drawn with.
	ErrBadOptions = errors.New("bad render options")
	// ErrFolder means a folder was asked to be drawn.
	ErrFolder = errors.New("a folder has no pages to draw; name a document")
	// ErrSeveralDocuments means a local archive holds several documents
	// and none was named.
	ErrSeveralDocuments = errors.New("the archive holds several documents; name one with --id")
)

// Service implements port.Rendering.
type Service struct {
	lib      *library.Library
	blobs    port.BlobStore
	files    port.LocalFiles
	reader   port.ArchiveReader
	writer   port.ArchiveWriter
	renderer port.Renderer
}

var _ port.Rendering = (*Service)(nil)

// New returns the render use case. A nil dependency panics.
func New(
	lib *library.Library, blobs port.BlobStore, files port.LocalFiles,
	reader port.ArchiveReader, writer port.ArchiveWriter, renderer port.Renderer,
) *Service {
	switch {
	case lib == nil:
		panic("render: Library is required")
	case blobs == nil:
		panic("render: BlobStore is required")
	case files == nil:
		panic("render: LocalFiles is required")
	case reader == nil:
		panic("render: ArchiveReader is required")
	case writer == nil:
		panic("render: ArchiveWriter is required")
	case renderer == nil:
		panic("render: Renderer is required")
	}
	return &Service{lib: lib, blobs: blobs, files: files, reader: reader, writer: writer, renderer: renderer}
}

// Normalized is opts with the defaults filled in, or ErrBadOptions: a
// page below 0, a format, mode or paper not known, a PDF of one page or an
// image of the whole document, a scale out of (0, MaxScale] or asked of
// anything but a PNG or a JPEG, a quality out of [0, MaxQuality] or asked
// of anything but a JPEG, or a mode asked of an image.
func Normalized(opts port.RenderOptions) (port.RenderOptions, error) {
	if opts.Page < 0 {
		return opts, fmt.Errorf("%w: page %d: pages count from 1", ErrBadOptions, opts.Page)
	}
	if opts.Paper == "" {
		opts.Paper = port.PaperA4
	}
	if !slices.Contains([]port.Paper{port.PaperA4, port.PaperLetter, port.PaperTablet}, opts.Paper) {
		return opts, fmt.Errorf("%w: unknown paper %q", ErrBadOptions, opts.Paper)
	}
	opts.Format = format(opts)
	if !slices.Contains([]port.Format{port.FormatPDF, port.FormatPNG, port.FormatJPEG, port.FormatSVG}, opts.Format) {
		return opts, fmt.Errorf("%w: unknown format %q", ErrBadOptions, opts.Format)
	}
	if opts.Quality != 0 && opts.Format != port.FormatJPEG {
		return opts, fmt.Errorf("%w: a quality is for a page drawn as a JPEG", ErrBadOptions)
	}
	if opts.Quality < 0 || opts.Quality > MaxQuality {
		return opts, fmt.Errorf("%w: quality %d is outside [1, %d]", ErrBadOptions, opts.Quality, MaxQuality)
	}
	if opts.Format == port.FormatPDF {
		if opts.Page != 0 {
			return opts, fmt.Errorf("%w: a page is drawn as an image: png, jpg or svg", ErrBadOptions)
		}
		if opts.Scale != 0 {
			return opts, fmt.Errorf("%w: a scale is for a page drawn as a PNG or a JPEG", ErrBadOptions)
		}
		if opts.Mode == "" {
			opts.Mode = port.PDFFresh
		}
		if !slices.Contains([]port.PDFMode{port.PDFFresh, port.PDFPreserving, port.PDFRecomposed}, opts.Mode) {
			return opts, fmt.Errorf("%w: unknown PDF mode %q", ErrBadOptions, opts.Mode)
		}
		return opts, nil
	}
	if opts.Page == 0 {
		return opts, fmt.Errorf("%w: a %s is of one page: name it", ErrBadOptions, opts.Format)
	}
	if opts.Mode != "" {
		return opts, fmt.Errorf("%w: a PDF mode is for a whole document drawn as a PDF", ErrBadOptions)
	}
	if opts.Format == port.FormatSVG {
		if opts.Scale != 0 {
			return opts, fmt.Errorf("%w: a scale is for a page drawn as a PNG or a JPEG", ErrBadOptions)
		}
		return opts, nil
	}
	if opts.Scale == 0 {
		opts.Scale = DefaultScale
	}
	if opts.Scale < 0 || opts.Scale > MaxScale {
		return opts, fmt.Errorf("%w: scale %g is outside (0, %g]", ErrBadOptions, opts.Scale, MaxScale)
	}
	return opts, nil
}

// format is the format opts asks for: a PNG for a page and a PDF for the
// whole document when it names none.
func format(opts port.RenderOptions) port.Format {
	switch {
	case opts.Format != "":
		return opts.Format
	case opts.Page > 0:
		return port.FormatPNG
	default:
		return port.FormatPDF
	}
}

// FileName is the name a drawing of the document name takes without -o:
// "<name>-rendered.pdf" for a PDF, so it never takes the place of the
// document's own PDF, and "<name>-p<N>.png", ".jpg" or ".svg" for a page.
// A .pdf or .epub the name ends with is dropped first.
func FileName(name string, opts port.RenderOptions) string {
	base := fetch.SafeName(name)
	for _, ext := range []string{".pdf", ".epub"} {
		if cut := len(base) - len(ext); cut > 0 && strings.EqualFold(base[cut:], ext) {
			base = base[:cut]
			break
		}
	}
	if f := format(opts); f != port.FormatPDF {
		return fmt.Sprintf("%s-p%d.%s", base, opts.Page, f)
	}
	return base + "-rendered.pdf"
}

// Render implements port.Rendering: the document's files are downloaded
// and packed into an archive in a staging directory, as export packs them,
// and the archive is drawn from there.
func (s *Service) Render(ctx context.Context, query, dest string, opts port.RenderOptions) (port.RenderReport, error) {
	opts, err := s.ready(opts)
	if err != nil {
		return port.RenderReport{}, err
	}
	it, err := s.document(ctx, query)
	if err != nil {
		return port.RenderReport{}, err
	}
	for _, f := range it.Files {
		if cerr := s.writer.CheckName(f.Name); cerr != nil {
			return port.RenderReport{}, fmt.Errorf("%s: %w", it.Name, cerr)
		}
	}
	path, err := fetch.Destination(s.files, dest, FileName(it.Name, opts))
	if err != nil {
		return port.RenderReport{}, err
	}
	archive, err := s.download(ctx, it)
	if err != nil {
		return port.RenderReport{}, err
	}
	return s.draw(ctx, archive, it, path, opts)
}

// RenderFile implements port.Rendering.
func (s *Service) RenderFile(ctx context.Context, local, dest string, opts port.RenderOptions) (port.RenderReport, error) {
	opts, err := s.ready(opts)
	if err != nil {
		return port.RenderReport{}, err
	}
	it, err := s.archived(local, opts.Document)
	if err != nil {
		return port.RenderReport{}, fmt.Errorf("%s: %w", local, err)
	}
	path, err := fetch.Destination(s.files, dest, FileName(it.Name, opts))
	if err != nil {
		return port.RenderReport{}, err
	}
	archive, err := s.readAll(local)
	if err != nil {
		return port.RenderReport{}, err
	}
	return s.draw(ctx, archive, it, path, opts)
}

// ready is opts normalized, once this build is known to draw.
func (s *Service) ready(opts port.RenderOptions) (port.RenderOptions, error) {
	opts, err := Normalized(opts)
	if err != nil {
		return opts, err
	}
	return opts, s.renderer.Available()
}

// draw renders the document it of archive and writes the result to path,
// whole or not at all.
func (s *Service) draw(ctx context.Context, archive []byte, it domain.Item, path string, opts port.RenderOptions) (port.RenderReport, error) {
	out, report, err := s.renderer.Render(ctx, archive, it.ID, opts)
	if err != nil {
		return port.RenderReport{}, fmt.Errorf("%s: %w", it.Name, err)
	}
	err = s.files.Produce(ctx, path, func(w io.Writer) error {
		_, werr := w.Write(out)
		return werr
	})
	if err != nil {
		return port.RenderReport{}, err
	}
	report.Path = path
	return report, nil
}

// document resolves a query to a document; trashed documents count.
func (s *Service) document(ctx context.Context, query string) (domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	it, found, err := tree.Resolve(query, true)
	switch {
	case err != nil:
		return domain.Item{}, err
	case !found:
		return domain.Item{}, fmt.Errorf("%s: %w", query, domain.ErrNotFound)
	case it.Kind.IsFolder():
		return domain.Item{}, fmt.Errorf("%s: %w", it.Name, ErrFolder)
	}
	return it, nil
}

// download packs the document's files into an archive in a staging
// directory and reads it back; the directory is removed after.
func (s *Service) download(ctx context.Context, it domain.Item) (data []byte, err error) {
	stage, remove, err := s.files.Stage()
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := remove(); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove the staging directory %s: %w", stage, rerr))
		}
	}()
	archive, _, err := fetch.Pack(ctx, s.blobs, s.files, s.writer, []domain.Item{it}, stage)
	if err != nil {
		return nil, err
	}
	return s.readAll(archive)
}

// readAll is the bytes of the local file at path.
func (s *Service) readAll(path string) ([]byte, error) {
	r, err := s.files.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// archived is the document of the local archive at path that want names
// (an identifier or a unique prefix of one), or its only document when
// want is empty. Folders do not count.
func (s *Service) archived(path, want string) (domain.Item, error) {
	arc, err := s.reader.OpenArchive(path)
	if err != nil {
		return domain.Item{}, err
	}
	defer func() { _ = arc.Close() }()
	groups := domain.GroupArchive(arc.Entries())
	metadata := map[string][]byte{}
	err = arc.Read(func(e domain.ArchiveEntry) bool { return e.Name == string(e.DocumentID)+".metadata" },
		func(e domain.ArchiveEntry, data []byte) error {
			metadata[e.Name] = slices.Clone(data)
			return nil
		})
	if err != nil {
		return domain.Item{}, err
	}
	var docs []domain.Item
	for _, g := range groups {
		raw, ok := metadata[string(g.ID)+".metadata"]
		if !ok || g.ID == "" {
			continue
		}
		meta, merr := domain.ParseMetadata(raw)
		if merr != nil || meta.Type() == domain.TypeCollection {
			continue
		}
		docs = append(docs, domain.Item{ID: g.ID, Name: meta.VisibleName()})
	}
	return pick(docs, want)
}

// pick is the one document of docs want names, or the only one.
func pick(docs []domain.Item, want string) (domain.Item, error) {
	if want == "" {
		switch len(docs) {
		case 0:
			return domain.Item{}, domain.ErrNotFound
		case 1:
			return docs[0], nil
		default:
			names := make([]string, len(docs))
			for i, d := range docs {
				names[i] = string(d.ID)
			}
			return domain.Item{}, fmt.Errorf("%w: %s", ErrSeveralDocuments, strings.Join(names, ", "))
		}
	}
	var found []domain.Item
	for _, d := range docs {
		if string(d.ID) == want {
			return d, nil
		}
		if strings.HasPrefix(string(d.ID), want) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return domain.Item{}, fmt.Errorf("%s: %w", want, domain.ErrNotFound)
	case 1:
		return found[0], nil
	default:
		return domain.Item{}, &domain.AmbiguousError{Query: want, Candidates: found}
	}
}
