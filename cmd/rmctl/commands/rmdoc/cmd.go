// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdoc

import (
	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
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
			return env.Emit(cmd, output.Result{JSON: v, Doc: doc(v)})
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print document names unmasked")
	return cliutil.WithAccess(cmd, cliutil.AccessOffline)
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

// doc is the archive as a record, then its documents as a tree: each with
// its kind, identifier, file count and size, and its files below it.
func doc(v archiveView) output.Doc {
	rec := output.Record{Fields: []output.Field{
		{Key: "path", Value: output.Path(v.Path)},
		{Key: "documents", Value: output.Count(len(v.Documents))},
	}}
	tree := output.Tree{}
	for _, d := range v.Documents {
		name := output.Text(d.Name)
		if d.Name == "" {
			name = output.Text("(no readable .metadata)").With(output.Muted)
		}
		n := output.Node{Cells: []output.Cell{
			name, output.Text(d.Kind).With(output.Muted), output.ID(d.ID), output.Count(len(d.Entries)), output.Size(d.Size),
		}}
		for _, e := range d.Entries {
			n.Children = append(n.Children, output.Node{Cells: []output.Cell{output.Text(e.Name), output.Size(e.Size)}})
		}
		tree.Nodes = append(tree.Nodes, n)
	}
	return output.Doc{rec, tree}
}
