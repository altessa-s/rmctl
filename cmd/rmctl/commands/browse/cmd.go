// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"fmt"
	"io"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of the reading commands.
const GroupID = cliutil.GroupRead

// Commands are ls, tree, stat, find, text and highlights.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{newLs(env), newTree(env), newStat(env), newFind(env), newText(env), newHighlights(env)}
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
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
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
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, cliutil.Views(tree, items))
			}
			for _, it := range items {
				line := cliutil.Line(it, tree.IsTrashed(it.ID))
				if long {
					line = cliutil.LongLine(it, tree.IsTrashed(it.ID))
				}
				if _, err := fmt.Fprintln(out, line); err != nil {
					return err
				}
			}
			return nil
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
			tree: a folder ends in "/", a document carries its kind in brackets, a
			pinned item a "*", and each line ends with the identifier.

			A broken account is described rather than refused: an item whose parent
			is missing is shown at the top, and an identifier that repeats keeps its
			first item. Both are counted on standard error.
		`),
		Example: heredoc.Doc(`
			rmctl tree
			rmctl tree /Books --all
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, true),
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
			return PrintTree(cmd.OutOrStdout(), cmd.ErrOrStderr(), tree, root, all, cliutil.JSON(cmd))
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include the trash")
	return cmd
}

// PrintTree writes the tree below root as text or JSON, and its oddities as
// notes on errOut.
func PrintTree(out, errOut io.Writer, tree *domain.Tree, root domain.ItemID, all, asJSON bool) error {
	if asJSON {
		return cliutil.WriteJSON(out, cliutil.TreeJSON(tree, root, all))
	}
	lines := cliutil.RenderTree(tree, root, all)
	if lines.Repeated > 0 {
		cliutil.Notef(errOut, "%d items repeat an identifier — the first of each is shown", lines.Repeated)
	}
	if lines.Stranded > 0 {
		cliutil.Notef(errOut, "%d items name a parent that leads nowhere — shown at the top", lines.Stranded)
	}
	for _, line := range lines.Lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
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
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
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
			out := cmd.OutOrStdout()
			view := statView{ItemView: cliutil.View(tree, it), Files: fileViews(it.Files)}
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, view)
			}
			return writeStat(out, view)
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

func writeStat(w io.Writer, v statView) error {
	rows := [][2]string{
		{"id", v.ID},
		{"name", v.Name},
		{"path", v.Path},
		{"kind", v.Kind},
		{"size", cliutil.Size(v.Size)},
		{"modified", dash(v.Modified)},
		{"created", dash(v.Created)},
		{"trashed", fmt.Sprint(v.Trashed)},
		{"pinned", fmt.Sprint(v.Pinned)},
		{"tags", tagsText(v.Tags)},
		{"hash", v.Hash},
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(w, "%-9s %s\n", r[0], r[1]); err != nil {
			return err
		}
	}
	for _, f := range v.Files {
		if _, err := fmt.Fprintf(w, "file      %s  %s  %s\n", f.Name, cliutil.Size(f.Size), f.Hash); err != nil {
			return err
		}
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func tagsText(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return fmt.Sprint(tags)
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
			answer is a path and an identifier.
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
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, cliutil.Views(tree, items))
			}
			for _, it := range items {
				if _, err := fmt.Fprintf(out, "%s  %s\n", tree.Path(it.ID), it.ID); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&q.Tags, "tag", nil, "carries this tag (repeat for several)")
	cmd.Flags().BoolVar(&q.Starred, "starred", false, "is starred")
	cmd.Flags().StringVar(&q.Under, "under", "", "lies below this folder")
	cmd.Flags().BoolVarP(&q.All, "all", "a", false, "include the trash")
	cmd.Flags().StringVar(&q.Text, "text", "", "typed or highlighted text holds this, ignoring case")
	return cmd
}
