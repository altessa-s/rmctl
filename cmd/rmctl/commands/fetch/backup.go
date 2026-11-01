// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"fmt"
	"path"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// backupView is what a backup did, for --json.
type backupView struct {
	Dir       string `json:"dir"`
	Documents int    `json:"documents"`
	Written   int    `json:"written"`
	Skipped   int    `json:"skipped"`
	Moved     int    `json:"moved"`
	Pruned    int    `json:"pruned"`
}

// backupArgs are a directory and, at most, one path.
const backupArgs = 2

func newBackup(env *cliutil.Env) *cobra.Command {
	var opts port.BackupOptions
	cmd := &cobra.Command{
		Use:     "backup <dir> [path]",
		GroupID: cliutil.GroupRead,
		Short:   "Back up documents whole into a directory, incrementally",
		Long: heredoc.Doc(`
			Writes every document below a path (the whole library by default) into a
			local directory, one .rmdoc archive per document named <id>.rmdoc, packed
			the way export packs one: pages, annotations and tags. Documents in the
			trash stay out unless --all.

			A manifest.json beside the archives records the profile, the time and,
			per document, its id, name, path, kind, record hash, archive and size.
			The next run skips every document whose record hash is unchanged and
			rewrites the others; each archive appears only when whole. An archive of
			a document the backup no longer covers (deleted, in the trash, moved out
			of the path) is moved into removed/, or deleted with --prune. A directory
			holding the backup of another profile is refused unless --force.

			"rmctl backup restore" puts a backup back. A directory that is itself
			named restore is written ./restore.
		`),
		Example: heredoc.Doc(`
			rmctl backup ~/rm-backup
			rmctl backup ~/rm-backup /Notes --jobs 8
			rmctl backup ~/rm-backup --all --prune
			rmctl backup ./restore
		`),
		Args: cliutil.Args(cobra.RangeArgs(1, backupArgs)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
			return cliutil.CompleteRemote(env, true)(cmd, args, word)
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := fetcher(cmd, env)
			if err != nil {
				return err
			}
			query := "/"
			if len(args) == backupArgs {
				query = args[1]
			}
			opts.Profile = cliutil.ProfileName(cmd)
			r, err := f.Backup(cmd.Context(), args[0], query, opts)
			if err != nil {
				return cliutil.PagesErr("backup", err)
			}
			return env.Emit(cmd, output.Fields(backupView(r),
				output.Field{Key: "dir", Value: output.Path(r.Dir)},
				output.Field{Key: "documents", Value: output.Count(r.Documents)},
				output.Field{Key: "written", Value: output.Count(r.Written)},
				output.Field{Key: "skipped", Value: output.Count(r.Skipped)},
				output.Field{Key: "moved", Value: output.Count(r.Moved)},
				output.Field{Key: "pruned", Value: output.Count(r.Pruned)}))
		},
	}
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "back up the documents in the trash too")
	cmd.Flags().BoolVar(&opts.Prune, "prune", false, "delete the archives of documents gone instead of moving them to removed/")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "back up into a directory holding another profile's backup")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 0, "documents at a time (0: the default)")
	cmd.AddCommand(newBackupRestore(env))
	return cmd
}

// restoreView is what a restore did, for --json.
type restoreView struct {
	Profile   string         `json:"profile"`
	Created   string         `json:"created,omitempty"`
	DryRun    bool           `json:"dry_run,omitzero"`
	Folders   []string       `json:"folders"`
	Documents []restoredView `json:"documents"`
}

// restoredView is one document a restore imported.
type restoredView struct {
	Item cliutil.ItemView `json:"item"`
	// From is the identifier the document had in the backup, when the
	// library held it and the document took a new one.
	From   string `json:"renamed_from,omitempty"`
	Folder string `json:"folder"`
	File   string `json:"file"`
}

func newBackupRestore(env *cliutil.Env) *cobra.Command {
	var opts port.RestoreOptions
	cmd := &cobra.Command{
		Use:   "restore <dir>",
		Short: "Put the documents of a backup back into the library",
		Long: heredoc.Doc(`
			Imports the archives of a backup directory into the library in one
			change, each into the folder its manifest names; missing folders are
			created. --to recreates those folder paths below another folder instead.
			--id restores only the documents named, each by its id or a unique
			prefix of one. A document whose id the library already holds comes back
			as a copy under a new id; the library's document is not touched.

			The archives are read before anything is uploaded. It asks first unless
			--yes; --dry-run shows what it would do and needs no --yes. To back up
			into a directory named restore, write "rmctl backup ./restore".
		`),
		Example: heredoc.Doc(`
			rmctl backup restore ~/rm-backup --dry-run
			rmctl backup restore ~/rm-backup --to /Restored --yes
			rmctl backup restore ~/rm-backup --id 3f2a --yes
		`),
		Args: cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Writer { return s.Writer })
			if err != nil {
				return err
			}
			// --id takes commas as --page does.
			opts.IDs = cliutil.SplitPages(opts.IDs)
			dryRun := opts.DryRun
			opts.DryRun = true
			plan, err := w.RestoreBackup(cmd.Context(), args[0], opts)
			if err != nil {
				return cliutil.PagesErr("backup restore", err)
			}
			if dryRun {
				return printRestore(cmd, env, plan, true)
			}
			if err = cliutil.Confirm(cmd, env, restoreQuestion(plan, opts.To)); err != nil {
				return err
			}
			opts.DryRun = false
			done, err := w.RestoreBackup(cmd.Context(), args[0], opts)
			if err != nil {
				return cliutil.PagesErr("backup restore", err)
			}
			return printRestore(cmd, env, done, false)
		},
	}
	cmd.Flags().StringVar(&opts.To, "to", "", "the folder to recreate the backup's folders below (default: the top level)")
	cmd.Flags().StringArrayVar(&opts.IDs, "id", nil, "restore only this document: an id or a unique prefix (repeat or comma-separate)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show what would be restored; upload nothing")
	return cmd
}

// restoreQuestion asks for a restore as planned.
func restoreQuestion(r port.RestoreReport, to string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Restore %s from the backup of profile %q", count(len(r.Documents), "document"), r.Profile)
	if stamp := cliutil.Stamp(r.Created); stamp != "" {
		fmt.Fprintf(&b, " taken %s", stamp)
	}
	fmt.Fprintf(&b, " into %s", path.Join("/", to))
	if len(r.Folders) > 0 {
		fmt.Fprintf(&b, ", creating %s", count(len(r.Folders), "folder"))
	}
	b.WriteString("?")
	return b.String()
}

// count is n things, singular for one.
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// printRestore answers with the folders a restore created and the
// documents it imported, or with --dry-run would.
func printRestore(cmd *cobra.Command, env *cliutil.Env, r port.RestoreReport, dryRun bool) error {
	created, restored := "created", "restored"
	if dryRun {
		created, restored = "would create", "would restore"
	}
	v := restoreView{
		Profile: r.Profile, Created: cliutil.Stamp(r.Created), DryRun: dryRun,
		Folders: append([]string{}, r.Folders...), Documents: make([]restoredView, 0, len(r.Documents)),
	}
	t := output.Table{Columns: []output.Column{
		{Title: "action", Role: output.RoleKind},
		{Title: "name", Role: output.RoleName},
		{Title: "kind", Role: output.RoleKind},
		{Title: "id", Role: output.RoleID},
		{Title: "renamed_from", Role: output.RoleID},
		{Title: "folder", Role: output.RolePath},
		{Title: "file", Role: output.RolePath, Detail: true},
	}}
	for _, p := range r.Folders {
		folder := domain.Item{Name: path.Base(p), Kind: domain.KindFolder}
		t.Rows = append(t.Rows, []output.Cell{
			output.Text(created), cliutil.NameCell(folder, false), output.Text(string(domain.KindFolder)),
			output.ID(""), output.ID(""), output.Path(path.Dir(p)), output.Path(""),
		})
	}
	for _, d := range r.Documents {
		rv := restoredView{Item: cliutil.View(nil, d.Item), Folder: d.Folder, File: d.File}
		if d.From != d.Item.ID {
			rv.From = string(d.From)
		}
		v.Documents = append(v.Documents, rv)
		t.Rows = append(t.Rows, []output.Cell{
			output.Text(restored), cliutil.NameCell(d.Item, false), output.Text(string(d.Item.Kind)),
			output.ID(string(d.Item.ID)), output.ID(rv.From), output.Path(d.Folder), output.Path(d.File),
		})
	}
	return env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{t}})
}
