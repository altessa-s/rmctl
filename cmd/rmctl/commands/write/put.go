// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"errors"
	"fmt"
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

// ErrSomeFailed means put uploaded some files and failed others; each
// failure was reported as it happened.
var ErrSomeFailed = errors.New("some files were not uploaded")

// isDocument reports the extensions put uploads as they are.
func isDocument(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf", ".epub":
		return true
	default:
		return false
	}
}

// isArchive reports an .rmdoc archive, which put imports whole.
func isArchive(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".rmdoc")
}

// isText reports the extensions put --notebook makes notebooks of.
func isText(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt":
		return true
	default:
		return false
	}
}

// putFlags are put's options as given on the command line.
type putFlags struct {
	opts     port.PutOptions
	to       string
	cover    string
	notebook bool
	device   string
}

// split separates the files from the destination folder. --to names the
// folder, and every argument is a file. Without it, a last argument that
// has no uploadable extension is the folder, so "put a.pdf b.epub /Papers"
// reads as it looks; a folder named like a file takes --to.
func (f *putFlags) split(args []string) (files []string, dest string) {
	if f.to != "" {
		return args, f.to
	}
	last := args[len(args)-1]
	if len(args) > 1 && !isDocument(last) && !isText(last) && !isArchive(last) {
		return args[:len(args)-1], last
	}
	return args, "/"
}

// validate checks the flags and every file's kind before anything is sent.
func (f *putFlags) validate(cmd *cobra.Command, files []string) error {
	switch f.cover {
	case "", string(port.CoverLastOpened), string(port.CoverFirst):
		f.opts.Cover = port.Cover(f.cover)
	default:
		return cliutil.Usagef("%s: unknown --cover %q: use first or last", cmd.CommandPath(), f.cover)
	}
	switch domain.Device(f.device) {
	case domain.DeviceRM2, domain.DevicePure, domain.DevicePro, domain.DeviceMove:
	default:
		return cliutil.Usagef("%s: unknown --device %q: use rm2, pure, pro or move", cmd.CommandPath(), f.device)
	}
	if f.opts.Name != "" && len(files) > 1 {
		return cliutil.Usagef("%s: --name names one file; %d were given", cmd.CommandPath(), len(files))
	}
	if f.notebook && (f.opts.Replace || f.opts.Cover != "") {
		return cliutil.Usagef("%s: --replace and --cover are for PDF and EPUB, not --notebook", cmd.CommandPath())
	}
	for _, file := range files {
		switch {
		case isArchive(file) && f.notebook:
			return cliutil.Usagef("%s: %s: an archive is imported as it is, not with --notebook", cmd.CommandPath(), file)
		case isArchive(file) && (f.opts.Name != "" || f.opts.Cover != "" || f.opts.Replace):
			return cliutil.Usagef("%s: %s: --name, --cover and --replace are for PDF and EPUB, not an archive", cmd.CommandPath(), file)
		case isArchive(file):
		case f.notebook && !isText(file):
			return cliutil.Usagef("%s: %s: --notebook takes .md, .markdown or .txt", cmd.CommandPath(), file)
		case !f.notebook && isText(file):
			return cliutil.Usagef("%s: %s: a text file becomes a notebook with --notebook", cmd.CommandPath(), file)
		case !f.notebook && !isDocument(file):
			return cliutil.Usagef("%s: %s: only a PDF, an EPUB or an .rmdoc archive can be uploaded", cmd.CommandPath(), file)
		}
	}
	return nil
}

func (f *putFlags) question(files []string, dest string) string {
	what := filepath.Base(files[0])
	if len(files) > 1 {
		what = fmt.Sprintf("%d files", len(files))
	}
	if f.notebook {
		return fmt.Sprintf("Make %s into notebooks in %s?", what, dest)
	}
	if f.opts.Replace {
		return fmt.Sprintf("Upload %s into %s, replacing documents of the same name?", what, dest)
	}
	return fmt.Sprintf("Upload %s into %s?", what, dest)
}

// putResult is one file's outcome as --json prints it.
type putResult struct {
	File     string            `json:"file"`
	Item     *cliutil.ItemView `json:"item,omitempty"`
	Imported []importedView    `json:"imported,omitempty"`
	DryRun   bool              `json:"dry_run,omitzero"`
	Error    string            `json:"error,omitempty"`
	// uploaded is the item a PDF, an EPUB or a notebook became, for the
	// text line.
	uploaded domain.Item
}

// importedView is one document an archive put into the library.
type importedView struct {
	Item cliutil.ItemView `json:"item"`
	// From is the identifier the document had in the archive, when the
	// library held it and the document took a new one.
	From string `json:"renamed_from,omitempty"`
}

func newPut(env *cliutil.Env) *cobra.Command {
	f := putFlags{device: string(domain.DeviceRM2)}
	cmd := &cobra.Command{
		Use:     "put <file>... [folder]",
		GroupID: GroupID,
		Short:   "Upload PDFs and EPUBs, or make notebooks of text",
		Long: heredoc.Doc(`
			Uploads PDFs and EPUBs into a folder, the top level by default. The folder
			is --to, or else the last argument when it has no .pdf, .epub, .md,
			.markdown or .txt extension; a folder named like a file needs --to. Each
			file's name is its file name without the extension unless --name gives one
			(one file only). A locked PDF, a PDF without pages and an EPUB with DRM are
			refused before anything is sent.

			Each file is its own change: a file that fails is reported on standard
			error and the others still go up; the exit status is 1 when any failed.

			--cover first shows the first page as the cover instead of the last one
			read. --replace replaces the original of the document of the same name in
			the folder instead of adding a second one: its tags are kept, its
			annotations are not. --dry-run checks every file and the folder and sends
			nothing; it needs no --yes.

			--notebook makes a notebook of typed text from a markdown or text file:
			headings, bold lines, bullets and numbered lists become the tablet's own
			paragraph styles, and --device lays it out for that tablet.
		`),
		Example: heredoc.Doc(`
			rmctl put paper.pdf /Papers --yes
			rmctl put a.pdf b.epub c.pdf /Papers --tag work --yes
			rmctl put book.epub --name "A Book" --cover first --yes
			rmctl put paper.pdf --to /Papers --replace --yes
			rmctl put *.pdf /Inbox --dry-run
			rmctl put notes.md /Notes --notebook --device move --yes
		`),
		Args: cliutil.Args(cobra.MinimumNArgs(1)),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, word string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []cobra.Completion{"pdf", "epub", "md", "markdown", "txt"}, cobra.ShellCompDirectiveFilterFileExt
			}
			return cliutil.CompleteRemote(env, true)(cmd, args, word)
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			files, dest := f.split(args)
			if err := f.validate(cmd, files); err != nil {
				return err
			}
			svc, err := env.Services(cmd)
			if err != nil {
				return err
			}
			if !f.opts.DryRun {
				if err = cliutil.Confirm(cmd, env, f.question(files, dest)); err != nil {
					return err
				}
			}
			return runPut(cmd, env, svc, &f, files, dest)
		},
	}
	cmd.Flags().StringVar(&f.opts.Name, "name", "", "visible name (default: the file name; one file only)")
	cmd.Flags().StringArrayVar(&f.opts.Tags, "tag", nil, "tag the documents (repeat for several)")
	cmd.Flags().StringVar(&f.to, "to", "", "the folder to upload into (default: the last argument, else the top level)")
	cmd.Flags().StringVar(&f.cover, "cover", "", "cover page: first, or last (the page last read; the default)")
	cmd.Flags().BoolVar(&f.opts.Replace, "replace", false, "replace the document of the same name; its annotations are lost")
	cmd.Flags().BoolVar(&f.opts.DryRun, "dry-run", false, "check the files and the folder; upload nothing")
	cmd.Flags().BoolVar(&f.notebook, "notebook", false, "make notebooks of typed text from .md, .markdown or .txt files")
	cmd.Flags().StringVar(&f.device, "device", string(domain.DeviceRM2), "tablet a notebook is laid out for: rm2, pure, pro, move")
	return cmd
}

// runPut uploads each file in turn and reports each outcome.
func runPut(cmd *cobra.Command, env *cliutil.Env, svc cliutil.Services, f *putFlags, files []string, dest string) error {
	if (f.notebook && svc.Pages == nil) || (!f.notebook && svc.Writer == nil) {
		return cliutil.ErrNoService
	}
	stream := env.Stream(cmd, putColumns())
	errOut := cmd.ErrOrStderr()
	results := make([]putResult, 0, len(files))
	failed := 0
	for _, file := range files {
		if err := cmd.Context().Err(); err != nil {
			return cliutil.Interrupted(cmd.Context(), err)
		}
		f.warnOnReplace(errOut, file)
		r, err := putFile(cmd, env, svc, f, file, dest)
		if err != nil {
			switch {
			case cmd.Context().Err() != nil:
				return cliutil.Interrupted(cmd.Context(), err)
			case len(files) == 1, errors.Is(err, port.ErrNotRegistered):
				// One file's failure is the command's, with its own exit
				// status; an account that is not registered fails every file.
				return cliutil.PagesErr("put", err)
			}
			failed++
			cliutil.Notef(errOut, "%s", cliutil.PagesErr("put "+file, err))
			results = append(results, putResult{File: file, Error: err.Error()})
			continue
		}
		results = append(results, r)
		for _, row := range putRows(f, r, dest) {
			if err := stream.Row(output.Row{Cells: row}); err != nil {
				return err
			}
		}
	}
	if err := stream.End(results); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("put: %d of %d files: %w", failed, len(files), ErrSomeFailed)
	}
	return nil
}

// warnOnReplace has --replace warn of a document that is really replaced:
// without one of the same name, the file is simply added and loses nothing.
func (f *putFlags) warnOnReplace(errOut io.Writer, file string) {
	if !f.opts.Replace {
		return
	}
	base := filepath.Base(file)
	f.opts.OnReplace = func(old domain.Item) {
		verb := "replaces"
		if f.opts.DryRun {
			verb = "would replace"
		}
		cliutil.Notef(errOut, "%s %s %s (%s); its annotations are dropped", base, verb, old.Name, old.ID)
	}
}

// putFile sends one file the way its kind asks: an archive is imported,
// anything else uploaded or made into a notebook.
func putFile(cmd *cobra.Command, env *cliutil.Env, svc cliutil.Services, f *putFlags, file, dest string) (putResult, error) {
	if isArchive(file) {
		return importOne(cmd, svc, f, file, dest)
	}
	it, err := putOne(cmd, env, svc, f, file, dest)
	if err != nil {
		return putResult{}, err
	}
	r := putResult{File: file, DryRun: f.opts.DryRun, uploaded: it}
	if it.ID != "" {
		v := cliutil.View(nil, it)
		r.Item = &v
	}
	return r, nil
}

// putColumns are the columns of put's answer: what happened to each
// document, and from which file into which folder.
func putColumns() []output.Column {
	return []output.Column{
		{Title: "action", Role: output.RoleKind},
		{Title: "name", Role: output.RoleName},
		{Title: "kind", Role: output.RoleKind},
		{Title: "id", Role: output.RoleID},
		{Title: "renamed_from", Role: output.RoleID},
		{Title: "file", Role: output.RolePath, Detail: true},
		{Title: "folder", Role: output.RolePath, Detail: true},
	}
}

// putRows are the rows of one file's outcome: one document uploaded, or
// every document of an archive.
func putRows(f *putFlags, r putResult, dest string) [][]output.Cell {
	row := func(verb string, it domain.Item, from string) []output.Cell {
		action := past(verb)
		if r.DryRun {
			action = "would " + verb
		}
		return []output.Cell{
			output.Text(action), output.Text(it.Name).With(cliutil.NameCell(it, false).Flags), output.Text(string(it.Kind)),
			output.ID(string(it.ID)), output.ID(from), output.Path(r.File), output.Path(dest),
		}
	}
	if isArchive(r.File) {
		rows := make([][]output.Cell, 0, len(r.Imported))
		for _, d := range r.Imported {
			it := domain.Item{ID: domain.ItemID(d.Item.ID), Name: d.Item.Name, Kind: domain.Kind(d.Item.Kind)}
			rows = append(rows, row("import", it, d.From))
		}
		return rows
	}
	it := r.uploaded
	if it.Name == "" {
		it.Name = strings.TrimSuffix(filepath.Base(r.File), filepath.Ext(r.File))
	}
	verb := "upload"
	if f.notebook {
		verb = "make"
	}
	return [][]output.Cell{row(verb, it, "")}
}

// past is a verb of putRows as done.
func past(verb string) string {
	if verb == "make" {
		return "made"
	}
	return verb + "ed"
}

func putOne(cmd *cobra.Command, env *cliutil.Env, svc cliutil.Services, f *putFlags, file, dest string) (domain.Item, error) {
	if f.notebook {
		opts := port.NotebookOptions{Name: f.opts.Name, Device: domain.Device(f.device), Tags: f.opts.Tags}
		// A dry run checks the file and the folder as the upload would.
		if f.opts.DryRun {
			return svc.Pages.PlanNotebook(cmd.Context(), file, dest, opts)
		}
		return svc.Pages.Notebook(cmd.Context(), file, dest, opts)
	}
	progress, done := cliutil.Progress(env.Streams, "uploading "+filepath.Base(file))
	defer done()
	return svc.Writer.Put(cmd.Context(), file, dest, f.opts, progress)
}

func newUnmark(env *cliutil.Env) *cobra.Command {
	var pages []string
	cmd := &cobra.Command{
		Use:     "unmark <path|id>",
		GroupID: GroupID,
		Short:   "Erase the highlights of a document",
		Long: heredoc.Doc(`
			Erases the highlights of a PDF or an EPUB, of every page or of the pages
			--page names, the way the tablet's own eraser does: each one stays in the
			page as erased, and every other mark, stroke and byte of the page is kept.
			The edited pages are read back before they are sent. The question names
			how many highlights go.
		`),
		Example: heredoc.Doc(`
			rmctl unmark /Books/Paper --yes
			rmctl unmark 3f2a --page 2,5 --yes
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Pages { return s.Pages })
			if err != nil {
				return err
			}
			selected := cliutil.SplitPages(pages)
			doc, err := p.Pages(cmd.Context(), args[0], selected)
			if err != nil {
				return cliutil.PagesErr("unmark", err)
			}
			// The highlighter leaves highlights over a text layer and
			// strokes of its own; unmark erases both, and says which.
			marks, highlights, on := 0, 0, 0
			for _, pg := range doc.Pages {
				if pg.Stats.Marks > 0 {
					marks += pg.Stats.Marks
					highlights += min(len(pg.Highlights), pg.Stats.Marks)
					on++
				}
			}
			if marks == 0 {
				cliutil.Notef(cmd.ErrOrStderr(), "%s has no highlighter marks; nothing to erase", doc.Item.Name)
				return printUnmark(cmd, env, port.UnmarkReport{Item: doc.Item})
			}
			question := fmt.Sprintf("Erase %s on %d pages of %s?", markCount(marks, highlights), on, doc.Item.Name)
			if err = cliutil.Confirm(cmd, env, question); err != nil {
				return err
			}
			report, err := p.Unmark(cmd.Context(), args[0], selected)
			if err != nil {
				return cliutil.PagesErr("unmark", err)
			}
			return printUnmark(cmd, env, report)
		},
	}
	cmd.Flags().StringArrayVar(&pages, "page", nil, "only this page: a number from 1 or a page id (repeat or comma-separate)")
	return cmd
}

type unmarkView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Erased     int    `json:"erased"`
	Highlights int    `json:"highlights"`
	Strokes    int    `json:"strokes"`
	Pages      int    `json:"pages"`
}

func printUnmark(cmd *cobra.Command, env *cliutil.Env, r port.UnmarkReport) error {
	v := unmarkView{ID: string(r.Item.ID), Name: r.Item.Name, Erased: r.Erased, Highlights: r.Highlights, Strokes: r.Strokes, Pages: r.Pages}
	return env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{
		{Key: "id", Value: output.ID(v.ID)},
		{Key: "name", Value: output.Text(v.Name)},
		{Key: "erased", Value: output.Count(v.Erased)},
		{Key: "highlights", Value: output.Count(v.Highlights)},
		{Key: "strokes", Value: output.Count(v.Strokes)},
		{Key: "pages", Value: output.Count(v.Pages)},
	}}}})
}

// markCount names marks as highlights and marker strokes.
func markCount(marks, highlights int) string {
	return fmt.Sprintf("%d marks (%d highlights, %d marker strokes)", marks, highlights, marks-highlights)
}

// importOne imports one .rmdoc archive, or with --dry-run plans the import
// against the current library and uploads nothing.
func importOne(cmd *cobra.Command, svc cliutil.Services, f *putFlags, file, dest string) (putResult, error) {
	r := putResult{File: file, DryRun: f.opts.DryRun}
	// A dry run asks the library too: an identifier it already holds is
	// shown as the new one the document would get.
	importer := svc.Writer.Import
	if f.opts.DryRun {
		importer = svc.Writer.PlanImport
	}
	docs, err := importer(cmd.Context(), file, dest)
	if err != nil {
		return r, err
	}
	for _, d := range docs {
		v := importedView{Item: cliutil.View(nil, d.Item)}
		if d.From != d.Item.ID {
			v.From = string(d.From)
		}
		r.Imported = append(r.Imported, v)
	}
	return r, nil
}
