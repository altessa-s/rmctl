// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package archives

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Service implements port.Archives.
type Service struct {
	archives port.ArchiveReader
	pages    port.PageReader
}

var _ port.Archives = (*Service)(nil)

// New returns the archives use case. A nil dependency panics.
func New(archives port.ArchiveReader, pageReader port.PageReader) *Service {
	switch {
	case archives == nil:
		panic("archives: ArchiveReader is required")
	case pageReader == nil:
		panic("archives: PageReader is required")
	}
	return &Service{archives: archives, pages: pageReader}
}

// Inspect implements port.Archives. Only the .metadata and .content of each
// document are read; a document whose .metadata is missing or does not
// decode is listed with its entries and no name.
func (s *Service) Inspect(_ context.Context, path string) (port.ArchiveReport, error) {
	arc, err := s.archives.OpenArchive(path)
	if err != nil {
		return port.ArchiveReport{}, err
	}
	defer func() { _ = arc.Close() }()
	groups := domain.GroupArchive(arc.Entries())
	bodies, err := read(arc, sidecar)
	if err != nil {
		return port.ArchiveReport{}, fmt.Errorf("%s: %w", path, err)
	}
	report := port.ArchiveReport{Path: path}
	for _, g := range groups {
		doc := port.ArchiveDocumentReport{ID: g.ID, Entries: g.Entries}
		if it, ok := item(g, bodies); ok {
			doc.Name, doc.Kind = it.Name, it.Kind
		}
		report.Documents = append(report.Documents, doc)
	}
	return report, nil
}

// Pages implements port.Archives: every document of the archive that has
// a .metadata, folders aside, with its pages parsed in its own order. Only
// the .metadata, .content and .rm pages are read: an original PDF or EPUB,
// thumbnails and the rest stay in the archive.
func (s *Service) Pages(ctx context.Context, path string, selected []string) ([]port.DocumentText, error) {
	arc, err := s.archives.OpenArchive(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = arc.Close() }()
	groups := domain.GroupArchive(arc.Entries())
	bodies, err := read(arc, func(e domain.ArchiveEntry) bool { return sidecar(e) || pageEntry(e) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	fetch := func(_ context.Context, f domain.File) ([]byte, error) {
		data, ok := bodies[f.Name]
		if !ok {
			return nil, fmt.Errorf("%s: %w", f.Name, fs.ErrNotExist)
		}
		return data, nil
	}
	var out []port.DocumentText
	for _, g := range groups {
		it, ok := item(g, bodies)
		if !ok || it.Kind.IsFolder() {
			continue
		}
		read, err := pages.ReadPages(ctx, s.pages, it, selected, fetch, pages.DefaultConcurrency)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", it.Name, err)
		}
		out = append(out, port.DocumentText{Item: it, Pages: read})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %w", path, domain.ErrNotFound)
	}
	return out, nil
}

// sidecar reports a document's .metadata or .content.
func sidecar(e domain.ArchiveEntry) bool {
	return e.Name == string(e.DocumentID)+".metadata" || e.Name == string(e.DocumentID)+".content"
}

// pageEntry reports a page of a document: <id>/<page>.rm.
func pageEntry(e domain.ArchiveEntry) bool {
	rest, ok := strings.CutPrefix(e.Name, string(e.DocumentID)+"/")
	return ok && e.DocumentID != "" && strings.HasSuffix(rest, ".rm") && !strings.Contains(rest, "/")
}

// read takes the bodies of the entries keep selects.
func read(arc port.Archive, keep func(domain.ArchiveEntry) bool) (map[string][]byte, error) {
	bodies := map[string][]byte{}
	err := arc.Read(keep, func(e domain.ArchiveEntry, data []byte) error {
		bodies[e.Name] = bytes.Clone(data)
		return nil
	})
	return bodies, err
}

// item is one archived document as the library models it, when its
// .metadata decodes. Its files are its entries; they have no hash.
func item(g domain.ArchiveDocument, bodies map[string][]byte) (domain.Item, bool) {
	raw, ok := bodies[string(g.ID)+".metadata"]
	if !ok || g.ID == "" {
		return domain.Item{}, false
	}
	meta, err := domain.ParseMetadata(raw)
	if err != nil {
		return domain.Item{}, false
	}
	files := make([]domain.File, len(g.Entries))
	var size int64
	for i, e := range g.Entries {
		files[i] = domain.File{Name: e.Name, Size: e.Size}
		size += e.Size
	}
	tags := domain.ContentTags(bodies[string(g.ID)+".content"])
	return domain.NewItem(domain.Record{ID: g.ID, Subfiles: len(files), Size: size}, files, meta, tags), true
}
