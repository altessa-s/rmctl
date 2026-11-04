// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// The values --mode and --paper take, the first of each the default.
var (
	modes  = []port.PDFMode{port.PDFFresh, port.PDFPreserving, port.PDFRecomposed} //nolint:gochecknoglobals // a fixed list
	papers = []port.Paper{port.PaperA4, port.PaperLetter, port.PaperTablet}        //nolint:gochecknoglobals // a fixed list
)

// formats are the values --format takes and the extensions of -o that
// stand for one, and names what each draws in a message.
var (
	formats = map[string]port.Format{ //nolint:gochecknoglobals // a fixed table
		"pdf": port.FormatPDF, "png": port.FormatPNG, "jpg": port.FormatJPEG, "jpeg": port.FormatJPEG, "svg": port.FormatSVG,
	}
	formatNames = map[port.Format]string{ //nolint:gochecknoglobals // a fixed table
		port.FormatPDF: "a PDF", port.FormatPNG: "a PNG", port.FormatJPEG: "a JPEG", port.FormatSVG: "an SVG",
	}
)

// renderView is what render wrote, for --json.
type renderView struct {
	Path          string `json:"path"`
	Pages         int    `json:"pages"`
	SkippedBlocks int    `json:"skipped_blocks"`
	RefusedPages  int    `json:"refused_pages"`
	LinksDropped  int    `json:"links_dropped"`
}

// flags are what render is asked.
type flags struct {
	dest, format, mode, paper, id string
	page, quality                 int
	noTemplate                    bool
	scale                         float64
	tree                          port.RenderTreeOptions
}

// New returns the render command. It changes nothing; a local archive needs
// no cloud, and a cloud document is drawn from the cache when offline.
func New(env *cliutil.Env) *cobra.Command {
	var f flags
	cmd := &cobra.Command{
		Use:     "render <path|id|file.rmdoc> [-o file|dir]",
		GroupID: cliutil.GroupRead,
		Short:   "Draw a document or a folder's documents with their annotations as PDFs, or a page as an image",
		Long: heredoc.Doc(`
			Draws a document the way the tablet shows it: handwriting with its pen
			textures, typed text, highlights, images and page templates, over the
			pages of the PDF or EPUB it was made from. The whole document becomes a
			PDF; --page N draws its page N, from 1, as an image instead.

			--format is what is drawn: pdf, the whole document, or png, jpg (or
			jpeg) or svg, the page --page names. Without it the extension of -o
			decides, when it is one of these; otherwise a page is a PNG and the
			whole document a PDF. A PNG or a JPEG is drawn on white; an SVG has no
			background. --no-template draws the pages without their templates, in
			any format.

			--mode says what becomes of a document made from a PDF (a notebook comes
			out the same in each): fresh, the default, draws a new PDF, keeping links
			and the outline but not forms or other annotations; preserving appends
			the marks to the original PDF, its bytes untouched, and refuses a
			document whose pages were moved, added or hidden; recomposed writes a
			new PDF from the original's objects, keeping forms and annotations, and
			breaks signatures. --paper is the sheet a notebook page is laid out on;
			a page of a PDF keeps its own size. --scale is a PNG's or a JPEG's
			pixels per tablet unit: at 1, the default, a page is 1404 pixels wide,
			as on the tablet's screen. --quality is a JPEG's, from 1 to 100; 0, the
			default, is the renderer's own, 75.

			Without -o the file is written in the working directory under the
			document's name: <name>-rendered.pdf, or <name>-p<N>.png, .jpg or .svg
			for a page; a directory takes that name inside it. The file appears
			only once it is whole. The path written is the answer.

			A folder, or "/" for the whole library, draws every document below it as
			a PDF into a directory, keeping the folder tree: -o names the directory,
			created when missing; without it, a directory named after the folder,
			or remarkable for "/", in the working directory. Each document becomes
			<folder path>/<name>.pdf; two names that collide in one directory get
			the first 8 characters of the later one's id in brackets. --mode,
			--paper and --no-template apply to every document; --page, --scale,
			--quality and image formats do not. Documents in the trash are left out
			unless --all. The directory keeps .rmctl-render.json, a record of what
			each PDF was drawn from and with which --mode, --paper and
			--no-template. --incremental skips a document whose PDF is there with
			the document's modification time, which each PDF written takes, and
			which that record shows drawn from the same document with the same
			options; other options, or a record missing or damaged, draw it again.
			--jobs is how many documents are drawn at a time. A document that
			fails is reported and the others go on; the answer is a row per
			document (rendered, unchanged or failed) and the totals, and the
			command then fails if any did.

			An argument ending with .rmdoc that is a local file is read from that
			archive, offline, as "rmctl export" and "rmctl backup" write one; "./"
			in front, or the identifier, names a cloud document whose name ends with
			.rmdoc. --id picks the document of an archive that holds several.

			Rendering is in the macOS and Linux builds; on Windows the command
			says it is not available.
		`),
		Example: heredoc.Doc(`
			rmctl render /Books/Paper -o paper.pdf
			rmctl render /Notes/Meeting --paper letter
			rmctl render /Books/Paper --mode preserving
			rmctl render 3f2a --page 3 --scale 2 -o page3.png
			rmctl render 3f2a --page 3 --format jpg --quality 90
			rmctl render /Notes/Meeting --page 1 -o page1.svg --no-template
			rmctl render backup.rmdoc -o meeting.pdf
			rmctl render ~/rm-backup/3f2a.rmdoc --page 1
			rmctl render /Clients -o ~/clients-pdf
			rmctl render / -o ~/remarkable-pdf --incremental --jobs 8
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := f.options(cmd)
			if err != nil {
				return err
			}
			local := cliutil.LocalArchive(args[0])
			if opts.Document != "" && !local {
				return cliutil.Usagef("render: --id picks a document of a local .rmdoc archive; name a cloud document by its path or id")
			}
			if local {
				if err = f.onlyFolder(cmd, "a local archive"); err != nil {
					return err
				}
			}
			r, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Rendering { return s.Rendering })
			if err != nil {
				return err
			}
			if local {
				report, rerr := r.RenderFile(cmd.Context(), args[0], f.dest, opts)
				return f.answer(cmd, env, report, rerr)
			}
			folder, err := r.Folder(cmd.Context(), args[0])
			if err != nil {
				return cliutil.PagesErr("render", err)
			}
			if folder {
				return f.renderTree(cmd, env, r, args[0], opts)
			}
			if err = f.onlyFolder(cmd, "a document"); err != nil {
				return err
			}
			report, err := r.Render(cmd.Context(), args[0], f.dest, opts)
			return f.answer(cmd, env, report, err)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.dest, "output", "o", "", "file to write, or a folder's directory (default: <name>-rendered.pdf, <name>-p<N>.<format> or <folder> here)")
	fl.StringVar(&f.format, "format", "", "pdf, png, jpg or svg (default: from -o's extension, else png with --page and pdf without)")
	fl.StringVar(&f.mode, "mode", string(port.PDFFresh), "PDF mode: fresh, preserving or recomposed")
	fl.StringVar(&f.paper, "paper", string(port.PaperA4), "paper of a notebook page: a4, letter or tablet")
	fl.IntVar(&f.page, "page", 0, "the page to draw as an image, from 1")
	fl.Float64Var(&f.scale, "scale", 1, "pixels per tablet unit of a PNG or a JPEG: 1 is 1404 pixels across")
	fl.IntVar(&f.quality, "quality", 0, "quality of a JPEG, 1 to 100 (default: 0, the renderer's, 75)")
	fl.BoolVar(&f.noTemplate, "no-template", false, "draw the pages without their templates")
	fl.StringVar(&f.id, "id", "", "the document of a local archive that holds several: an id or a unique prefix")
	fl.BoolVarP(&f.tree.All, "all", "a", false, "with a folder: draw the documents in the trash too")
	fl.BoolVar(&f.tree.Incremental, "incremental", false, "with a folder: skip a document whose PDF is there, drawn from it as it is with these options")
	fl.IntVarP(&f.tree.Jobs, "jobs", "j", 0, "with a folder: documents at a time (0: the default)")
	return cmd
}

// answer prints what one document drawn wrote, with what the drawing could
// not keep on standard error.
func (f *flags) answer(cmd *cobra.Command, env *cliutil.Env, report port.RenderReport, err error) error {
	if err != nil {
		return cliutil.PagesErr("render", err)
	}
	notes(cmd.ErrOrStderr(), "", report)
	return env.Emit(cmd, output.Fields(renderView{
		Path: report.Path, Pages: report.Pages, SkippedBlocks: report.SkippedBlocks,
		RefusedPages: report.RefusedPages, LinksDropped: report.LinksDropped,
	},
		output.Field{Key: "path", Value: output.Path(report.Path)},
		output.Field{Key: "pages", Value: output.Count(report.Pages)},
		output.Field{Key: "skipped_blocks", Value: output.Count(report.SkippedBlocks)},
		output.Field{Key: "refused_pages", Value: output.Count(report.RefusedPages)},
		output.Field{Key: "links_dropped", Value: output.Count(report.LinksDropped)}))
}

// treeFlags are the flags for a folder alone.
var treeFlags = []string{"all", "incremental", "jobs"} //nolint:gochecknoglobals // a fixed list

// onlyFolder refuses the flags for a folder alone when what is drawn is
// one document.
func (f *flags) onlyFolder(cmd *cobra.Command, what string) error {
	for _, name := range treeFlags {
		if cmd.Flags().Changed(name) {
			return cliutil.Usagef("render: --%s is for a folder; %s is drawn on its own", name, what)
		}
	}
	return nil
}

// renderTree draws every document below the folder query names. The
// answer is written also when some documents failed; the command then
// fails after it.
func (f *flags) renderTree(cmd *cobra.Command, env *cliutil.Env, r port.Rendering, query string, opts port.RenderOptions) error {
	changed := cmd.Flags().Changed
	switch {
	case changed("page"):
		return cliutil.Usagef("render: %s is a folder: its documents are drawn whole as PDFs; leave --page out", query)
	case f.dest != "" && formats[strings.ToLower(strings.TrimPrefix(filepath.Ext(f.dest), "."))] != "":
		return cliutil.Usagef("render: %s is a folder: -o names the directory its PDFs go into, not a file such as %s", query, f.dest)
	}
	report, err := r.RenderTree(cmd.Context(), query, f.dest, opts, f.tree)
	if err != nil && (len(report.Documents) == 0 || cmd.Context().Err() != nil) {
		return cliutil.PagesErr("render", err)
	}
	w := cmd.ErrOrStderr()
	for _, d := range report.Documents {
		notes(w, d.LibraryPath+": ", d.Report)
	}
	if len(report.Documents) == 0 && !env.JSON(cmd) {
		cliutil.Notef(w, "no document to draw below %s", query)
	}
	if eerr := env.Emit(cmd, treeResult(report)); eerr != nil {
		return eerr
	}
	return cliutil.PagesErr("render", err)
}

// treeView is what drawing a folder did, for --json.
type treeView struct {
	Dir       string         `json:"dir"`
	Documents []treeDocument `json:"documents"`
	Rendered  int            `json:"rendered"`
	Unchanged int            `json:"unchanged"`
	Failed    int            `json:"failed"`
}

// treeDocument is one document of a folder drawn.
type treeDocument struct {
	Path          string `json:"path"`
	Document      string `json:"document"`
	ID            string `json:"id"`
	Status        string `json:"status"`
	Pages         int    `json:"pages"`
	SkippedBlocks int    `json:"skipped_blocks"`
	RefusedPages  int    `json:"refused_pages"`
	LinksDropped  int    `json:"links_dropped"`
	Error         string `json:"error,omitempty"`
}

// treeResult is one row per document, then the totals.
func treeResult(r port.RenderTreeReport) output.Result {
	v := treeView{
		Dir: r.Dir, Documents: make([]treeDocument, 0, len(r.Documents)),
		Rendered: r.Count(port.RenderRendered), Unchanged: r.Count(port.RenderUnchanged), Failed: r.Count(port.RenderFailed),
	}
	t := output.Table{Columns: []output.Column{
		{Title: "status", Role: output.RoleKind},
		{Title: "path", Role: output.RolePath},
		{Title: "document", Role: output.RolePath},
		{Title: "pages", Role: output.RoleCount},
		{Title: "error", Role: output.RoleText},
	}}
	for _, d := range r.Documents {
		dv := treeDocument{
			Path: d.Report.Path, Document: d.LibraryPath, ID: string(d.Document.ID), Status: string(d.Status), Pages: d.Report.Pages,
			SkippedBlocks: d.Report.SkippedBlocks, RefusedPages: d.Report.RefusedPages, LinksDropped: d.Report.LinksDropped,
		}
		pages := output.Count(d.Report.Pages)
		if d.Status != port.RenderRendered {
			pages = output.Text("")
		}
		if d.Err != nil {
			dv.Error = d.Err.Error()
		}
		v.Documents = append(v.Documents, dv)
		t.Rows = append(t.Rows, []output.Cell{
			output.Text(dv.Status), output.Path(dv.Path), output.Path(dv.Document), pages, output.Text(dv.Error),
		})
	}
	total := output.Record{Fields: []output.Field{
		{Key: "dir", Value: output.Path(r.Dir)},
		{Key: "documents", Value: output.Count(len(r.Documents))},
		{Key: "rendered", Value: output.Count(v.Rendered)},
		{Key: "unchanged", Value: output.Count(v.Unchanged)},
		{Key: "failed", Value: output.Count(v.Failed)},
	}}
	return output.Result{JSON: v, Doc: output.Doc{t, total}}
}

// options are the flags as render options, or a usage error for a
// combination that cannot be drawn.
func (f *flags) options(cmd *cobra.Command) (port.RenderOptions, error) {
	changed := cmd.Flags().Changed
	opts := port.RenderOptions{Paper: port.Paper(strings.ToLower(f.paper)), Document: f.id, NoTemplates: f.noTemplate}
	if !slices.Contains(papers, opts.Paper) {
		return opts, cliutil.Usagef("render: unknown --paper %q: use a4, letter or tablet", f.paper)
	}
	format, err := f.chosenFormat(changed)
	if err != nil {
		return opts, err
	}
	opts.Format = format
	image := format == port.FormatPNG || format == port.FormatJPEG
	switch {
	case format == port.FormatPDF && changed("page"):
		return opts, cliutil.Usagef("render: a PDF is the whole document: leave --page out, or draw the page with --format png, jpg or svg")
	case format != port.FormatPDF && !changed("page"):
		return opts, cliutil.Usagef("render: %s is one page: name it with --page N, or draw the whole document with --format pdf", formatNames[format])
	case changed("scale") && !image:
		return opts, cliutil.Usagef("render: --scale is for a page drawn as a PNG or a JPEG")
	case changed("quality") && format != port.FormatJPEG:
		return opts, cliutil.Usagef("render: --quality is for a page drawn as a JPEG")
	case changed("mode") && format != port.FormatPDF:
		return opts, cliutil.Usagef("render: --mode is for a whole document drawn as a PDF")
	}
	if format == port.FormatPDF {
		opts.Mode = port.PDFMode(strings.ToLower(f.mode))
		if !slices.Contains(modes, opts.Mode) {
			return opts, cliutil.Usagef("render: unknown --mode %q: use fresh, preserving or recomposed", f.mode)
		}
		return opts, nil
	}
	if f.page < 1 {
		return opts, cliutil.Usagef("render: --page %d: pages count from 1", f.page)
	}
	opts.Page = f.page
	if !image {
		return opts, nil
	}
	if f.scale <= 0 {
		return opts, cliutil.Usagef("render: --scale %g: it must be above 0", f.scale)
	}
	if f.quality < 0 || f.quality > 100 {
		return opts, cliutil.Usagef("render: --quality %d: use 1 to 100, or 0 for the default", f.quality)
	}
	opts.Scale, opts.Quality = f.scale, f.quality
	return opts, nil
}

// chosenFormat is the format --format names; without it, the one the
// extension of -o stands for, or else a PNG with --page and a PDF without.
func (f *flags) chosenFormat(changed func(string) bool) (port.Format, error) {
	if changed("format") {
		format, ok := formats[strings.ToLower(f.format)]
		if !ok {
			return "", cliutil.Usagef("render: unknown --format %q: use pdf, png, jpg or svg", f.format)
		}
		return format, nil
	}
	if format, ok := formats[strings.ToLower(strings.TrimPrefix(filepath.Ext(f.dest), "."))]; ok {
		return format, nil
	}
	if changed("page") {
		return port.FormatPNG, nil
	}
	return port.FormatPDF, nil
}

// notes says on w what the drawing could not keep, each line after
// prefix.
func notes(w io.Writer, prefix string, r port.RenderReport) {
	if r.SkippedBlocks > 0 {
		cliutil.Notef(w, "%s%d blocks of the pages did not parse and were left out", prefix, r.SkippedBlocks)
	}
	if r.RefusedPages > 0 {
		cliutil.Notef(w, "%s%d pages name a page the original PDF does not have; they are drawn without it", prefix, r.RefusedPages)
	}
	if r.LinksDropped > 0 {
		cliutil.Notef(w, "%s%d links of the original PDF could not be carried over; --mode preserving or recomposed keeps them", prefix, r.LinksDropped)
	}
}
