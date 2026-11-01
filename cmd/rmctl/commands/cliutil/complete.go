// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/internal/domain"
)

// CompleteRemote completes library paths and identifiers. A word that starts
// with "/" completes a path one folder at a time; any other word completes
// an identifier. foldersOnly offers only folders, for a destination. It is
// best effort: without a library it offers nothing and says nothing.
func CompleteRemote(env *Env, foldersOnly bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
		svc, err := env.Services(cmd)
		if err != nil || svc.Browser == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		tree, err := svc.Browser.Tree(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return Candidates(tree, word, foldersOnly)
	}
}

// Candidates are the completions of word in tree.
func Candidates(tree *domain.Tree, word string, foldersOnly bool) ([]cobra.Completion, cobra.ShellCompDirective) {
	if !strings.HasPrefix(word, "/") {
		var out []cobra.Completion
		for _, it := range tree.Items() {
			if tree.IsTrashed(it.ID) || (foldersOnly && !it.Kind.IsFolder()) {
				continue
			}
			if strings.HasPrefix(string(it.ID), word) {
				out = append(out, cobra.CompletionWithDesc(string(it.ID), it.Name))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	dir, base := word[:strings.LastIndex(word, "/")+1], word[strings.LastIndex(word, "/")+1:]
	parent, err := tree.ResolveFolder(strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []cobra.Completion
	for _, it := range tree.Children(parent, false) {
		if foldersOnly && !it.Kind.IsFolder() {
			continue
		}
		if !strings.HasPrefix(it.Name, base) {
			continue
		}
		name := dir + it.Name
		if it.Kind.IsFolder() {
			name += "/"
		}
		out = append(out, name)
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}
