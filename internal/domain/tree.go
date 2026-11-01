// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"cmp"
	"slices"
	"strings"
)

// Tree is the library: the items and the links between them. A broken
// account is the case worth describing rather than refusing, so repeated
// identifiers keep their first item, and an item whose parent is missing
// shows at the top level.
type Tree struct {
	Generation int64
	items      []Item
	byID       map[ItemID]int
	children   map[ItemID][]ItemID
	// Repeated counts items dropped because their identifier repeats.
	Repeated int
}

// NewTree links items. The first item under each identifier wins.
func NewTree(generation int64, items []Item) *Tree {
	t := &Tree{
		Generation: generation,
		items:      make([]Item, 0, len(items)),
		byID:       make(map[ItemID]int, len(items)),
		children:   map[ItemID][]ItemID{},
	}
	for _, it := range items {
		if _, dup := t.byID[it.ID]; dup {
			t.Repeated++
			continue
		}
		t.byID[it.ID] = len(t.items)
		t.items = append(t.items, it)
	}
	for _, it := range t.items {
		parent := it.Parent
		if _, ok := t.byID[parent]; !ok {
			parent = RootID
		}
		t.children[parent] = append(t.children[parent], it.ID)
	}
	for id := range t.children {
		t.sort(t.children[id])
	}
	return t
}

// Len is the number of items.
func (t *Tree) Len() int { return len(t.items) }

// Items are every item, in root index order.
func (t *Tree) Items() []Item { return slices.Clone(t.items) }

// Get returns the item with id.
func (t *Tree) Get(id ItemID) (Item, bool) {
	i, ok := t.byID[id]
	if !ok {
		return Item{}, false
	}
	return t.items[i], true
}

// Stranded reports whether the item's parent is not in the library.
func (t *Tree) Stranded(it Item) bool {
	if it.Parent == RootID {
		return false
	}
	_, ok := t.byID[it.Parent]
	return !ok
}

// Children are the items under parent, folders first, then by name.
// includeTrashed keeps the items in the trash.
func (t *Tree) Children(parent ItemID, includeTrashed bool) []Item {
	var out []Item
	for _, id := range t.children[parent] {
		it := t.items[t.byID[id]]
		if !includeTrashed && t.IsTrashed(it.ID) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// IsTrashed reports an item in the trash, itself or through a trashed
// ancestor: the tablet marks only the folder. A missing ancestor or a loop
// ends the walk; a walk longer than the library has items is a loop.
func (t *Tree) IsTrashed(id ItemID) bool {
	for steps := 0; id != RootID && steps <= len(t.items); steps++ {
		i, ok := t.byID[id]
		if !ok {
			return false
		}
		if t.items[i].trashedHere {
			return true
		}
		id = t.items[i].Parent
	}
	return false
}

// Descendants are the items below id, not id itself, each once.
func (t *Tree) Descendants(id ItemID) []Item {
	var out []Item
	seen := map[ItemID]bool{id: true}
	queue := []ItemID{id}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, child := range t.children[next] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, t.items[t.byID[child]])
			queue = append(queue, child)
		}
	}
	return out
}

// IsWithin reports whether id is target or lies below it.
func (t *Tree) IsWithin(id, target ItemID) bool {
	seen := map[ItemID]bool{}
	for !seen[id] {
		if id == target {
			return true
		}
		seen[id] = true
		it, ok := t.Get(id)
		if !ok || id == RootID {
			return false
		}
		id = it.Parent
	}
	return false
}

// maxDepth bounds a path walk on a library with a parent loop.
const maxDepth = 64

// Path is the item's display path, "/A/B/name". A missing parent or a loop
// ends the walk at the top.
func (t *Tree) Path(id ItemID) string {
	var parts []string
	seen := map[ItemID]bool{}
	for depth := 0; id != RootID && !seen[id] && depth < maxDepth; depth++ {
		seen[id] = true
		it, ok := t.Get(id)
		if !ok {
			break
		}
		parts = append(parts, it.Name)
		id = it.Parent
	}
	slices.Reverse(parts)
	return "/" + strings.Join(parts, "/")
}

// Resolve finds the item a query names. A query that starts with "/" is a
// path from the top; anything else is first tried as an identifier or a
// unique identifier prefix, then as a path. "/" names the top level, which
// is not an item: Resolve returns found false and no error for it. Unless
// includeTrashed, an item in the trash — itself or through a folder — is not
// found, whichever way it is named.
func (t *Tree) Resolve(query string, includeTrashed bool) (Item, bool, error) {
	if query == "" {
		return Item{}, false, ErrEmptyName
	}
	if strings.HasPrefix(query, "/") {
		return t.resolvePath(query, includeTrashed)
	}
	if it, ok := t.Get(ItemID(query)); ok {
		if !includeTrashed && t.IsTrashed(it.ID) {
			return Item{}, false, ErrNotFound
		}
		return it, true, nil
	}
	var matches []Item
	for _, it := range t.items {
		if strings.HasPrefix(string(it.ID), query) && (includeTrashed || !t.IsTrashed(it.ID)) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], true, nil
	case 0:
		return t.resolvePath("/"+query, includeTrashed)
	default:
		return Item{}, false, &AmbiguousError{Query: query, Candidates: matches}
	}
}

// ResolveFolder finds the folder a query names; "/" and "" are the top level.
func (t *Tree) ResolveFolder(query string) (ItemID, error) {
	if query == "" || query == "/" {
		return RootID, nil
	}
	it, found, err := t.Resolve(query, false)
	if err != nil {
		return RootID, err
	}
	if !found {
		return RootID, nil
	}
	if !it.Kind.IsFolder() {
		return RootID, ErrNotAFolder
	}
	return it.ID, nil
}

func (t *Tree) resolvePath(p string, includeTrashed bool) (Item, bool, error) {
	parent := RootID
	var current Item
	found := false
	for part := range strings.SplitSeq(strings.Trim(p, "/"), "/") {
		if part == "" {
			continue
		}
		if found && !current.Kind.IsFolder() {
			return Item{}, false, ErrNotAFolder
		}
		var match Item
		var others []Item
		for _, id := range t.children[parent] {
			child := &t.items[t.byID[id]]
			if child.Name != part || (!includeTrashed && t.IsTrashed(child.ID)) {
				continue
			}
			if match.ID == "" && others == nil {
				match = *child
				continue
			}
			if others == nil {
				others = []Item{match}
			}
			others = append(others, *child)
		}
		switch {
		case others != nil:
			return Item{}, false, &AmbiguousError{Query: p, Candidates: others}
		case match.ID == "":
			return Item{}, false, ErrNotFound
		}
		current, found = match, true
		parent = current.ID
	}
	return current, found, nil
}

func (t *Tree) sort(ids []ItemID) {
	slices.SortStableFunc(ids, func(a, b ItemID) int {
		x, y := t.items[t.byID[a]], t.items[t.byID[b]]
		if x.Kind.IsFolder() != y.Kind.IsFolder() {
			if x.Kind.IsFolder() {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
			return c
		}
		return cmp.Compare(x.ID, y.ID)
	})
}
