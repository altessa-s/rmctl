// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"path/filepath"
	"strings"

	"github.com/altessa-s/rmctl/internal/domain"
)

// PlannedFile is one download of a mirror.
type PlannedFile struct {
	File domain.File
	Item domain.ItemID
	Path string
}

// MirrorPlan is what a mirror creates: directories, then files. Skipped
// counts documents with no original to download, such as notebooks.
type MirrorPlan struct {
	Dirs    []string
	Files   []PlannedFile
	Skipped int
}

// Plan maps the folder top of tree onto dir: one directory per folder, one
// file per document original. Names that collide in one directory, compared
// without case, are told apart by their identifiers. Trashed items stay out.
func Plan(tree *domain.Tree, top domain.ItemID, dir string) MirrorPlan {
	p := MirrorPlan{Dirs: []string{dir}}
	seen := map[domain.ItemID]bool{top: true}
	var walk func(folder domain.ItemID, local string)
	walk = func(folder domain.ItemID, local string) {
		children := tree.Children(folder, false)
		names := make([]string, len(children))
		exts := make([]string, len(children))
		count := map[string]int{}
		for i, it := range children {
			if it.Kind.IsFolder() {
				names[i] = SafeName(it.Name)
			} else if f, ok := it.Original(); ok {
				exts[i] = f.Extension()
				names[i] = FileName(it.Name, exts[i])
			}
			count[strings.ToLower(names[i])]++
		}
		for i, it := range children {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			name := names[i]
			if name == "" {
				p.Skipped++
				continue
			}
			if count[strings.ToLower(name)] > 1 {
				name = Disambiguated(name, it.ID, exts[i])
			}
			path := filepath.Join(local, name)
			if it.Kind.IsFolder() {
				p.Dirs = append(p.Dirs, path)
				walk(it.ID, path)
				continue
			}
			f, _ := it.Original()
			p.Files = append(p.Files, PlannedFile{File: f, Item: it.ID, Path: path})
		}
	}
	walk(top, dir)
	return p
}

// planDocument is the plan of one document mirrored into dir.
func planDocument(it domain.Item, dir string) MirrorPlan {
	p := MirrorPlan{Dirs: []string{dir}}
	f, ok := it.Original()
	if !ok {
		p.Skipped = 1
		return p
	}
	p.Files = []PlannedFile{{File: f, Item: it.ID, Path: filepath.Join(dir, FileName(it.Name, f.Extension()))}}
	return p
}
