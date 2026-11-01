// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/domain"
)

// TreeLines is the library below root as indented lines, with what was odd
// about it counted: a debugging client is pointed at broken accounts, and
// each oddity would otherwise end the command rather than describe the tree.
type TreeLines struct {
	Lines []string
	// Stranded counts items whose parent is missing or that sit in a parent
	// loop; they are shown at the top level.
	Stranded int
	// Repeated counts items dropped because their identifier repeats.
	Repeated int
}

// TreeNode is one line of a tree for --json.
type TreeNode struct {
	ItemView
	Children []TreeNode `json:"children,omitempty"`
}

// RenderTree lays out the items below root. includeTrashed keeps the trash.
func RenderTree(tree *domain.Tree, root domain.ItemID, includeTrashed bool) TreeLines {
	out := TreeLines{Repeated: tree.Repeated}
	seen := map[domain.ItemID]bool{}
	var walk func(parent domain.ItemID, depth int)
	walk = func(parent domain.ItemID, depth int) {
		for _, it := range tree.Children(parent, includeTrashed) {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			if depth == 0 && tree.Stranded(it) {
				out.Stranded++
			}
			out.Lines = append(out.Lines, strings.Repeat("  ", depth)+Line(it, tree.IsTrashed(it.ID)))
			walk(it.ID, depth+1)
		}
	}
	walk(root, 0)
	if root != domain.RootID {
		return out
	}
	// Items in a parent loop are reachable from nowhere: show each at the top.
	for _, it := range tree.Items() {
		if seen[it.ID] || (!includeTrashed && tree.IsTrashed(it.ID)) {
			continue
		}
		seen[it.ID] = true
		out.Stranded++
		out.Lines = append(out.Lines, Line(it, tree.IsTrashed(it.ID)))
		walk(it.ID, 1)
	}
	return out
}

// TreeJSON is the items below root as nested nodes.
func TreeJSON(tree *domain.Tree, root domain.ItemID, includeTrashed bool) []TreeNode {
	seen := map[domain.ItemID]bool{}
	var walk func(parent domain.ItemID) []TreeNode
	walk = func(parent domain.ItemID) []TreeNode {
		var nodes []TreeNode
		for _, it := range tree.Children(parent, includeTrashed) {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			nodes = append(nodes, TreeNode{ItemView: View(tree, it), Children: walk(it.ID)})
		}
		return nodes
	}
	nodes := walk(root)
	if root == domain.RootID {
		for _, it := range tree.Items() {
			if seen[it.ID] || (!includeTrashed && tree.IsTrashed(it.ID)) {
				continue
			}
			seen[it.ID] = true
			nodes = append(nodes, TreeNode{ItemView: View(tree, it), Children: walk(it.ID)})
		}
	}
	if nodes == nil {
		nodes = []TreeNode{}
	}
	return slices.Clip(nodes)
}
