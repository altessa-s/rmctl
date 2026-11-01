// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

// Commands are get, export and mget.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{newGet(env), newExport(env), newMget(env)}
}

func fetcher(cmd *cobra.Command, env *cliutil.Env) (port.Fetcher, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Fetcher { return s.Fetcher })
}

// pathView is the --json answer of a command that wrote one file.
type pathView struct {
	Path string `json:"path"`
}

func printPath(cmd *cobra.Command, path string) error {
	out := cmd.OutOrStdout()
	if cliutil.JSON(cmd) {
		return cliutil.WriteJSON(out, pathView{Path: path})
	}
	_, err := fmt.Fprintln(out, path)
	return err
}

func newGet(env *cliutil.Env) *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:     "get <path|id>",
		GroupID: cliutil.GroupRead,
		Short:   "Download a document's original",
		Long: heredoc.Doc(`
			Downloads the PDF or EPUB a document was made from, checked against its
			hash. Without -o it is written under the document's name in the working
			directory. The file appears only once it is whole: an interrupted or failed
			download leaves nothing behind. The path written is the answer.
		`),
		Example: heredoc.Doc(`
			rmctl get /Books/Novel
			rmctl get 3f2a -o paper.pdf
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := fetcher(cmd, env)
			if err != nil {
				return err
			}
			progress, done := cliutil.Progress(env.Streams, "downloading")
			path, err := f.Get(cmd.Context(), args[0], dest, progress)
			done()
			if err != nil {
				return fmt.Errorf("get: %w", err)
			}
			return printPath(cmd, path)
		},
	}
	cmd.Flags().StringVarP(&dest, "output", "o", "", "file to write")
	return cmd
}

// exportView is what export wrote, for --json.
type exportView struct {
	Path      string `json:"path"`
	Documents int    `json:"documents"`
	Files     int    `json:"files"`
}

func newExport(env *cliutil.Env) *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:     "export <path|id>... -o <file.rmdoc>",
		GroupID: cliutil.GroupRead,
		Short:   "Download documents whole as one .rmdoc",
		Long: heredoc.Doc(`
			Downloads every file of the documents named and packs them into one
			.rmdoc archive, stored the way the tablet writes one: the container the
			reMarkable apps import, ink and all. A folder, or "/" for the whole
			library, stands for every document below it that is not in the trash on
			its own; a document named twice is in the archive once. The tablet
			imports one document per archive; "rmctl put" imports any number.
		`),
		Example: heredoc.Doc(`
			rmctl export /Notes/Meeting -o meeting.rmdoc
			rmctl export /Notes 3f2a -o backup.rmdoc
		`),
		Args:              cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := fetcher(cmd, env)
			if err != nil {
				return err
			}
			report, err := f.Export(cmd.Context(), args, dest)
			if err != nil {
				return cliutil.PagesErr("export", err)
			}
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, exportView(report))
			}
			_, err = fmt.Fprintf(out, "%s (%d documents, %d files)\n", report.Path, report.Documents, report.Files)
			return err
		},
	}
	cmd.Flags().StringVarP(&dest, "output", "o", "", "archive to write")
	_ = cmd.MarkFlagRequired("output") //nolint:errcheck // the flag exists
	return cmd
}

func newMget(env *cliutil.Env) *cobra.Command {
	var (
		dir  string
		opts port.MirrorOptions
	)
	cmd := &cobra.Command{
		Use:     "mget <path> -o <dir>",
		GroupID: cliutil.GroupRead,
		Short:   "Download a folder's documents, keeping the tree",
		Long: heredoc.Doc(`
			Downloads the original of every document below a folder into a local
			directory, keeping the folder tree. --incremental skips files already
			there with the size the cloud names; --delete removes local files the
			folder no longer holds.
		`),
		Example: heredoc.Doc(`
			rmctl mget /Books -o ~/Books
			rmctl mget / -o ~/remarkable --incremental --delete --jobs 8
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, true),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := fetcher(cmd, env)
			if err != nil {
				return err
			}
			report, err := f.Mirror(cmd.Context(), args[0], dir, opts)
			if err != nil {
				return fmt.Errorf("mget: %w", err)
			}
			return cliutil.PrintReport(cmd.OutOrStdout(), cliutil.JSON(cmd), report)
		},
	}
	cmd.Flags().StringVarP(&dir, "output", "o", "", "directory to write into")
	cmd.Flags().BoolVar(&opts.Incremental, "incremental", false, "skip files already present")
	cmd.Flags().BoolVar(&opts.Delete, "delete", false, "remove local files the folder no longer holds")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 0, "transfers at a time (0: the default)")
	_ = cmd.MarkFlagRequired("output") //nolint:errcheck // the flag exists
	return cmd
}

// reportView is a recursive transfer as --json prints it.
