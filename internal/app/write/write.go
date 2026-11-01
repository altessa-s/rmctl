// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrExists means an item with that name is already where it would go.
var ErrExists = errors.New("already exists")

// Service implements port.Writer.
type Service struct {
	lib      *library.Library
	docs     port.DocumentStore
	blobs    port.BlobStore
	files    port.LocalFiles
	pdf      port.PDFInspector
	archives port.ArchiveReader
	ids      port.IDs
	clock    port.Clock
}

var _ port.Writer = (*Service)(nil)

// New returns the write use case. A nil dependency panics.
func New(
	lib *library.Library, docs port.DocumentStore, blobs port.BlobStore, files port.LocalFiles,
	pdf port.PDFInspector, archives port.ArchiveReader, ids port.IDs, clk port.Clock,
) *Service {
	switch {
	case lib == nil:
		panic("write: Library is required")
	case docs == nil:
		panic("write: DocumentStore is required")
	case blobs == nil:
		panic("write: BlobStore is required")
	case files == nil:
		panic("write: LocalFiles is required")
	case pdf == nil:
		panic("write: PDFInspector is required")
	case archives == nil:
		panic("write: ArchiveReader is required")
	case ids == nil:
		panic("write: IDs is required")
	case clk == nil:
		panic("write: Clock is required")
	}
	return &Service{lib: lib, docs: docs, blobs: blobs, files: files, pdf: pdf, archives: archives, ids: ids, clock: clk}
}

// Mkdir implements port.Writer. With parents every missing folder of the
// path is created, in one commit, and an existing folder is not an error.
func (s *Service) Mkdir(ctx context.Context, p string, parents bool) (domain.Item, error) {
	parts := components(p)
	if len(parts) == 0 {
		return domain.Item{}, domain.ErrEmptyName
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	parent, existing, missing, err := walk(tree, parts)
	if err != nil {
		return domain.Item{}, err
	}
	if len(missing) == 0 {
		if parents {
			return existing, nil
		}
		return domain.Item{}, fmt.Errorf("%s: %w", p, ErrExists)
	}
	if len(missing) > 1 && !parents {
		return domain.Item{}, fmt.Errorf("%s: parent folder: %w", p, domain.ErrNotFound)
	}
	records, last, err := s.newFolders(ctx, missing, parent)
	if err != nil {
		return domain.Item{}, err
	}
	_, err = s.lib.Commit(ctx, func(_ context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		if cerr := checkDestination(tree, parent, domain.RootID); cerr != nil {
			return nil, cerr
		}
		return append(slices.Clone(state.Records), records...), nil
	})
	if err != nil {
		return domain.Item{}, err
	}
	return last, nil
}

// newFolders stores a chain of new folders, each inside the one before,
// the first inside parent. It returns their records and the last folder.
func (s *Service) newFolders(ctx context.Context, names []string, parent domain.ItemID) ([]domain.Record, domain.Item, error) {
	now := s.clock.Now()
	records := make([]domain.Record, 0, len(names))
	var last domain.Item
	for _, name := range names {
		id := s.ids.NewID()
		meta := domain.NewFolderMetadata(name, parent, now)
		files, err := s.uploadAll(ctx, []named{
			{name: string(id) + ".metadata", body: meta.Bytes()},
			{name: string(id) + ".content", body: domain.FolderContent()},
		})
		if err != nil {
			return nil, domain.Item{}, err
		}
		rec, err := s.docs.PutIndex(ctx, id, files)
		if err != nil {
			return nil, domain.Item{}, err
		}
		records = append(records, rec)
		last = domain.NewItem(rec, files, meta, nil)
		parent = id
	}
	return records, last, nil
}

// Move implements port.Writer. dst naming a folder moves src into it; any
// other dst is a new path, whose parent must exist and whose last component
// is the new name.
func (s *Service) Move(ctx context.Context, src, dst string) (domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	it, err := resolve(tree, src)
	if err != nil {
		return domain.Item{}, err
	}
	if tree.IsTrashed(it.ID) {
		return domain.Item{}, domain.ErrInTrash
	}
	parent, name, err := target(tree, it, dst)
	if err != nil {
		return domain.Item{}, err
	}
	result := it
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		records, moved, merr := s.moveChange(ctx, tree, state, it.ID, parent, name)
		if merr == nil {
			result = moved
		}
		return records, merr
	})
	return result, err
}

// moveChange puts the item id under parent with name in the current root.
func (s *Service) moveChange(
	ctx context.Context, tree *domain.Tree, state port.RootState, id, parent domain.ItemID, name string,
) ([]domain.Record, domain.Item, error) {
	cur, ok := tree.Get(id)
	if !ok {
		return nil, domain.Item{}, domain.ErrVanished
	}
	if tree.IsTrashed(cur.ID) {
		return nil, domain.Item{}, domain.ErrInTrash
	}
	if err := checkDestination(tree, parent, cur.ID); err != nil {
		return nil, domain.Item{}, err
	}
	meta, _, err := s.lib.Metadata(ctx, cur)
	if err != nil {
		return nil, domain.Item{}, err
	}
	edited := meta
	now := s.clock.Now()
	if meta.ParentID() != parent {
		edited = edited.Moved(parent, now)
	}
	if meta.VisibleName() != name {
		edited = edited.Renamed(name, now)
	}
	if string(edited.Bytes()) == string(meta.Bytes()) {
		return state.Records, cur, nil
	}
	rec, files, err := s.rewriteMetadata(ctx, cur, edited)
	if err != nil {
		return nil, domain.Item{}, err
	}
	records, _ := library.Replace(state.Records, rec)
	return records, domain.NewItem(rec, files, edited, cur.Tags), nil
}

// Trash implements port.Writer. An item already in the trash is left as it is.
func (s *Service) Trash(ctx context.Context, queries []string) ([]domain.Item, error) {
	return s.editEach(ctx, queries, func(tree *domain.Tree, it domain.Item, meta domain.Metadata) (domain.Metadata, bool, error) {
		if meta.Trashed() {
			return meta, false, nil
		}
		return meta.MovedToTrash(s.clock.Now()), true, nil
	})
}

// Restore implements port.Writer: the items come back into the folder dest,
// "" being the top level. dest must be a folder outside the trash, and not
// the restored item or one below it.
func (s *Service) Restore(ctx context.Context, queries []string, dest string) ([]domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	parent, err := destination(tree, dest)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dest, err)
	}
	items, err := resolveAll(tree, queries)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if !tree.IsTrashed(it.ID) {
			return nil, fmt.Errorf("%s: %w", it.Name, domain.ErrNotInTrash)
		}
	}
	return s.editEach(ctx, queries, func(tree *domain.Tree, it domain.Item, meta domain.Metadata) (domain.Metadata, bool, error) {
		if !tree.IsTrashed(it.ID) {
			return meta, false, nil
		}
		if err := checkDestination(tree, parent, it.ID); err != nil {
			return meta, false, err
		}
		return meta.Restored(parent, s.clock.Now()), true, nil
	})
}

// Remove implements port.Writer. A folder goes with everything below it, in
// one commit. The files stay in the cloud's storage: nothing references them.
func (s *Service) Remove(ctx context.Context, queries []string) ([]domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := resolveAll(tree, queries)
	if err != nil {
		return nil, err
	}
	var removed []domain.Item
	_, err = s.lib.Commit(ctx, func(_ context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		removed = nil
		gone := map[domain.ItemID]bool{}
		for _, t := range targets {
			it, ok := tree.Get(t.ID)
			if !ok {
				continue
			}
			for _, x := range append([]domain.Item{it}, tree.Descendants(it.ID)...) {
				if !gone[x.ID] {
					gone[x.ID] = true
					removed = append(removed, x)
				}
			}
		}
		return slices.DeleteFunc(slices.Clone(state.Records), func(r domain.Record) bool { return gone[r.ID] }), nil
	})
	return removed, err
}

// edit decides an item's new metadata; changed false leaves the item alone.
type edit func(tree *domain.Tree, it domain.Item, meta domain.Metadata) (domain.Metadata, bool, error)

// editEach resolves every query first, then applies fn to each item in one
// commit. A query that names nothing fails before anything is written.
func (s *Service) editEach(ctx context.Context, queries []string, fn edit) ([]domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := resolveAll(tree, queries)
	if err != nil {
		return nil, err
	}
	var results []domain.Item
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		records, edited, eerr := s.editChange(ctx, tree, state, targets, fn)
		results = edited
		return records, eerr
	})
	return results, err
}

// editChange applies fn to each target in the current root.
func (s *Service) editChange(
	ctx context.Context, tree *domain.Tree, state port.RootState, targets []domain.Item, fn edit,
) ([]domain.Record, []domain.Item, error) {
	records := state.Records
	results := make([]domain.Item, 0, len(targets))
	for _, target := range targets {
		cur, ok := tree.Get(target.ID)
		if !ok {
			return nil, nil, domain.ErrVanished
		}
		meta, _, err := s.lib.Metadata(ctx, cur)
		if err != nil {
			return nil, nil, err
		}
		edited, changed, err := fn(tree, cur, meta)
		if err != nil {
			return nil, nil, err
		}
		if !changed {
			results = append(results, cur)
			continue
		}
		rec, files, err := s.rewriteMetadata(ctx, cur, edited)
		if err != nil {
			return nil, nil, err
		}
		records, _ = library.Replace(records, rec)
		results = append(results, domain.NewItem(rec, files, edited, cur.Tags))
	}
	return records, results, nil
}

// rewriteMetadata stores new metadata for it and the index that lists it;
// every other file keeps its hash.
func (s *Service) rewriteMetadata(ctx context.Context, it domain.Item, meta domain.Metadata) (domain.Record, []domain.File, error) {
	f, err := s.blobs.Upload(ctx, string(it.ID)+".metadata", meta.Bytes())
	if err != nil {
		return domain.Record{}, nil, err
	}
	files := library.ReplaceFile(it.Files, f)
	rec, err := s.docs.PutIndex(ctx, it.ID, files)
	return rec, files, err
}

// named is a file to upload.
// named is a file to upload under name: body in memory, or, when path is
// set, the local file there.
type named struct {
	name string
	body []byte
	path string
}

func (s *Service) uploadAll(ctx context.Context, list []named) ([]domain.File, error) {
	files := make([]domain.File, 0, len(list))
	for _, n := range list {
		var f domain.File
		var err error
		if n.path != "" {
			f, err = s.blobs.UploadFile(ctx, n.path, n.name, nil)
		} else {
			f, err = s.blobs.Upload(ctx, n.name, n.body)
		}
		if err != nil {
			return nil, fmt.Errorf("upload %s: %w", n.name, err)
		}
		files = append(files, f)
	}
	return files, nil
}

// checkDestination refuses a folder that is gone, in the trash, not a
// folder, or, when moving is set, the moving item or one below it.
func checkDestination(tree *domain.Tree, parent, moving domain.ItemID) error {
	if parent == domain.RootID {
		return nil
	}
	p, ok := tree.Get(parent)
	if !ok || tree.IsTrashed(parent) {
		return domain.ErrDestinationGone
	}
	if !p.Kind.IsFolder() {
		return domain.ErrNotAFolder
	}
	if moving != domain.RootID && tree.IsWithin(parent, moving) {
		return domain.ErrIntoOwnSubtree
	}
	return nil
}

// resolve finds the item a query names, trashed items included.
func resolve(tree *domain.Tree, query string) (domain.Item, error) {
	it, found, err := tree.Resolve(query, true)
	if err != nil {
		return domain.Item{}, fmt.Errorf("%s: %w", query, err)
	}
	if !found {
		return domain.Item{}, fmt.Errorf("%s: %w", query, domain.ErrNotFound)
	}
	return it, nil
}

// resolveAll resolves every query; the first that names nothing fails.
func resolveAll(tree *domain.Tree, queries []string) ([]domain.Item, error) {
	items := make([]domain.Item, 0, len(queries))
	for _, q := range queries {
		it, err := resolve(tree, q)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

// destination is the folder dest names; a folder in the trash is a
// destination gone rather than one that is not there.
func destination(tree *domain.Tree, dest string) (domain.ItemID, error) {
	id, err := tree.ResolveFolder(dest)
	if errors.Is(err, domain.ErrNotFound) && trashedName(tree, dest) {
		return domain.RootID, domain.ErrDestinationGone
	}
	return id, err
}

// trashedName reports whether query names an item in the trash.
func trashedName(tree *domain.Tree, query string) bool {
	it, found, err := tree.Resolve(query, true)
	return err == nil && found && tree.IsTrashed(it.ID)
}

// target is the parent and the name a move to dst gives it.
func target(tree *domain.Tree, it domain.Item, dst string) (domain.ItemID, string, error) {
	if dst == "" || dst == "/" {
		return domain.RootID, it.Name, nil
	}
	found, ok, err := tree.Resolve(dst, false)
	switch {
	case errors.Is(err, domain.ErrNotFound) && trashedName(tree, dst):
		// A folder in the trash is a destination gone, not a new name.
		return "", "", fmt.Errorf("%s: %w", dst, domain.ErrDestinationGone)
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return "", "", err
	case ok && found.Kind.IsFolder():
		return found.ID, it.Name, nil
	case ok && found.ID != it.ID:
		return "", "", fmt.Errorf("%s: %w", dst, ErrExists)
	}
	dir, name := path.Split("/" + strings.Trim(dst, "/"))
	if name == "" {
		return "", "", domain.ErrEmptyName
	}
	parent, err := destination(tree, dir)
	if err != nil {
		return "", "", err
	}
	return parent, name, nil
}

// components splits a library path into its non-empty parts.
func components(p string) []string {
	var parts []string
	for part := range strings.SplitSeq(p, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// walk follows parts down the existing folders. It returns the deepest
// folder found, that folder as an item, and the parts below it that are
// missing.
func walk(tree *domain.Tree, parts []string) (domain.ItemID, domain.Item, []string, error) {
	parent := domain.RootID
	var current domain.Item
	for i, part := range parts {
		var matches []domain.Item
		for _, child := range tree.Children(parent, false) {
			if child.Name == part {
				matches = append(matches, child)
			}
		}
		switch len(matches) {
		case 0:
			return parent, current, parts[i:], nil
		case 1:
			if !matches[0].Kind.IsFolder() {
				return "", domain.Item{}, nil, fmt.Errorf("%s: %w", part, domain.ErrNotAFolder)
			}
			current, parent = matches[0], matches[0].ID
		default:
			return "", domain.Item{}, nil, &domain.AmbiguousError{Query: "/" + strings.Join(parts[:i+1], "/"), Candidates: matches}
		}
	}
	return parent, current, nil, nil
}
