// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// itemEdit decides an item's new .metadata and, when it returns one, its
// new .content (nil keeps the .content); changed false leaves the item
// alone. content is the item's .content, when the edit asked for it.
type itemEdit func(cur domain.Item, meta domain.Metadata, content []byte) (domain.Metadata, []byte, bool, error)

// Rename implements port.Writer: the item keeps its folder and takes name.
func (s *Service) Rename(ctx context.Context, query, name string) (port.Edited, error) {
	switch {
	case strings.TrimSpace(name) == "":
		return port.Edited{}, domain.ErrEmptyName
	case strings.Contains(name, "/"):
		return port.Edited{}, fmt.Errorf("%q: %w", name, domain.ErrSlashInName)
	}
	out, err := s.editItems(ctx, []string{query}, false, nil, func(_ domain.Item, meta domain.Metadata, _ []byte) (domain.Metadata, []byte, bool, error) {
		if meta.VisibleName() == name {
			return meta, nil, false, nil
		}
		return meta.Renamed(name, s.clock.Now()), nil, true, nil
	})
	if err != nil {
		return port.Edited{}, err
	}
	return out[0], nil
}

// Pin implements port.Writer.
func (s *Service) Pin(ctx context.Context, queries []string, on bool) ([]port.Edited, error) {
	return s.editItems(ctx, queries, false, nil, func(_ domain.Item, meta domain.Metadata, _ []byte) (domain.Metadata, []byte, bool, error) {
		if meta.Pinned() == on {
			return meta, nil, false, nil
		}
		return meta.WithPinned(on, s.clock.Now()), nil, true, nil
	})
}

// Tag implements port.Writer: the .content's tags change, every other field
// stays, and the .metadata is marked edited so the tablet takes the change.
func (s *Service) Tag(ctx context.Context, queries []string, add, remove []string) ([]port.Edited, error) {
	noFolders := func(it domain.Item) error {
		if it.Kind.IsFolder() {
			return fmt.Errorf("%s: %w", it.Name, domain.ErrFolderTags)
		}
		return nil
	}
	return s.editItems(ctx, queries, true, noFolders, func(cur domain.Item, meta domain.Metadata, content []byte) (domain.Metadata, []byte, bool, error) {
		if err := noFolders(cur); err != nil {
			return meta, nil, false, err
		}
		now := s.clock.Now()
		before := domain.ContentTagList(content)
		after := domain.WithoutTags(domain.MergeTags(before, add, now.UnixMilli()), remove)
		if slices.Equal(domain.TagNames(before), domain.TagNames(after)) {
			return meta, nil, false, nil
		}
		next, err := domain.ContentWithTags(content, after)
		if err != nil {
			return meta, nil, false, err
		}
		return meta.Edited(now), next, true, nil
	})
}

// editItems resolves every query and checks each item with check before
// anything is written, then applies fn to each item in one commit, replayed
// against the current root on a conflict. An item named twice is edited
// once. Items fn leaves unchanged are reported unchanged and cost nothing;
// with none changed, nothing is committed.
func (s *Service) editItems(ctx context.Context, queries []string, needContent bool, check func(domain.Item) error, fn itemEdit) ([]port.Edited, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := resolveAll(tree, queries)
	if err != nil {
		return nil, err
	}
	seen := map[domain.ItemID]bool{}
	targets = slices.DeleteFunc(targets, func(it domain.Item) bool {
		dup := seen[it.ID]
		seen[it.ID] = true
		return dup
	})
	for _, it := range targets {
		if check != nil {
			if cerr := check(it); cerr != nil {
				return nil, cerr
			}
		}
	}
	var results []port.Edited
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		records := state.Records
		results = results[:0]
		for _, target := range targets {
			cur, ok := tree.Get(target.ID)
			if !ok {
				return nil, domain.ErrVanished
			}
			edited, rec, eerr := s.editOne(ctx, cur, needContent, fn)
			if eerr != nil {
				return nil, eerr
			}
			if edited.Changed {
				records, _ = library.Replace(records, rec)
			}
			results = append(results, edited)
		}
		return records, nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// editOne applies fn to cur and stores what it changed: the new .metadata,
// the new .content if any, and the index; every other file keeps its hash.
func (s *Service) editOne(ctx context.Context, cur domain.Item, needContent bool, fn itemEdit) (port.Edited, domain.Record, error) {
	meta, _, err := s.lib.Metadata(ctx, cur)
	if err != nil {
		return port.Edited{}, domain.Record{}, err
	}
	var content []byte
	if needContent {
		f, ok := cur.File("content")
		if !ok {
			return port.Edited{}, domain.Record{}, domain.MalformedFile("content", "missing from "+string(cur.ID))
		}
		if content, err = s.lib.Read(ctx, f); err != nil {
			return port.Edited{}, domain.Record{}, err
		}
	}
	nextMeta, nextContent, changed, err := fn(cur, meta, content)
	if err != nil || !changed {
		return port.Edited{Item: cur}, domain.Record{}, err
	}
	list := []named{{name: string(cur.ID) + ".metadata", body: nextMeta.Bytes()}}
	tags := cur.Tags
	if nextContent != nil {
		list = append(list, named{name: string(cur.ID) + ".content", body: nextContent})
		tags = domain.ContentTags(nextContent)
	}
	uploaded, err := s.uploadAll(ctx, list)
	if err != nil {
		return port.Edited{}, domain.Record{}, err
	}
	files := cur.Files
	for _, f := range uploaded {
		files = library.ReplaceFile(files, f)
	}
	rec, err := s.docs.PutIndex(ctx, cur.ID, files)
	if err != nil {
		return port.Edited{}, domain.Record{}, err
	}
	return port.Edited{Item: domain.NewItem(rec, files, nextMeta, tags), Changed: true}, rec, nil
}
