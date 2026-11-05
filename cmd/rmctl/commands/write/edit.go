// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// columnName titles the name column of the write commands' answers.
const columnName = "name"

// editedView is one item an edit touched, as --json prints it.
type editedView struct {
	Action  string           `json:"action"`
	Changed bool             `json:"changed"`
	Item    cliutil.ItemView `json:"item"`
}

// printEdited answers with the items an edit touched: what it did to each,
// or "unchanged" for an item that already was as asked. An unchanged item
// is noted on the error stream too.
func printEdited(cmd *cobra.Command, env *cliutil.Env, done string, edited []port.Edited) error {
	views := make([]editedView, 0, len(edited))
	t := output.Table{Columns: []output.Column{
		{Title: "action", Role: output.RoleKind},
		{Title: columnName, Role: output.RoleName},
		{Title: "kind", Role: output.RoleKind},
		{Title: "tags"},
		{Title: "id", Role: output.RoleID},
		{Title: "pinned", Detail: true},
	}}
	for _, e := range edited {
		action := done
		if !e.Changed {
			action = "unchanged"
			cliutil.Notef(cmd.ErrOrStderr(), "%s is already as asked; nothing to change", e.Item.Name)
		}
		views = append(views, editedView{Action: action, Changed: e.Changed, Item: cliutil.View(nil, e.Item)})
		t.Rows = append(t.Rows, []output.Cell{
			output.Text(action), cliutil.NameCell(e.Item, false), output.Text(string(e.Item.Kind)),
			output.Text(strings.Join(e.Item.Tags, ",")), output.ID(string(e.Item.ID)), output.Bool(e.Item.Pinned),
		})
	}
	return env.Emit(cmd, output.Result{JSON: views, Doc: output.Doc{t}})
}

func newRename(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "rename <path|id> <new name>",
		GroupID: GroupID,
		Short:   "Rename an item where it is",
		Long: heredoc.Doc(`
			Gives an item a new name in its folder. Only the name changes: the
			folder, the files and every other setting stay, so nothing is uploaded
			but the .metadata. mv moves an item, and renames it on the way when its
			destination names no folder. A name cannot hold a slash. Renaming to the
			name it has changes nothing.
		`),
		Example: heredoc.Doc(`
			rmctl rename /Books/Novel "Novel (2nd ed.)" --yes
		`),
		Args:              cliutil.Args(cobra.ExactArgs(withDest)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Rename %s to %q?", args[0], args[1])); err != nil {
				return err
			}
			e, err := w.Rename(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("rename: %w", err)
			}
			return printEdited(cmd, env, "renamed", []port.Edited{e})
		},
	}
}

func newPin(env *cliutil.Env, on bool) *cobra.Command {
	use, verb, done := "pin", "Pin", "pinned"
	if !on {
		use, verb, done = "unpin", "Unpin", "unpinned"
	}
	return &cobra.Command{
		Use:     use + " <path|id>...",
		GroupID: GroupID,
		Short:   verb + " items: star them, or take the star off",
		Long: heredoc.Doc(`
			Sets the star (the "pinned" flag) of documents and folders. Only the
			.metadata changes. An item already as asked is reported and left alone,
			and when every item is, nothing is written.
		`),
		Example:           fmt.Sprintf("  rmctl %s /Books/Novel /Papers --yes\n", use),
		Args:              cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("%s %s?", verb, describe(args))); err != nil {
				return err
			}
			edited, err := w.Pin(cmd.Context(), args, on)
			if err != nil {
				return fmt.Errorf("%s: %w", use, err)
			}
			return printEdited(cmd, env, done, edited)
		},
	}
}

func newTag(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag <command>",
		GroupID: GroupID,
		Short:   "Add tags to documents, or take them off",
		Long: heredoc.Doc(`
			Tags documents without uploading them again: only the .content's tags
			change, with the .metadata marked edited; every other field stays as it
			is. The documents are arguments and the tags are --tag, repeated, so a
			tag never reads as a path. Folders take no tags.
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTagEdit(env, true), newTagEdit(env, false))
	return cmd
}

func newTagEdit(env *cliutil.Env, add bool) *cobra.Command {
	var tags []string
	use, short, done, question := "add", "Add tags to documents", "tagged", "Tag %s with %s?"
	if !add {
		use, short, done, question = "rm", "Take tags off documents", "untagged", "Take %[2]s off %[1]s?"
	}
	cmd := &cobra.Command{
		Use:   use + " <path|id>... --tag <tag>...",
		Short: short,
		Long: heredoc.Doc(`
			A document that already has (or lacks) every tag given is reported and
			left alone, and when every document is, nothing is written.
		`),
		Example:           fmt.Sprintf("  rmctl tag %s /Papers/Paper /Books/Novel --tag work --tag 2026 --yes\n", use),
		Args:              cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(tags) == 0 {
				return cliutil.Usagef("%s: name the tags with --tag", cmd.CommandPath())
			}
			for _, t := range tags {
				if strings.TrimSpace(t) == "" {
					return cliutil.Usagef("%s: a tag is not empty", cmd.CommandPath())
				}
			}
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf(question, describe(args), strings.Join(tags, ", "))); err != nil {
				return err
			}
			var edited []port.Edited
			if add {
				edited, err = w.Tag(cmd.Context(), args, tags, nil)
			} else {
				edited, err = w.Tag(cmd.Context(), args, nil, tags)
			}
			if err != nil {
				return fmt.Errorf("tag %s: %w", use, err)
			}
			return printEdited(cmd, env, done, edited)
		},
	}
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "a tag (repeat for several)")
	return cmd
}
