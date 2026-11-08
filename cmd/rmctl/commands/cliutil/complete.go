// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/internal/domain"
)

// Trash is which items in the trash a completion offers.
type Trash int

// The ways of offering the trash.
const (
	// TrashWithAll offers the trash when the command's --all is on, and
	// leaves it out otherwise: what the command itself shows.
	TrashWithAll Trash = iota
	// TrashToo offers the items in the trash with the others.
	TrashToo
	// TrashOnly offers the items in the trash alone, as restore takes them.
	TrashOnly
	// trashLeftOut leaves the trash out: TrashWithAll without --all.
	trashLeftOut
)

// Completing says what a completion offers.
type Completing struct {
	// Folders offers folders alone, for a destination.
	Folders bool
	Trash   Trash
}

// FlagAll is the flag of the commands that take the trash in.
const FlagAll = "all"

// CompleteRemote completes library paths and identifiers, the trash only
// with --all; foldersOnly offers only folders, for a destination. See
// [Complete].
func CompleteRemote(env *Env, foldersOnly bool) cobra.CompletionFunc {
	return Complete(env, Completing{Folders: foldersOnly})
}

// Complete completes library paths and identifiers from the listing the
// profile's cache keeps (see [Env.Listing]): never the cloud, so a Tab
// costs one read of one file. A word that starts with "/" completes a path
// one folder at a time, and an empty word starts one at the top; any other
// word completes an identifier, and a path from the top without its first
// "/", as the commands read one. Without a listing it offers nothing and
// says nothing.
func Complete(env *Env, c Completing) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
		tree := env.Listing(cmd)
		if tree == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if c.Trash == TrashWithAll {
			c.Trash = trashLeftOut
			if all, err := cmd.Flags().GetBool(FlagAll); err == nil && all {
				c.Trash = TrashToo
			}
		}
		return Candidates(tree, word, c)
	}
}

// OrFiles completes with complete, and when it offers nothing, with the
// local files of the extensions exts, or any file without exts: for an
// argument that is a library path or a local file.
func OrFiles(complete cobra.CompletionFunc, exts ...string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if out, dir := complete(cmd, args, word); len(out) > 0 {
			return out, dir
		}
		if len(exts) == 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		return exts, cobra.ShellCompDirectiveFilterFileExt
	}
}

// UpTo completes the first n arguments with complete, and nothing after.
func UpTo(n int, complete cobra.CompletionFunc) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) >= n {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return complete(cmd, args, word)
	}
}

// CompleteNothing offers nothing, for [OrFiles] over local files alone.
func CompleteNothing(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// CompleteDirs completes local directories.
func CompleteDirs(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

// LocalThenRemote completes the first argument with local, then the n-1
// next with library folders, and nothing after: a local source and the
// folder it goes to, or the reverse.
func LocalThenRemote(env *Env, n int, local cobra.CompletionFunc) cobra.CompletionFunc {
	remote := UpTo(n, CompleteRemote(env, true))
	return func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return local(cmd, args, word)
		}
		return remote(cmd, args, word)
	}
}

// CompleteFlags completes each flag of cmd named in flags with its
// function. A flag cmd does not have is a programming error and panics.
func CompleteFlags(cmd *cobra.Command, flags map[string]cobra.CompletionFunc) {
	for name, complete := range flags {
		if err := cmd.RegisterFlagCompletionFunc(name, complete); err != nil {
			panic(err)
		}
	}
}

// Candidates are the completions of word in tree.
func Candidates(tree *domain.Tree, word string, c Completing) ([]cobra.Completion, cobra.ShellCompDirective) {
	if c.Trash == TrashWithAll {
		c.Trash = trashLeftOut
	}
	var out []cobra.Completion
	if word != "" && !strings.HasPrefix(word, "/") {
		out = identifiers(tree, word, c)
	}
	if word == "" {
		word = "/"
	}
	out = append(out, paths(tree, word, c)...)
	directive := cobra.ShellCompDirectiveNoFileComp
	for _, o := range out {
		if value, _, _ := strings.Cut(o, "\t"); strings.HasSuffix(value, "/") {
			directive |= cobra.ShellCompDirectiveNoSpace
			break
		}
	}
	return out, directive
}

// identifiers are the items whose identifier starts with word, each
// described by its path.
func identifiers(tree *domain.Tree, word string, c Completing) []cobra.Completion {
	var out []cobra.Completion
	prefix := strings.ToLower(word)
	for _, it := range tree.Items() {
		if strings.HasPrefix(string(it.ID), prefix) && offered(tree, it, c) {
			out = append(out, cobra.CompletionWithDesc(string(it.ID), tree.Path(it.ID)))
		}
	}
	return out
}

// paths are the children of the folder before word's last "/" whose names
// start with the rest of word, ignoring case; a folder ends with "/". A
// name with a "/" in it cannot be part of a path and is left out. In the
// trash alone, a folder in the trash leads to what it holds.
func paths(tree *domain.Tree, word string, c Completing) []cobra.Completion {
	cut := strings.LastIndex(word, "/") + 1
	dir, base := word[:cut], strings.ToLower(word[cut:])
	withTrash := c.Trash != trashLeftOut
	parent := domain.RootID
	if folder := strings.Trim(dir, "/"); folder != "" {
		it, found, err := tree.Resolve("/"+folder, withTrash)
		if err != nil || !found || !it.Kind.IsFolder() {
			return nil
		}
		parent = it.ID
	}
	var out []cobra.Completion
	for _, it := range tree.Children(parent, withTrash) {
		if it.Name == "" || strings.Contains(it.Name, "/") || !strings.HasPrefix(strings.ToLower(it.Name), base) || !offered(tree, it, c) {
			continue
		}
		name := dir + it.Name
		if it.Kind.IsFolder() {
			name += "/"
		}
		out = append(out, cobra.CompletionWithDesc(name, describe(tree, it)))
	}
	return out
}

// offered reports whether c offers it.
func offered(tree *domain.Tree, it domain.Item, c Completing) bool {
	if c.Folders && !it.Kind.IsFolder() {
		return false
	}
	switch c.Trash {
	case trashLeftOut:
		return !tree.IsTrashed(it.ID)
	case TrashOnly:
		return tree.IsTrashed(it.ID)
	case TrashWithAll, TrashToo:
	}
	return true
}

// describe is what a path completion says of it: its kind, and the trash.
func describe(tree *domain.Tree, it domain.Item) string {
	if tree.IsTrashed(it.ID) {
		return string(it.Kind) + ", in the trash"
	}
	return string(it.Kind)
}
