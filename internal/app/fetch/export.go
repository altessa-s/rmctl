// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"errors"
	"fmt"

	"github.com/altessa-s/rmctl/internal/domain"
)

// ErrRepeatedEntry means two files would take one name in an archive.
var ErrRepeatedEntry = errors.New("repeated archive entry")

// ErrNothingToExport means the queries name no document.
var ErrNothingToExport = errors.New("nothing to export: no document")

// defaultExportName names the archive of several queries.
const defaultExportName = "rmctl-export"

// ExportPlan is the documents queries name, each once, in the order the
// queries name them, and the name of the archive they go into. A document
// is itself, wherever it is. A folder, or "/" for the top level, stands for
// every document below it that is not in the trash on its own; a folder in
// the trash that is named itself keeps its documents. Folders hold no files
// an archive carries: the tablet imports a document per archive.
func ExportPlan(tree *domain.Tree, queries []string) ([]domain.Item, string, error) {
	if len(queries) == 0 {
		return nil, "", domain.ErrEmptyName
	}
	var docs []domain.Item
	seen := map[domain.ItemID]bool{}
	add := func(it domain.Item) {
		if it.Kind.IsFolder() || seen[it.ID] {
			return
		}
		seen[it.ID] = true
		docs = append(docs, it)
	}
	name := defaultExportName
	for _, q := range queries {
		it, found, err := tree.Resolve(q, true)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q, err)
		}
		folder := domain.RootID
		switch {
		case found && !it.Kind.IsFolder():
			add(it)
		case found:
			folder = it.ID
			fallthrough
		default:
			named := tree.IsTrashed(folder)
			for _, d := range below(tree, folder) {
				if named || !tree.IsTrashed(d.ID) {
					add(d)
				}
			}
		}
		if len(queries) == 1 && found {
			name = it.Name
		}
	}
	if len(docs) == 0 {
		return nil, "", ErrNothingToExport
	}
	return docs, name, nil
}

// below is every item under folder, the top level included for RootID.
func below(tree *domain.Tree, folder domain.ItemID) []domain.Item {
	if folder != domain.RootID {
		return tree.Descendants(folder)
	}
	var out []domain.Item
	for _, top := range tree.Children(domain.RootID, true) {
		out = append(out, top)
		out = append(out, tree.Descendants(top.ID)...)
	}
	return out
}
