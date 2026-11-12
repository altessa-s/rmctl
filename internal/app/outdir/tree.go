// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
)

// TopDir names the directory the whole library goes into without one.
const TopDir = "remarkable"

// Folder reports whether query names a folder ("" and "/" are the top
// level) rather than a document. Items in the trash count.
func Folder(ctx context.Context, lib *library.Library, query string) (bool, error) {
	if query == "" || query == "/" {
		return true, nil
	}
	tree, _, err := lib.Load(ctx)
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

// Top is the folder query names and the directory name it goes into by
// default: its safe name, or TopDir for the top level. A document is
// notFolder.
func Top(tree *domain.Tree, query string, notFolder error) (domain.ItemID, string, error) {
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
		return "", "", fmt.Errorf("%s: %w", it.Name, notFolder)
	}
	return it.ID, domain.SafeName(it.Name), nil
}

// Planned is one document of a folder laid out and the file it becomes.
type Planned struct {
	Document domain.Item
	// LibraryPath is the document's path in the library, Path its file's.
	LibraryPath, Path string
}

// Plan is the documents below the folder top of tree, each with the file
// it becomes under dir, [domain.DerivedFileName] with ext, in library path
// order. Every folder below top is a directory under dir, named by its
// safe name. Documents and folders in the trash stay out unless all, or
// unless top itself is in the trash. Two names that collide in one
// directory, compared without case, are told apart by identifier: in
// order of creation, then of identifier, the first keeps its name and the
// later ones get " (<first 8 characters of the identifier>)" before the
// extension, or at the end of a folder's name.
func Plan(tree *domain.Tree, top domain.ItemID, dir string, all bool, ext string) []Planned {
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
			name, suffix := domain.DerivedFileName(it.Name, ext), ext
			if it.Kind.IsFolder() {
				name, suffix = domain.SafeName(it.Name), ""
			}
			if taken[strings.ToLower(name)] {
				name = fetch.Disambiguated(name, it.ID, suffix)
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

// Each calls do for the indexes 0 to n-1, jobs at a time, and returns what
// it answered for those it finished, in index order: all of them, or, when
// do fails or ctx ends, those done by then, with the error.
func Each[T any](ctx context.Context, n, jobs int, do func(ctx context.Context, i int) (T, error)) ([]T, error) {
	out := make([]T, n)
	done := make([]bool, n)
	indexes := make([]int, n)
	for i := range indexes {
		indexes[i] = i
	}
	err := concurrency.Process(ctx, indexes, func(ctx context.Context, i int) error {
		v, err := do(ctx, i)
		if err != nil {
			return err
		}
		out[i], done[i] = v, true
		return nil
	}, concurrency.WithConcurrency[int](max(jobs, 1)), concurrency.WithStopOnError[int]())
	finished := out[:0]
	for i, v := range out {
		if done[i] {
			finished = append(finished, v)
		}
	}
	return finished, err
}
