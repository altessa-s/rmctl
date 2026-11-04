// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
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
	dest, mode, paper, id string
	page                  int
	png                   bool
	scale                 float64
}

// New returns the render command. It changes nothing; a local archive needs
// no cloud, and a cloud document is drawn from the cache when offline.
func New(env *cliutil.Env) *cobra.Command {
	var f flags
	cmd := &cobra.Command{
		Use:     "render <path|id|file.rmdoc> [-o file]",
		GroupID: cliutil.GroupRead,
		Short:   "Draw a document with its annotations as a PDF, or a page as a PNG",
		Long: heredoc.Doc(`
			Draws a document the way the tablet shows it: handwriting with its pen
			textures, typed text, highlights, images and page templates, over the
			pages of the PDF or EPUB it was made from. The whole document becomes a
			PDF; --page N --png draws its page N, from 1, as a PNG instead.

			--mode says what becomes of a document made from a PDF (a notebook comes
			out the same in each): fresh, the default, draws a new PDF, keeping links
			and the outline but not forms or other annotations; preserving appends
			the marks to the original PDF, its bytes untouched, and refuses a
			document whose pages were moved, added or hidden; recomposed writes a
			new PDF from the original's objects, keeping forms and annotations, and
			breaks signatures. --paper is the sheet a notebook page is laid out on;
			a page of a PDF keeps its own size. --scale is a PNG's pixels per tablet
			unit: at 1, the default, a page is 1404 pixels wide, as on the tablet's
			screen.

			Without -o the file is written in the working directory under the
			document's name: <name>-rendered.pdf, or <name>-p<N>.png for a page; a
			directory takes that name inside it. The file appears only once it is
			whole. The path written is the answer.

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
			rmctl render 3f2a --page 3 --png --scale 2 -o page3.png
			rmctl render backup.rmdoc -o meeting.pdf
			rmctl render ~/rm-backup/3f2a.rmdoc --page 1 --png
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
			r, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Rendering { return s.Rendering })
			if err != nil {
				return err
			}
			var report port.RenderReport
			if local {
				report, err = r.RenderFile(cmd.Context(), args[0], f.dest, opts)
			} else {
				report, err = r.Render(cmd.Context(), args[0], f.dest, opts)
			}
			if err != nil {
				return cliutil.PagesErr("render", err)
			}
			notes(cmd, report)
			return env.Emit(cmd, output.Fields(renderView{
				Path: report.Path, Pages: report.Pages, SkippedBlocks: report.SkippedBlocks,
				RefusedPages: report.RefusedPages, LinksDropped: report.LinksDropped,
			},
				output.Field{Key: "path", Value: output.Path(report.Path)},
				output.Field{Key: "pages", Value: output.Count(report.Pages)},
				output.Field{Key: "skipped_blocks", Value: output.Count(report.SkippedBlocks)},
				output.Field{Key: "refused_pages", Value: output.Count(report.RefusedPages)},
				output.Field{Key: "links_dropped", Value: output.Count(report.LinksDropped)}))
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.dest, "output", "o", "", "file to write (default: <name>-rendered.pdf or <name>-p<N>.png here)")
	fl.StringVar(&f.mode, "mode", string(port.PDFFresh), "PDF mode: fresh, preserving or recomposed")
	fl.StringVar(&f.paper, "paper", string(port.PaperA4), "paper of a notebook page: a4, letter or tablet")
	fl.IntVar(&f.page, "page", 0, "the page to draw as a PNG, from 1 (with --png)")
	fl.BoolVar(&f.png, "png", false, "draw the page --page names as a PNG")
	fl.Float64Var(&f.scale, "scale", 1, "pixels per tablet unit of a PNG: 1 is 1404 pixels across")
	fl.StringVar(&f.id, "id", "", "the document of a local archive that holds several: an id or a unique prefix")
	return cmd
}

// options are the flags as render options, or a usage error for a
// combination that cannot be drawn.
func (f *flags) options(cmd *cobra.Command) (port.RenderOptions, error) {
	changed := cmd.Flags().Changed
	opts := port.RenderOptions{Paper: port.Paper(strings.ToLower(f.paper)), Document: f.id}
	if !slices.Contains(papers, opts.Paper) {
		return opts, cliutil.Usagef("render: unknown --paper %q: use a4, letter or tablet", f.paper)
	}
	switch {
	case f.png && !changed("page"):
		return opts, cliutil.Usagef("render: --png draws one page: name it with --page N")
	case changed("page") && !f.png:
		return opts, cliutil.Usagef("render: --page draws one page as a PNG: add --png, or leave --page out for the whole document as a PDF")
	case !f.png && changed("scale"):
		return opts, cliutil.Usagef("render: --scale is for a page drawn with --png")
	case f.png && changed("mode"):
		return opts, cliutil.Usagef("render: --mode is for a whole document drawn as a PDF")
	}
	if !f.png {
		opts.Mode = port.PDFMode(strings.ToLower(f.mode))
		if !slices.Contains(modes, opts.Mode) {
			return opts, cliutil.Usagef("render: unknown --mode %q: use fresh, preserving or recomposed", f.mode)
		}
		return opts, nil
	}
	if f.page < 1 {
		return opts, cliutil.Usagef("render: --page %d: pages count from 1", f.page)
	}
	if f.scale <= 0 {
		return opts, cliutil.Usagef("render: --scale %g: it must be above 0", f.scale)
	}
	opts.Page, opts.Scale = f.page, f.scale
	return opts, nil
}

// notes says on standard error what the drawing could not keep.
func notes(cmd *cobra.Command, r port.RenderReport) {
	w := cmd.ErrOrStderr()
	if r.SkippedBlocks > 0 {
		cliutil.Notef(w, "%d blocks of the pages did not parse and were left out", r.SkippedBlocks)
	}
	if r.RefusedPages > 0 {
		cliutil.Notef(w, "%d pages name a page the original PDF does not have; they are drawn without it", r.RefusedPages)
	}
	if r.LinksDropped > 0 {
		cliutil.Notef(w, "%d links of the original PDF could not be carried over; --mode preserving or recomposed keeps them", r.LinksDropped)
	}
}
