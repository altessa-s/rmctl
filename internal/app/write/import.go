// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrEmptyArchive means an archive holds no document.
var ErrEmptyArchive = errors.New("the archive holds no document")

// ErrNotADocument means an archive's entries do not make a document the
// library can take: no .metadata or .content, a folder, or an entry that
// belongs to no document.
var ErrNotADocument = errors.New("not a document")

// archived is one document of an archive, its files in archive order: a
// .metadata and a .content in memory, every other file staged on disk.
type archived struct {
	id    domain.ItemID
	files []named
	sizes []int64
	meta  domain.Metadata
	tags  []string
}

// placed is a document uploaded under its final identifier and parent.
type placed struct {
	rec   domain.Record
	files []domain.File
	meta  domain.Metadata
}

type placement struct {
	from, id, parent domain.ItemID
}

// Import implements port.Writer. Every document of the archive is uploaded
// under its record names, its .metadata filed under dest, and the documents
// go into the root in one commit. A document whose identifier the library
// holds takes a fresh one, and its files are renamed to it, so the
// library's document is never touched. When dest is gone or in the trash by
// the time of the commit, the documents land at the top level. The rest of
// the metadata stays as the archive holds it: an import is not an edit.
func (s *Service) Import(ctx context.Context, local, dest string) (_ []port.ImportedDocument, err error) {
	stage, remove, err := s.files.Stage()
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := remove(); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove the staging directory %s: %w", stage, rerr))
		}
	}()
	docs, err := s.readArchive(ctx, local, stage)
	if err != nil {
		return nil, err
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	parent, err := destination(tree, dest)
	if err != nil {
		return nil, err
	}
	uploaded := map[placement]placed{}
	chosen := map[domain.ItemID]domain.ItemID{}
	var result []port.ImportedDocument
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		target := parent
		if checkDestination(tree, parent, domain.RootID) != nil {
			target = domain.RootID
		}
		taken := make(map[domain.ItemID]bool, len(state.Records)+len(docs))
		for _, rec := range state.Records {
			taken[rec.ID] = true
		}
		records := slices.Clone(state.Records)
		result = result[:0]
		for _, d := range docs {
			id := s.identifier(d.id, chosen, taken)
			key := placement{from: d.id, id: id, parent: target}
			p, ok := uploaded[key]
			if !ok {
				var perr error
				if p, perr = s.place(ctx, d, id, target); perr != nil {
					return nil, perr
				}
				uploaded[key] = p
			}
			records = append(records, p.rec)
			result = append(result, port.ImportedDocument{Item: domain.NewItem(p.rec, p.files, p.meta, d.tags), From: d.id})
		}
		return records, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PlanImport implements port.Writer: the documents Import would add, under
// the identifiers it would give them against the current root, and the
// folder they would land in. Nothing is uploaded and nothing is committed;
// a library that moves before the real import may still change an
// identifier, and Import checks again.
func (s *Service) PlanImport(ctx context.Context, local, dest string) ([]port.ImportedDocument, error) {
	docs, err := s.readArchive(ctx, local, "")
	if err != nil {
		return nil, err
	}
	tree, state, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	parent, err := destination(tree, dest)
	if err != nil {
		return nil, err
	}
	// Every record of the root holds its identifier, even one the tree left
	// out for an unreadable .metadata.
	taken := make(map[domain.ItemID]bool, len(state.Records)+len(docs))
	for _, rec := range state.Records {
		taken[rec.ID] = true
	}
	chosen := map[domain.ItemID]domain.ItemID{}
	result := make([]port.ImportedDocument, 0, len(docs))
	for _, d := range docs {
		id := s.identifier(d.id, chosen, taken)
		files := make([]domain.File, len(d.files))
		for i, f := range d.files {
			name, _ := domain.RenameRecord(f.name, d.id, id)
			files[i] = domain.File{Name: name, Size: d.sizes[i]}
		}
		meta := d.meta.ImportedUnder(parent)
		result = append(result, port.ImportedDocument{Item: domain.NewItem(domain.Record{ID: id}, files, meta, d.tags), From: d.id})
	}
	return result, nil
}

// identifier is the identifier document from goes in under: its own while
// the library does not hold it, else the fresh one chosen on an earlier
// attempt while that is still free, else a new one.
func (s *Service) identifier(from domain.ItemID, chosen map[domain.ItemID]domain.ItemID, taken map[domain.ItemID]bool) domain.ItemID {
	id := from
	if taken[id] {
		id = chosen[from]
		for id == "" || taken[id] {
			id = s.ids.NewID()
		}
		chosen[from] = id
	}
	taken[id] = true
	return id
}

// place uploads one document under id with its .metadata filed under
// parent, and stores its index.
func (s *Service) place(ctx context.Context, d archived, id, parent domain.ItemID) (placed, error) {
	meta := d.meta.ImportedUnder(parent)
	list := make([]named, len(d.files))
	for i, f := range d.files {
		name, _ := domain.RenameRecord(f.name, d.id, id)
		n := named{name: name, body: f.body, path: f.path}
		if f.name == string(d.id)+".metadata" {
			n.body, n.path = meta.Bytes(), ""
		}
		list[i] = n
	}
	files, err := s.uploadAll(ctx, list)
	if err != nil {
		return placed{}, err
	}
	rec, err := s.docs.PutIndex(ctx, id, files)
	if err != nil {
		return placed{}, err
	}
	return placed{rec: rec, files: files, meta: meta}, nil
}

// readArchive opens a local archive and takes out its documents, each with
// a .metadata that decodes and a .content. Those two stay in memory; with a
// stage, every other entry is written to a file of its own there, so no
// more than one entry is in memory at a time. Without one, only the
// .metadata and .content are read.
func (s *Service) readArchive(ctx context.Context, local, stage string) ([]archived, error) {
	arc, err := s.archives.OpenArchive(local)
	if err != nil {
		return nil, err
	}
	defer func() { _ = arc.Close() }()
	entries := arc.Entries()
	groups := domain.GroupArchive(entries)
	if len(groups) == 0 {
		return nil, fmt.Errorf("%s: %w", local, ErrEmptyArchive)
	}
	keep := sidecar
	if stage != "" {
		keep = nil
	}
	bodies := map[string][]byte{}
	staged := map[string]string{}
	err = arc.Read(keep, func(e domain.ArchiveEntry, data []byte) error {
		if sidecar(e) {
			bodies[e.Name] = bytes.Clone(data)
			return nil
		}
		// The entry's own name may hold slashes or worse: the file is
		// named by its count instead.
		path := filepath.Join(stage, strconv.Itoa(len(staged)))
		staged[e.Name] = path
		return s.files.Produce(ctx, path, func(w io.Writer) error {
			_, werr := w.Write(data)
			return werr
		})
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", local, err)
	}
	docs := make([]archived, 0, len(groups))
	for _, g := range groups {
		d, err := document(g, bodies, staged)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", local, err)
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// sidecar reports the entries an import reads: a document's .metadata and
// .content.
func sidecar(e domain.ArchiveEntry) bool {
	return e.DocumentID != "" && (e.Name == string(e.DocumentID)+".metadata" || e.Name == string(e.DocumentID)+".content")
}

// document is one group of an archive as a document to import.
func document(g domain.ArchiveDocument, bodies map[string][]byte, staged map[string]string) (archived, error) {
	if g.ID == "" {
		return archived{}, fmt.Errorf("%q belongs to no document: %w", g.Entries[0].Name, ErrNotADocument)
	}
	d := archived{id: g.ID}
	for _, e := range g.Entries {
		d.files = append(d.files, named{name: e.Name, body: bodies[e.Name], path: staged[e.Name]})
		d.sizes = append(d.sizes, e.Size)
	}
	raw, ok := bodies[string(g.ID)+".metadata"]
	if !ok {
		return archived{}, fmt.Errorf("%s has no .metadata: %w", g.ID, ErrNotADocument)
	}
	meta, err := domain.ParseMetadata(raw)
	if err != nil {
		return archived{}, fmt.Errorf("%s: %w", g.ID, err)
	}
	if meta.Type() == domain.TypeCollection {
		return archived{}, fmt.Errorf("%s is a folder: %w", meta.VisibleName(), ErrNotADocument)
	}
	content, ok := bodies[string(g.ID)+".content"]
	if !ok {
		return archived{}, fmt.Errorf("%s has no .content: %w", meta.VisibleName(), ErrNotADocument)
	}
	d.meta = meta
	d.tags = domain.ContentTags(content)
	return d, nil
}
