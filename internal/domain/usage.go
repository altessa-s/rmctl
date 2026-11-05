// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"cmp"
	"slices"
)

// KindUsage is what the documents of one kind take.
type KindUsage struct {
	Kind      Kind
	Documents int
	Bytes     int64
}

// FolderUsage is what a folder takes with everything below it: the bytes
// of every document's files, the documents and the folders below it.
type FolderUsage struct {
	ID   ItemID
	Path string
	// Depth is how far below the folder asked about this one is: 0 for
	// that folder itself.
	Depth     int
	Bytes     int64
	Documents int
	Folders   int
}

// Usage is what a folder and the folders below it take.
type Usage struct {
	// Total is the folder asked about.
	Total FolderUsage
	// Folders are the folders down to the depth asked for, largest first.
	Folders []FolderUsage
	// Kinds are the documents below the folder by kind, largest first.
	Kinds []KindUsage
}

// Usage adds up the folder root and every folder below it down to depth
// levels (0 is root alone): each document's bytes are the sizes of the
// files its index lists, which is what the cloud keeps for it.
// includeTrashed counts the items in the trash, which the tablet shows
// under the top level. A folder reached twice through a loop counts once.
func (t *Tree) Usage(root ItemID, depth int, includeTrashed bool) Usage {
	kinds := map[Kind]*KindUsage{}
	var folders []FolderUsage
	seen := map[ItemID]bool{root: true}
	var walk func(id ItemID, level int) FolderUsage
	walk = func(id ItemID, level int) FolderUsage {
		u := FolderUsage{ID: id, Path: t.Path(id), Depth: level}
		if id == RootID {
			u.Path = "/"
		}
		for _, it := range t.Children(id, includeTrashed) {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			if it.Kind.IsFolder() {
				sub := walk(it.ID, level+1)
				u.Bytes += sub.Bytes
				u.Documents += sub.Documents
				u.Folders += sub.Folders + 1
				if sub.Depth <= depth {
					folders = append(folders, sub)
				}
				continue
			}
			u.Bytes += it.Size
			u.Documents++
			k, ok := kinds[it.Kind]
			if !ok {
				k = &KindUsage{Kind: it.Kind}
				kinds[it.Kind] = k
			}
			k.Documents++
			k.Bytes += it.Size
		}
		return u
	}
	out := Usage{Total: walk(root, 0)}
	slices.SortFunc(folders, func(a, b FolderUsage) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(a.Path, b.Path))
	})
	out.Folders = folders
	for _, k := range kinds {
		out.Kinds = append(out.Kinds, *k)
	}
	slices.SortFunc(out.Kinds, func(a, b KindUsage) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(a.Kind, b.Kind))
	})
	return out
}

// minDuplicates is how many documents make a group of duplicates.
const minDuplicates = 2

// DuplicateGroup are documents whose original — the PDF or EPUB they were
// made from — is the same file, by its hash.
type DuplicateGroup struct {
	Hash string
	// Size is the original's size; each copy past the first takes it again.
	Size  int64
	Items []Item
}

// Wasted is what the copies past the first take.
func (g DuplicateGroup) Wasted() int64 { return g.Size * int64(len(g.Items)-1) }

// Duplicates are the documents below root that share their original with
// another document below root, grouped, the groups that waste the most
// first and each group's documents by path. Notebooks have no original and
// are never duplicates: two notebooks with the same pages are two notes.
// includeTrashed counts the documents in the trash.
func (t *Tree) Duplicates(root ItemID, includeTrashed bool) []DuplicateGroup {
	byHash := map[string]*DuplicateGroup{}
	for _, it := range t.below(root, includeTrashed) {
		f, ok := it.Original()
		if !ok || f.Hash == "" {
			continue
		}
		g, ok := byHash[f.Hash]
		if !ok {
			g = &DuplicateGroup{Hash: f.Hash, Size: f.Size}
			byHash[f.Hash] = g
		}
		g.Items = append(g.Items, it)
	}
	var out []DuplicateGroup
	for _, g := range byHash {
		if len(g.Items) < minDuplicates {
			continue
		}
		slices.SortFunc(g.Items, func(a, b Item) int { return cmp.Or(cmp.Compare(t.Path(a.ID), t.Path(b.ID)), cmp.Compare(a.ID, b.ID)) })
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b DuplicateGroup) int {
		return cmp.Or(cmp.Compare(b.Wasted(), a.Wasted()), cmp.Compare(a.Hash, b.Hash))
	})
	return out
}

// below are the items below root, each once, with the trash or without.
func (t *Tree) below(root ItemID, includeTrashed bool) []Item {
	var out []Item
	seen := map[ItemID]bool{root: true}
	queue := []ItemID{root}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, it := range t.Children(next, includeTrashed) {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			out = append(out, it)
			queue = append(queue, it.ID)
		}
	}
	return out
}
