// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
)

// The keys and column titles the browse answers share.
const (
	titlePath    = "path"
	titleSize    = "size"
	keyKind      = "kind"
	keyHash      = "hash"
	keyDocuments = "documents"
)

// folderUsageView is one folder du reports, as --json prints it.
type folderUsageView struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Depth     int    `json:"depth"`
	Bytes     int64  `json:"bytes"`
	Documents int    `json:"documents"`
	Folders   int    `json:"folders"`
}

type kindUsageView struct {
	Kind      string `json:"kind"`
	Documents int    `json:"documents"`
	Bytes     int64  `json:"bytes"`
}

// usageView is du's --json answer.
type usageView struct {
	folderUsageView
	ByKind     []kindUsageView   `json:"by_kind"`
	Subfolders []folderUsageView `json:"subfolders"`
}

func folderView(u domain.FolderUsage) folderUsageView {
	return folderUsageView{ID: string(u.ID), Path: u.Path, Depth: u.Depth, Bytes: u.Bytes, Documents: u.Documents, Folders: u.Folders}
}

func newDu(env *cliutil.Env) *cobra.Command {
	var depth int
	var all bool
	cmd := &cobra.Command{
		Use:     "du [path]",
		GroupID: GroupID,
		Short:   "What a folder and the folders below it take",
		Long: heredoc.Doc(`
			Adds up what the documents below a folder take, the top level by default:
			the bytes of every file their indexes list, which is what the cloud keeps
			for them. It reports the folder's total, each folder below it down to
			--depth levels (1 by default; 0 is the total alone), largest first, and
			the documents by kind. --all counts the trash. It reads only the indexes,
			so it runs from the cache, --offline too.
		`),
		Example: heredoc.Doc(`
			rmctl du
			rmctl du /Books --depth 2
			rmctl du --all --json
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, true)),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if depth < 0 {
				return cliutil.Usagef("%s: --depth is 0 or more", cmd.CommandPath())
			}
			b, err := browser(cmd, env)
			if err != nil {
				return err
			}
			query := "/"
			if len(args) == 1 {
				query = args[0]
			}
			_, u, err := b.Usage(cmd.Context(), query, depth, all)
			if err != nil {
				return fmt.Errorf("du: %w", err)
			}
			return env.Emit(cmd, usageResult(u))
		},
	}
	cmd.Flags().IntVarP(&depth, "depth", "d", 1, "report the folders this many levels down")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "count the trash")
	return cmd
}

// usageResult is the total as a record, then the folders and the kinds.
func usageResult(u domain.Usage) output.Result {
	v := usageView{folderUsageView: folderView(u.Total), ByKind: []kindUsageView{}, Subfolders: []folderUsageView{}}
	total := output.Record{Fields: []output.Field{
		{Key: titlePath, Value: output.Path(u.Total.Path)},
		{Key: "bytes", Value: output.Size(u.Total.Bytes)},
		{Key: keyDocuments, Value: output.Count(u.Total.Documents)},
		{Key: "folders", Value: output.Count(u.Total.Folders)},
	}}
	folders := output.Table{Columns: []output.Column{
		{Title: titlePath, Role: output.RolePath},
		{Title: titleSize, Role: output.RoleSize},
		{Title: keyDocuments, Role: output.RoleCount},
		{Title: "folders", Role: output.RoleCount},
		{Title: "id", Role: output.RoleID, Detail: true},
	}}
	for _, f := range u.Folders {
		v.Subfolders = append(v.Subfolders, folderView(f))
		folders.Rows = append(folders.Rows, []output.Cell{
			output.Path(f.Path).With(output.Folder), output.Size(f.Bytes), output.Count(f.Documents), output.Count(f.Folders), output.ID(string(f.ID)),
		})
	}
	kinds := output.Table{Columns: []output.Column{
		{Title: keyKind, Role: output.RoleKind},
		{Title: keyDocuments, Role: output.RoleCount},
		{Title: titleSize, Role: output.RoleSize},
	}}
	for _, k := range u.Kinds {
		v.ByKind = append(v.ByKind, kindUsageView{Kind: string(k.Kind), Documents: k.Documents, Bytes: k.Bytes})
		kinds.Rows = append(kinds.Rows, []output.Cell{output.Text(string(k.Kind)), output.Count(k.Documents), output.Size(k.Bytes)})
	}
	return output.Result{JSON: v, Doc: output.Doc{total, folders, kinds}}
}

// duplicateView is one group dupes reports, as --json prints it.
type duplicateView struct {
	Hash      string           `json:"hash"`
	Size      int64            `json:"size"`
	Wasted    int64            `json:"wasted"`
	Documents []duplicateEntry `json:"documents"`
}

type duplicateEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// duplicatesView is dupes' --json answer.
type duplicatesView struct {
	Groups []duplicateView `json:"groups"`
	Wasted int64           `json:"wasted"`
}

func newDupes(env *cliutil.Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "dupes [path]",
		GroupID: GroupID,
		Short:   "Documents made from the same PDF or EPUB",
		Long: heredoc.Doc(`
			Finds the documents below a folder, the top level by default, whose
			original — the PDF or EPUB they were made from — is the same file, by its
			hash, and groups them, the groups that waste the most first. Annotations
			are not compared: two copies of a PDF with different notes are still
			duplicates of the PDF. Notebooks have no original and are never listed.
			--all counts the trash. It reads only the indexes, so it runs from the
			cache, --offline too.
		`),
		Example: heredoc.Doc(`
			rmctl dupes
			rmctl dupes /Books --json
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, true)),
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
			tree, groups, err := b.Duplicates(cmd.Context(), query, all)
			if err != nil {
				return fmt.Errorf("dupes: %w", err)
			}
			if len(groups) == 0 && !env.JSON(cmd) {
				cliutil.Notef(cmd.ErrOrStderr(), "no two documents share an original")
			}
			return env.Emit(cmd, duplicatesResult(tree, groups))
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "count the trash")
	return cmd
}

// duplicatesResult is one row per document, the rows of a group together,
// then the total the copies waste.
func duplicatesResult(tree *domain.Tree, groups []domain.DuplicateGroup) output.Result {
	v := duplicatesView{Groups: []duplicateView{}}
	t := output.Table{Columns: []output.Column{
		{Title: keyHash, Role: output.RoleHash},
		{Title: titleSize, Role: output.RoleSize},
		{Title: titlePath, Role: output.RolePath},
		{Title: keyKind, Role: output.RoleKind},
		{Title: "id", Role: output.RoleID},
	}}
	for _, g := range groups {
		gv := duplicateView{Hash: g.Hash, Size: g.Size, Wasted: g.Wasted()}
		v.Wasted += g.Wasted()
		for _, it := range g.Items {
			trashed := tree.IsTrashed(it.ID)
			path := cliutil.NameCell(it, trashed)
			path.Kind, path.Text = output.KindPath, tree.Path(it.ID)
			gv.Documents = append(gv.Documents, duplicateEntry{ID: string(it.ID), Name: it.Name, Path: tree.Path(it.ID), Kind: string(it.Kind)})
			t.Rows = append(t.Rows, []output.Cell{output.ID(g.Hash), output.Size(g.Size), path, output.Text(string(it.Kind)), output.ID(string(it.ID))})
		}
		v.Groups = append(v.Groups, gv)
	}
	total := output.Record{Fields: []output.Field{
		{Key: "groups", Value: output.Count(len(groups))},
		{Key: "wasted", Value: output.Size(v.Wasted)},
	}}
	return output.Result{JSON: v, Doc: output.Doc{t, total}}
}
