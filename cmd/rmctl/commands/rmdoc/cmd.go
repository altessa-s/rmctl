// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdoc

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

type entryView struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type documentView struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Size    int64       `json:"size"`
	Entries []entryView `json:"entries"`
}

type archiveView struct {
	Path      string         `json:"path"`
	Documents []documentView `json:"documents"`
}

// New returns the rmdoc command.
func New(env *cliutil.Env) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:     "rmdoc <file.rmdoc>",
		GroupID: cliutil.GroupRead,
		Short:   "Show what a local .rmdoc archive holds",
		Long: heredoc.Doc(`
			Lists the documents of a local .rmdoc archive, offline: each one's
			identifier, name, kind and files with their sizes. Names are masked as
			<name:N> so the output can go into a report; --raw prints them as they
			are. "rmctl text" and "rmctl highlights" read the pages of an archive,
			and "rmctl put" imports it.
		`),
		Example: heredoc.Doc(`
			rmctl rmdoc backup.rmdoc
			rmctl rmdoc backup.rmdoc --raw --json
		`),
		Args:         cliutil.Args(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Archives { return s.Archives })
			if err != nil {
				return err
			}
			report, err := a.Inspect(cmd.Context(), args[0])
			if err != nil {
				return cliutil.PagesErr("rmdoc", err)
			}
			v := view(report, raw)
			out := cmd.OutOrStdout()
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(out, v)
			}
			if _, err := fmt.Fprintf(out, "%s: %d documents\n", v.Path, len(v.Documents)); err != nil {
				return err
			}
			for _, d := range v.Documents {
				name := d.Name
				if name == "" {
					name = "(no readable .metadata)"
				}
				kind := ""
				if d.Kind != "" {
					kind = " [" + d.Kind + "]"
				}
				if _, err := fmt.Fprintf(out, "%s%s  %s  %d files, %s\n", name, kind, d.ID, len(d.Entries), cliutil.Size(d.Size)); err != nil {
					return err
				}
				for _, e := range d.Entries {
					if _, err := fmt.Fprintf(out, "  %s  %s\n", e.Name, cliutil.Size(e.Size)); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print document names unmasked")
	return cmd
}

func view(r port.ArchiveReport, raw bool) archiveView {
	v := archiveView{Path: r.Path, Documents: []documentView{}}
	for _, d := range r.Documents {
		name := d.Name
		if !raw {
			name = cliutil.MaskName(name)
		}
		dv := documentView{ID: string(d.ID), Name: name, Kind: string(d.Kind), Entries: []entryView{}}
		for _, e := range d.Entries {
			dv.Entries = append(dv.Entries, entryView{Name: e.Name, Size: e.Size})
			dv.Size += e.Size
		}
		v.Documents = append(v.Documents, dv)
	}
	return v
}
