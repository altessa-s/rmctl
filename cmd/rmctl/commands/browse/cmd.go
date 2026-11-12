// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of the reading commands.
const GroupID = cliutil.GroupRead

// Commands are ls, tree, stat, find, text, highlights, du and dupes.
func Commands(env *cliutil.Env) []*cobra.Command {
	// They change nothing and can run from the cache alone; text and
	// highlights need the pages, which only a local archive or a page the
	// cache happens to hold give them offline.
	cmds := []*cobra.Command{newLs(env), newTree(env), newStat(env), newFind(env), newText(env), newHighlights(env), newDu(env), newDupes(env)}
	for _, c := range cmds {
		cliutil.WithAccess(c, cliutil.AccessOffline)
	}
	return cmds
}

func browser(cmd *cobra.Command, env *cliutil.Env) (port.Browser, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Browser { return s.Browser })
}

func newLs(env *cliutil.Env) *cobra.Command {
	var long, all bool
	cmd := &cobra.Command{
		Use:     "ls [path]",
		GroupID: GroupID,
		Short:   "List a folder",
		Long: heredoc.Doc(`
			Lists the folder a path or an identifier names, the top level by default:
			folders first, then documents, by name. A document is listed on its own.
			-l adds the modification time and the size; --all includes the trash.
		`),
		Example: heredoc.Doc(`
			rmctl ls
			rmctl ls -l /Books
			rmctl ls --all --json /
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, false)),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := browser(cmd, env)
			if err != nil {
				return err
			}
			query := "/"
			if len(args) == 1 {
				query = args[0]
			}
			tree, items, err := b.List(cmd.Context(), query, all)
			if err != nil {
				return fmt.Errorf("ls: %w", err)
			}
			return env.Emit(cmd, cliutil.Items(tree, items, long))
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "add the modification time and the size")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include the trash")
	return cmd
}

func newTree(env *cliutil.Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "tree [path]",
		GroupID: GroupID,
		Short:   "Show the library as a tree",
		Long: heredoc.Doc(`
			Shows the library below a folder, the top level by default, as an indented
			tree: each item with its kind and identifier, the items of a folder below
			it.

			A broken account is described rather than refused: an item whose parent
			is missing is shown at the top, and an identifier that repeats keeps its
			first item. Both are counted on standard error.
		`),
		Example: heredoc.Doc(`
			rmctl tree
			rmctl tree /Books --all
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, true)),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := browser(cmd, env)
			if err != nil {
				return err
			}
			tree, err := b.Tree(cmd.Context())
			if err != nil {
				return fmt.Errorf("tree: %w", err)
			}
			root := domain.RootID
			if len(args) == 1 {
				if root, err = tree.ResolveFolder(args[0]); err != nil {
					return fmt.Errorf("tree: %s: %w", args[0], err)
				}
			}
			return PrintTree(cmd, env, tree, root, all)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include the trash")
	return cmd
}

// PrintTree writes the tree below root, and its oddities as notes on the
// error stream.
func PrintTree(cmd *cobra.Command, env *cliutil.Env, tree *domain.Tree, root domain.ItemID, all bool) error {
	nodes := cliutil.BuildTree(tree, root, all)
	errOut := cmd.ErrOrStderr()
	if nodes.Repeated > 0 {
		cliutil.Notef(errOut, "%d items repeat an identifier — the first of each is shown", nodes.Repeated)
	}
	if nodes.Stranded > 0 {
		cliutil.Notef(errOut, "%d items name a parent that leads nowhere — shown at the top", nodes.Stranded)
	}
	return env.Emit(cmd, output.Result{JSON: cliutil.TreeJSON(tree, root, all), Doc: output.Doc{output.Tree{Nodes: nodes.Nodes}}})
}

func newStat(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "stat <path|id>",
		GroupID: GroupID,
		Short:   "Show what the library knows of an item",
		Long: heredoc.Doc(`
			Shows an item's identifier, path, kind, size, times, tags, state and the
			files its index lists.
		`),
		Example: heredoc.Doc(`
			rmctl stat /Books/Novel
			rmctl stat 3f2a --json
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, false)),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := browser(cmd, env)
			if err != nil {
				return err
			}
			tree, it, err := b.Stat(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("stat: %w", err)
			}
			view := statView{ItemView: cliutil.View(tree, it), Files: fileViews(it.Files)}
			return env.Emit(cmd, statResult(it, view))
		},
	}
}

// statView is stat's --json answer.
type statView struct {
	cliutil.ItemView
	Files []fileView `json:"files"`
}

type fileView struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func fileViews(files []domain.File) []fileView {
	out := make([]fileView, 0, len(files))
	for _, f := range files {
		out = append(out, fileView{Name: f.Name, Hash: f.Hash, Size: f.Size})
	}
	return out
}

// statResult is the item as a record, then its files as a table.
func statResult(it domain.Item, v statView) output.Result {
	name := cliutil.NameCell(it, v.Trashed)
	name.Text = v.Name
	tags := output.Text(strings.Join(v.Tags, ","))
	rec := output.Record{Fields: []output.Field{
		{Key: "id", Value: output.ID(v.ID)},
		{Key: "name", Value: name},
		{Key: "path", Value: output.Path(v.Path)},
		{Key: keyKind, Value: output.Text(v.Kind)},
		{Key: "size", Value: output.Size(v.Size)},
		{Key: "modified", Value: output.Time(it.Modified)},
		{Key: "created", Value: output.Time(it.Created)},
		{Key: "trashed", Value: output.Bool(v.Trashed)},
		{Key: "pinned", Value: output.Bool(v.Pinned)},
		{Key: "tags", Value: tags},
		{Key: keyHash, Value: output.ID(v.Hash)},
	}}
	files := output.Table{Columns: []output.Column{
		{Title: "file", Role: output.RoleName}, {Title: "size", Role: output.RoleSize}, {Title: keyHash, Role: output.RoleHash},
	}}
	for _, f := range v.Files {
		files.Rows = append(files.Rows, []output.Cell{output.Text(f.Name), output.Size(f.Size), output.ID(f.Hash)})
	}
	return output.Result{JSON: v, Doc: output.Doc{rec, files}}
}

func newFind(env *cliutil.Env) *cobra.Command {
	var q port.FindQuery
	cmd := &cobra.Command{
		Use:     "find [pattern]",
		GroupID: GroupID,
		Short:   "Find items by path, tag, star or text",
		Long: heredoc.Doc(`
			Finds the items whose path matches a regular expression, and that carry
			every --tag given, are starred with --starred, and lie below --under. With
			no pattern every item matches. --text finds the documents whose typed text
			or highlighted words hold the words given, ignoring case; it reads the
			pages, so it is slower than the other filters, which it runs after. Each
			answer is a path, a kind and an identifier.
		`),
		Example: heredoc.Doc(`
			rmctl find 'invoice'
			rmctl find --tag work --starred
			rmctl find '\.pdf$' --under /Papers --json
			rmctl find --text "quarterly report" --under /Notes
		`),
		Args:         cliutil.Args(cobra.MaximumNArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := browser(cmd, env)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				q.Pattern = args[0]
			}
			tree, items, err := b.Find(cmd.Context(), q)
			if err != nil {
				if q.Text != "" {
					return cliutil.PagesErr("find", err)
				}
				return fmt.Errorf("find: %w", err)
			}
			return env.Emit(cmd, findResult(tree, items))
		},
	}
	cmd.Flags().StringArrayVar(&q.Tags, "tag", nil, "carries this tag (repeat for several)")
	cmd.Flags().BoolVar(&q.Starred, "starred", false, "is starred")
	cmd.Flags().StringVar(&q.Under, "under", "", "lies below this folder")
	cmd.Flags().BoolVarP(&q.All, "all", "a", false, "include the trash")
	cmd.Flags().StringVar(&q.Text, "text", "", "typed or highlighted text holds this, ignoring case")
	cmd.ValidArgsFunction = cobra.NoFileCompletions
	cliutil.CompleteFlags(cmd, map[string]cobra.CompletionFunc{"under": cliutil.CompleteRemote(env, true)})
	return cmd
}

// findResult is the items found as paths, kinds and identifiers.
func findResult(tree *domain.Tree, items []domain.Item) output.Result {
	t := output.Table{Columns: []output.Column{
		{Title: "path", Role: output.RolePath},
		{Title: keyKind, Role: output.RoleKind},
		{Title: "id", Role: output.RoleID},
		{Title: "pinned", Detail: true},
		{Title: "trashed", Detail: true},
	}}
	for _, it := range items {
		trashed := tree.IsTrashed(it.ID)
		path := cliutil.NameCell(it, trashed)
		path.Kind, path.Text = output.KindPath, tree.Path(it.ID)
		t.Rows = append(t.Rows, []output.Cell{path, output.Text(string(it.Kind)), output.ID(string(it.ID)), output.Bool(it.Pinned), output.Bool(trashed)})
	}
	return output.Result{JSON: cliutil.Views(tree, items), Doc: output.Doc{t}}
}
