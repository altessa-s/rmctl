// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of the diagnostics.
const GroupID = "debug"

// New returns the debug command group.
func New(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "debug <command>",
		GroupID: GroupID,
		Short:   "Ask the protocol directly",
		Long: heredoc.Doc(`
			Diagnostics of the sync protocol rather than the library: the root, raw
			blobs, what a document is made of, which hash records carry, which blobs
			the cloud lacks, which fields occur across the library, and what the .rm
			pages of a document hold. None of them changes anything.
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRoot(env), newBlob(env), newFiles(env), newHashes(env), newCheckFiles(env), newSweep(env), newPage(env))
	return cmd
}

func inspector(cmd *cobra.Command, env *cliutil.Env) (port.Inspector, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Inspector { return s.Inspector })
}

// rootView is the root as --json prints it.
type rootView struct {
	Hash       string `json:"hash"`
	Generation int64  `json:"generation"`
	Records    int    `json:"records"`
}

func newRoot(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:          "root",
		Short:        "The hash and the generation of the root",
		Example:      "rmctl debug root --json\n",
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			root, err := in.Root(cmd.Context())
			if err != nil {
				return fmt.Errorf("debug root: %w", err)
			}
			v := rootView{Hash: root.Hash, Generation: root.Generation, Records: len(root.Records)}
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(cmd.OutOrStdout(), v)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "hash        %s\ngeneration  %d\nrecords     %d\n", v.Hash, v.Generation, v.Records)
			return err
		},
	}
}

func newBlob(env *cliutil.Env) *cobra.Command {
	var name, dest string
	cmd := &cobra.Command{
		Use:   "blob <hash>",
		Short: "A blob by hash, as stored",
		Long: heredoc.Doc(`
			Fetches a blob by its hash and prints it, or writes it to -o, which must
			not exist yet. --name is the record name the storage asks for. The
			transport checks every blob against its hash, so a blob whose bytes do
			not match is an error, not output.
		`),
		Example: heredoc.Doc(`
			rmctl debug blob 9f86d0… --name 3f2a.metadata
			rmctl debug blob 9f86d0… -o blob.bin
		`),
		Args:         cliutil.Args(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			if dest != "" {
				if _, err = in.SaveBlob(cmd.Context(), args[0], name, dest); err != nil {
					return fmt.Errorf("debug blob: %w", err)
				}
				return nil
			}
			data, err := in.Blob(cmd.Context(), args[0], name)
			if err != nil {
				return fmt.Errorf("debug blob: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the record name the storage asks for")
	cmd.Flags().StringVarP(&dest, "output", "o", "", "file to write instead of printing")
	return cmd
}

// fileReportView is one file of debug files as --json prints it.
type fileReportView struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Text string `json:"text,omitempty"`
}

func newFiles(env *cliutil.Env) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "files <path|id>",
		Short: "What a document is made of, and what its text files say",
		Long: heredoc.Doc(`
			Lists the files of an item's index and prints the ones that are text or
			JSON. The fields visibleName, source and tags are masked as <name:N> at any
			depth; --raw prints the files as they stand, so it is not for a report.
		`),
		Example:           "rmctl debug files 3f2a\n",
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			it, reports, err := in.Files(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("debug files: %w", err)
			}
			views := make([]fileReportView, 0, len(reports))
			for _, r := range reports {
				text := r.Text
				if text != nil && !raw {
					text = cliutil.MaskJSON(text)
				}
				views = append(views, fileReportView{Name: r.File.Name, Hash: r.File.Hash, Size: r.File.Size, Text: string(text)})
			}
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, struct {
					ID    string           `json:"id"`
					Files []fileReportView `json:"files"`
				}{ID: string(it.ID), Files: views})
			}
			return writeFiles(out, views)
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print the files unmasked")
	return cmd
}

func writeFiles(w io.Writer, views []fileReportView) error {
	for _, v := range views {
		if _, err := fmt.Fprintf(w, "== %s  %s  %s\n", v.Name, cliutil.Size(v.Size), v.Hash); err != nil {
			return err
		}
		if v.Text == "" {
			continue
		}
		text := v.Text
		if text[len(text)-1] != '\n' {
			text += "\n"
		}
		if _, err := io.WriteString(w, text); err != nil {
			return err
		}
	}
	return nil
}

// hashView is one record of debug hashes as --json prints it.
type hashView struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
	By   string `json:"by"`
}

func newHashes(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:          "hashes",
		Short:        "Which hash each record carries: of the index file or of the node",
		Example:      "rmctl debug hashes\n",
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			reports, err := in.Hashes(cmd.Context())
			if err != nil {
				return fmt.Errorf("debug hashes: %w", err)
			}
			views := make([]hashView, 0, len(reports))
			for _, r := range reports {
				by := "node"
				if r.ByIndexFile {
					by = "file"
				}
				views = append(views, hashView{ID: string(r.Record.ID), Hash: r.Record.Hash, By: by})
			}
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, views)
			}
			for _, v := range views {
				if _, err := fmt.Fprintf(out, "%s  %s  %s\n", v.By, v.ID, v.Hash); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func newCheckFiles(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:          "checkfiles",
		Short:        "Which blobs of the library the cloud lacks",
		Example:      "rmctl debug checkfiles\n",
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			missing, err := in.CheckFiles(cmd.Context())
			if err != nil {
				return fmt.Errorf("debug checkfiles: %w", err)
			}
			if missing == nil {
				missing = []string{}
			}
			return printList(cmd, missing, "the cloud holds every blob")
		},
	}
}

func newSweep(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Which .metadata and .content fields occur across the library",
		Long: heredoc.Doc(`
			Reads every document's .metadata and .content and counts how many carry
			each field: the way to find fields nobody has a name for yet. Only field
			names and counts are printed, never values.
		`),
		Example:      "rmctl debug sweep --json\n",
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := inspector(cmd, env)
			if err != nil {
				return err
			}
			counts, err := in.Sweep(cmd.Context())
			if err != nil {
				return fmt.Errorf("debug sweep: %w", err)
			}
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, counts)
			}
			for _, k := range slices.Sorted(maps.Keys(counts)) {
				if _, err := fmt.Fprintf(out, "%6d  %s\n", counts[k], k); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func printList(cmd *cobra.Command, lines []string, empty string) error {
	out := cmd.OutOrStdout()
	if cliutil.JSON(cmd) {
		return cliutil.WriteJSON(out, lines)
	}
	if len(lines) == 0 {
		cliutil.Notef(cmd.ErrOrStderr(), "%s", empty)
		return nil
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(out, l); err != nil {
			return err
		}
	}
	return nil
}
