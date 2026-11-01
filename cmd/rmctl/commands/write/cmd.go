// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of the writing commands.
const GroupID = "write"

// withDest is the argument count of a command given its destination too.
const withDest = 2

// Commands are mkdir, put, mv, trash, restore, rm, mput and unmark.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{
		newMkdir(env), newPut(env), newMv(env),
		newBatch(env, trashSpec()), newBatch(env, restoreSpec()), newRm(env),
		newMput(env), newUnmark(env),
	}
}

func writer(cmd *cobra.Command, env *cliutil.Env) (port.Writer, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Writer { return s.Writer })
}

// printItems writes the items a change touched.
func printItems(cmd *cobra.Command, env *cliutil.Env, items ...domain.Item) error {
	return env.Emit(cmd, cliutil.Items(nil, items, false))
}

func newMkdir(env *cliutil.Env) *cobra.Command {
	var parents bool
	cmd := &cobra.Command{
		Use:     "mkdir <path>",
		GroupID: GroupID,
		Short:   "Create a folder",
		Long: heredoc.Doc(`
			Creates a folder at a path. -p creates the missing folders above it as
			well, and is quiet when the folder is already there.
		`),
		Example: heredoc.Doc(`
			rmctl mkdir /Papers --yes
			rmctl mkdir -p /Papers/2026/Q1 --yes
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, true),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Create the folder %s?", args[0])); err != nil {
				return err
			}
			it, err := w.Mkdir(cmd.Context(), args[0], parents)
			if err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			return printItems(cmd, env, it)
		},
	}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "create missing folders above it")
	return cmd
}

func newMv(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "mv <src> <dst>",
		GroupID: GroupID,
		Short:   "Move or rename an item",
		Long: heredoc.Doc(`
			Moves an item into the folder dst names. When dst names no item but its
			parent folder exists, the item moves there under dst's last name, which
			renames it in place when the folder is the same. A folder cannot move into
			itself; an item in the trash is restored first.
		`),
		Example: heredoc.Doc(`
			rmctl mv /Inbox/paper /Papers --yes
			rmctl mv /Papers/paper "/Papers/Paper 2026" --yes
		`),
		Args:              cliutil.Args(cobra.ExactArgs(withDest)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Move %s to %s?", args[0], args[1])); err != nil {
				return err
			}
			it, err := w.Move(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("mv: %w", err)
			}
			return printItems(cmd, env, it)
		},
	}
}

// batchSpec describes a command that applies one change to many items.
type batchSpec struct {
	use, short, long, example, verb string
	run                             func(w port.Writer, ctx context.Context, queries []string) ([]domain.Item, error)
	// flags declares the command's own flags, if any.
	flags func(cmd *cobra.Command)
}

func trashSpec() batchSpec {
	return batchSpec{
		use:   "trash <path|id>...",
		short: "Move items to the trash",
		long: heredoc.Doc(`
		Moves items to the trash, where the tablet can restore them. A folder goes
		with everything in it. An item already in the trash is left as it is.
	`),
		example: "rmctl trash /Inbox/old --yes",
		verb:    "Move %s to the trash?",
		run: func(w port.Writer, ctx context.Context, q []string) ([]domain.Item, error) {
			return w.Trash(ctx, q)
		},
	}
}

func restoreSpec() batchSpec {
	var dest string
	return batchSpec{
		use:   "restore <path|id>... [--to <folder>]",
		short: "Restore items from the trash",
		long: heredoc.Doc(`
		Restores items from the trash into a folder, the top level of the library
		by default. The folder must exist outside the trash. Name items in the
		trash by identifier, or by path with ls --all.
	`),
		example: "rmctl restore 3f2a --yes\nrmctl restore 3f2a 9c1d --to /Papers --yes",
		verb:    "Restore %s from the trash?",
		run: func(w port.Writer, ctx context.Context, q []string) ([]domain.Item, error) {
			return w.Restore(ctx, q, dest)
		},
		flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&dest, "to", "", "the folder to restore into (the top level by default)")
		},
	}
}

func newBatch(env *cliutil.Env, spec batchSpec) *cobra.Command {
	name := strings.Fields(spec.use)[0]
	cmd := &cobra.Command{
		Use:               spec.use,
		GroupID:           GroupID,
		Short:             spec.short,
		Long:              spec.long,
		Example:           spec.example + "\n",
		Args:              cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf(spec.verb, describe(args))); err != nil {
				return err
			}
			items, err := spec.run(w, cmd.Context(), args)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			return printItems(cmd, env, items...)
		},
	}
	if spec.flags != nil {
		spec.flags(cmd)
	}
	return cmd
}

func describe(queries []string) string {
	if len(queries) == 1 {
		return queries[0]
	}
	return fmt.Sprintf("%d items", len(queries))
}

func newRm(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <path|id>...",
		GroupID: GroupID,
		Short:   "Delete items for good",
		Long: heredoc.Doc(`
			Deletes items from the library for good, a folder with everything below
			it. Unlike trash this cannot be undone from the tablet. The question names
			how many items go, descendants included.
		`),
		Example: heredoc.Doc(`
			rmctl rm /Inbox/old
			rmctl rm 3f2a 9c1d --yes
		`),
		Args:              cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Delete %s for good?", describe(args))
			if n, ok := countWithDescendants(cmd, env, args); ok {
				question = fmt.Sprintf("Delete %s for good — %d items with everything below them? This cannot be undone.",
					describe(args), n)
			}
			if err = cliutil.Confirm(cmd, env, question); err != nil {
				return err
			}
			items, err := w.Remove(cmd.Context(), args)
			if err != nil {
				return fmt.Errorf("rm: %w", err)
			}
			return printItems(cmd, env, items...)
		},
	}
}

// countWithDescendants counts the items rm would delete. It is for the
// question only: when the library cannot be read the question goes without.
func countWithDescendants(cmd *cobra.Command, env *cliutil.Env, queries []string) (int, bool) {
	svc, err := env.Services(cmd)
	if err != nil || svc.Browser == nil {
		return 0, false
	}
	tree, err := svc.Browser.Tree(cmd.Context())
	if err != nil {
		return 0, false
	}
	seen := map[domain.ItemID]bool{}
	for _, q := range queries {
		it, found, err := tree.Resolve(q, true)
		if err != nil || !found {
			return 0, false
		}
		seen[it.ID] = true
		for _, d := range tree.Descendants(it.ID) {
			seen[d.ID] = true
		}
	}
	return len(seen), true
}

func newMput(env *cliutil.Env) *cobra.Command {
	var opts port.UploadOptions
	cmd := &cobra.Command{
		Use:     "mput <dir> [folder]",
		GroupID: GroupID,
		Short:   "Upload a directory's PDFs and EPUBs, keeping the tree",
		Long: heredoc.Doc(`
			Uploads every PDF and EPUB below a local directory into a folder, the top
			level by default, creating the subfolders the directory has. With
			--incremental a document already there under the same name is skipped.
		`),
		Example: heredoc.Doc(`
			rmctl mput ~/Papers /Papers --yes
			rmctl mput ./books --jobs 4 --incremental --yes
		`),
		Args: cliutil.Args(cobra.RangeArgs(1, withDest)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
			return cliutil.CompleteRemote(env, true)(cmd, args, word)
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writer(cmd, env)
			if err != nil {
				return err
			}
			dest := "/"
			if len(args) == withDest {
				dest = args[1]
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Upload the documents below %s into %s?", args[0], dest)); err != nil {
				return err
			}
			report, err := w.Upload(cmd.Context(), args[0], dest, opts)
			if err != nil {
				return fmt.Errorf("mput: %w", err)
			}
			return env.Emit(cmd, cliutil.Report(report))
		},
	}
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 0, "transfers at a time (0: the default)")
	cmd.Flags().BoolVar(&opts.Incremental, "incremental", false, "skip documents the folder already holds by name")
	return cmd
}
