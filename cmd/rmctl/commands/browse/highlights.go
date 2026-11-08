// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// highlightsFlags are what highlights is asked: the page flags, and those
// of a note and of a folder's notes.
type highlightsFlags struct {
	pageFlags
	dest, pdfLink string
	tree          port.NotesTreeOptions
}

// notesTreeFlags are the flags for a folder alone.
var notesTreeFlags = []string{"output", "all", "all-docs", "incremental", "prune", "jobs"} //nolint:gochecknoglobals // a fixed list

func newHighlights(env *cliutil.Env) *cobra.Command {
	f := highlightsFlags{note: true}
	cmd := &cobra.Command{
		Use:     "highlights <path|id|file.rmdoc|folder> [-o dir]",
		GroupID: GroupID,
		Short:   "Print the highlights of a document, or write a folder's as markdown notes",
		Long: heredoc.Doc(`
			Prints the highlights of a PDF or an EPUB, page by page, with their color
			and the words under them as the tablet copied them out of the text layer.
			A highlight over a page with no text layer has no words: it is listed as
			"(no text)". --format markdown writes each page's highlights as quotes.
			A local .rmdoc file is read offline, every document of it in turn.

			--format note writes a note for a notes app such as Obsidian or Logseq:
			YAML front matter (title, remarkable-id, remarkable-path, type, modified,
			pages, highlights and the document's tags), the title, "## Highlights"
			with a "### Page N" for each page, each highlight a quote, top of the
			page first, ending with its page and its color as a tag
			(#highlight/yellow), then "## Typed text" with the typed text of each
			page. The same document always gives the same note, so a note written
			again changes only where the document did. --pdf-link makes each page
			a link, to the page of a PDF kept beside the notes: a pattern where
			{root} is the way up from the note's directory to the top of the notes,
			{path} the note's path below it without .md, {name} its last part, {id}
			the document's id and {page} the page. "{root}../pdf/{path}.pdf#page={page}"
			links the notes of a folder in vault/notes to the PDFs "rmctl render"
			draws of the same folder into vault/pdf, which share their names.

			A folder, or "/" for the whole library, writes the note of every
			document below it into a directory, keeping the folder tree: -o names
			the directory, created when missing; without it, a directory named
			after the folder, or remarkable for "/", in the working directory. Each
			document becomes <folder path>/<name>.md, its name made safe for a
			file, a .pdf or .epub it ends with dropped; two names that collide in
			one directory get the first 8 characters of the later one's id in
			brackets, as "rmctl render" names its PDFs. A document with no
			highlights gets no note, unless --all-docs, or unless it had one: a
			note follows its document. Documents in the trash are left out unless
			--all. The directory keeps .rmctl-notes.json, a record of what each
			note was written from and with which --pdf-link and --all-docs.
			--incremental skips a document whose note is there with the document's
			modification time, which each note written takes, and which that
			record shows written from the same document with the same options, and
			a document the record shows had no highlights, unread. A note is
			rmctl's: one edited by hand is written again. The notes the record
			lists whose document is no longer below the folder — deleted, moved,
			renamed or put in the trash — are left where they are; --prune moves
			them into .rmctl-removed in the directory, which Obsidian and Logseq
			do not show, keeping their paths. Nothing is ever deleted. --jobs is
			how many documents are read at a time. A document that fails is
			reported and the others go on; the answer is a row per document
			(written, unchanged, skipped or failed) and the totals, and the
			command then fails if any did.
		`),
		Example: heredoc.Doc(`
			rmctl highlights /Books/Paper
			rmctl highlights /Books/Paper --format markdown > notes.md
			rmctl highlights /Books/Paper --format note > Paper.md
			rmctl highlights 3f2a --json
			rmctl highlights /Books -o ~/vault/reMarkable --incremental
			rmctl highlights / -o ~/vault/rm --incremental --prune --pdf-link '{root}../rm-pdf/{path}.pdf#page={page}'
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			link, err := domain.ParsePageLink(f.pdfLink)
			if err != nil {
				return cliutil.Usagef("highlights: --pdf-link: %v", err)
			}
			if !cliutil.LocalArchive(args[0]) {
				n, nerr := cliutil.Need(cmd, env, func(s cliutil.Services) port.Notes { return s.Notes })
				if nerr != nil {
					return nerr
				}
				folder, ferr := n.Folder(cmd.Context(), args[0])
				if ferr != nil {
					return cliutil.PagesErr(cmd.Name(), ferr)
				}
				if folder {
					return f.writeTree(cmd, env, n, args[0])
				}
			}
			for _, name := range notesTreeFlags {
				if cmd.Flags().Changed(name) {
					return cliutil.Usagef("highlights: --%s is for a folder; a document's highlights are printed", name)
				}
			}
			return f.print(cmd, env, args[0], link)
		},
	}
	f.add(cmd)
	fl := cmd.Flags()
	fl.StringVarP(&f.dest, "output", "o", "", "with a folder: the directory the notes go into (default: <folder> here)")
	fl.StringVar(&f.pdfLink, "pdf-link", "", "with notes: link each page to this PDF path, of {root}, {path}, {name}, {id} and {page}")
	fl.BoolVarP(&f.tree.All, "all", "a", false, "with a folder: take the documents in the trash too")
	fl.BoolVar(&f.tree.AllDocs, "all-docs", false, "with a folder: write a note for every document, highlights or not")
	fl.BoolVar(&f.tree.Incremental, "incremental", false, "with a folder: skip a document whose note is there, written from it as it is with these options")
	fl.BoolVar(&f.tree.Prune, "prune", false, "with a folder: move the notes of documents no longer below it into .rmctl-removed")
	fl.IntVarP(&f.tree.Jobs, "jobs", "j", 0, "with a folder: documents at a time (0: the default)")
	return cmd
}

// print prints the highlights of the document query names, or of every
// document of a local archive.
func (f *highlightsFlags) print(cmd *cobra.Command, env *cliutil.Env, query string, link domain.PageLink) error {
	if format, err := f.resolved(cmd, env); err == nil && format != formatNote && !link.IsZero() {
		return cliutil.Usagef("highlights: --pdf-link is for a note: add --format note")
	}
	docs, format, local, err := f.load(cmd, env, query)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if format == formatJSON {
		return writeJSON(out, docs, local, highlightsView)
	}
	if format == formatNote && len(docs) > 1 {
		return cliutil.Usagef("highlights: %s holds %d documents and a note is of one: --format markdown writes them all", query, len(docs))
	}
	for i, doc := range docs {
		if domain.Highlights(doc.Pages) == 0 {
			cliutil.Notef(cmd.ErrOrStderr(), "%s has no highlights", doc.Item.Name)
		}
		var text string
		switch format {
		case formatNote:
			rel := strings.TrimSuffix(domain.DerivedFileName(doc.Item.Name, "md"), ".md")
			text = domain.Note{Item: doc.Item, Path: doc.Path, Pages: doc.Pages, Link: link, Rel: rel}.Markdown()
		case formatMarkdown:
			text = HighlightsMarkdown(doc.Pages)
		default:
			text = HighlightsText(doc.Pages)
		}
		if err := heading(out, docs, i, format); err != nil {
			return err
		}
		if _, err := io.WriteString(out, text); err != nil {
			return err
		}
	}
	return nil
}

// writeTree writes the notes of every document below the folder query
// names. The answer is written also when some documents failed; the
// command then fails after it.
func (f *highlightsFlags) writeTree(cmd *cobra.Command, env *cliutil.Env, n port.Notes, query string) error {
	changed := cmd.Flags().Changed
	switch {
	case changed("page"):
		return cliutil.Usagef("highlights: %s is a folder: each note holds every page of its document; leave --page out", query)
	case changed("format") && f.format != formatNote:
		return cliutil.Usagef("highlights: %s is a folder: its documents are written as notes; leave --format out", query)
	case strings.EqualFold(filepath.Ext(f.dest), ".md"):
		return cliutil.Usagef("highlights: %s is a folder: -o names the directory its notes go into, not a file such as %s", query, f.dest)
	}
	report, err := n.NotesTree(cmd.Context(), query, f.dest, port.NoteOptions{PDFLink: f.pdfLink}, f.tree)
	if err != nil && (len(report.Documents) == 0 || cmd.Context().Err() != nil) {
		return cliutil.PagesErr("highlights", err)
	}
	w := cmd.ErrOrStderr()
	if len(report.Moved) > 0 {
		cliutil.Notef(w, "%d notes of documents no longer below %s were moved into %s", len(report.Moved), query, domain.NotesRemovedDir)
	}
	if len(report.Left) > 0 {
		cliutil.Notef(w, "%d notes of documents no longer below %s were left where they are; --prune moves them into %s",
			len(report.Left), query, domain.NotesRemovedDir)
	}
	if len(report.Documents) == 0 && !env.JSON(cmd) {
		cliutil.Notef(w, "no document below %s", query)
	}
	if eerr := env.Emit(cmd, notesResult(report)); eerr != nil {
		return eerr
	}
	return cliutil.PagesErr("highlights", err)
}

// notesView is what writing a folder's notes did, for --json.
type notesView struct {
	Dir       string     `json:"dir"`
	Documents []noteView `json:"documents"`
	Written   int        `json:"written"`
	Unchanged int        `json:"unchanged"`
	Skipped   int        `json:"skipped"`
	Failed    int        `json:"failed"`
	Moved     []string   `json:"moved"`
	Left      []string   `json:"left"`
}

// noteView is one document of a folder's notes.
type noteView struct {
	Path       string `json:"path"`
	Document   string `json:"document"`
	ID         string `json:"id"`
	Status     string `json:"status"`
	Highlights int    `json:"highlights"`
	Error      string `json:"error,omitempty"`
}

// notesResult is one row per document, then the totals.
func notesResult(r port.NotesTreeReport) output.Result {
	v := notesView{
		Dir: r.Dir, Documents: make([]noteView, 0, len(r.Documents)),
		Written: r.Count(port.NoteWritten), Unchanged: r.Count(port.NoteUnchanged),
		Skipped: r.Count(port.NoteSkipped), Failed: r.Count(port.NoteFailed),
		Moved: append([]string{}, r.Moved...), Left: append([]string{}, r.Left...),
	}
	t := output.Table{Columns: []output.Column{
		{Title: "status", Role: output.RoleKind},
		{Title: titlePath, Role: output.RolePath},
		{Title: "document", Role: output.RolePath},
		{Title: "highlights", Role: output.RoleCount},
		{Title: "error", Role: output.RoleText},
	}}
	for _, d := range r.Documents {
		dv := noteView{Path: d.Path, Document: d.LibraryPath, ID: string(d.Document.ID), Status: string(d.Status), Highlights: d.Highlights}
		count := output.Text("")
		if d.Status == port.NoteWritten {
			count = output.Count(d.Highlights)
		}
		if d.Err != nil {
			dv.Error = d.Err.Error()
		}
		v.Documents = append(v.Documents, dv)
		t.Rows = append(t.Rows, []output.Cell{output.Text(dv.Status), output.Path(dv.Path), output.Path(dv.Document), count, output.Text(dv.Error)})
	}
	total := output.Record{Fields: []output.Field{
		{Key: "dir", Value: output.Path(r.Dir)},
		{Key: "documents", Value: output.Count(len(r.Documents))},
		{Key: "written", Value: output.Count(v.Written)},
		{Key: "unchanged", Value: output.Count(v.Unchanged)},
		{Key: "skipped", Value: output.Count(v.Skipped)},
		{Key: "failed", Value: output.Count(v.Failed)},
		{Key: "moved", Value: output.Count(len(r.Moved))},
		{Key: "left", Value: output.Count(len(r.Left))},
	}}
	return output.Result{JSON: v, Doc: output.Doc{t, total}}
}
