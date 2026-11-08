// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// openView is what open showed, for --json.
type openView struct {
	Path            string `json:"path"`
	Drawn           bool   `json:"drawn"`
	Pages           int    `json:"pages"`
	SkippedBlocks   int    `json:"skipped_blocks"`
	RefusedPages    int    `json:"refused_pages"`
	LinksDropped    int    `json:"links_dropped"`
	UnreadablePages int    `json:"unreadable_pages"`
}

// NewOpen returns the open command. It changes nothing in the library; a
// local archive needs no cloud, and a cloud document is drawn from the
// cache when offline.
func NewOpen(env *cliutil.Env) *cobra.Command {
	f := flags{name: "open"}
	cmd := &cobra.Command{
		Use:     "open <path|id|file.rmdoc>",
		GroupID: cliutil.GroupRead,
		Short:   "Draw a document as render does and show it in the system's viewer",
		Long: heredoc.Doc(`
			Draws a document as "rmctl render" does, a PDF of the whole document by
			default or the page --page N as an image, and shows the file in the
			viewer the system opens such files with: open on macOS, the shell's
			open action on Windows, xdg-open elsewhere. rmctl does not wait for the
			viewer. The path of the file shown is the answer.

			--format, --mode, --grain, --paper, --page, --scale, --quality,
			--no-template, --unreadable-pages and --id are those of render. A
			folder is not drawn: name a document.

			The drawing is kept in the profile's cache, counted against its
			budget like the files the cache keeps, under the document's name, and
			is shown again without drawing or downloading when the document has not
			changed since and the options are the same: another version of the
			document, other options or another version of rmctl draw it again. The
			least recently used drawings go first when the cache passes its budget,
			and "rmctl debug cache clear" removes them with the rest. With
			--no-cache every open draws into a new directory in the system's
			temporary directory, which the system clears in its own time.

			An argument ending with .rmdoc that is a local file is read from that
			archive, offline; its size and modification time stand for its version.

			Drawing is in the macOS, Linux and Windows builds of rmctl. rmctl-lite,
			the build without the renderer, has the command, which says drawing is
			not available, as render does: to see a document's original PDF or
			EPUB there, "rmctl get" it.
		`),
		Example: heredoc.Doc(`
			rmctl open /Books/Paper
			rmctl open /Notes/Meeting --paper letter --no-template
			rmctl open 3f2a --page 3
			rmctl open 3f2a --page 3 --format svg
			rmctl open ~/rm-backup/3f2a.rmdoc
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.OrFiles(cliutil.CompleteRemote(env, false), "rmdoc")),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := f.options(cmd)
			if err != nil {
				return err
			}
			local := cliutil.LocalArchive(args[0])
			if opts.Document != "" && !local {
				return cliutil.Usagef("open: --id picks a document of a local .rmdoc archive; name a cloud document by its path or id")
			}
			v, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Viewing { return s.Viewing })
			if err != nil {
				return err
			}
			var report port.OpenReport
			if local {
				report, err = v.OpenFile(cmd.Context(), args[0], opts)
			} else {
				report, err = v.Open(cmd.Context(), args[0], opts)
			}
			if report.Path == "" {
				return cliutil.PagesErr("open", err)
			}
			notes(cmd.ErrOrStderr(), "", report.RenderReport, port.Unreadable(strings.ToLower(f.unreadable)))
			if eerr := env.Emit(cmd, openResult(report)); eerr != nil {
				return eerr
			}
			if err != nil {
				return fmt.Errorf("open: the drawing is at %s, but it could not be shown: %w", report.Path, err)
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.format, "format", "", "pdf, png, jpg or svg (default: png with --page, pdf without)")
	fl.StringVar(&f.mode, "mode", string(port.PDFFresh), "PDF mode: fresh, preserving or recomposed")
	fl.StringVar(&f.grain, "grain", string(port.PDFGrainMask), "PDF drawing of pencil and brush grain: mask or group")
	fl.StringVar(&f.paper, "paper", string(port.PaperA4), "paper of a notebook page: a4, letter or tablet")
	fl.IntVar(&f.page, "page", 0, "the page to draw as an image, from 1")
	fl.Float64Var(&f.scale, "scale", 1, "pixels per tablet unit of a PNG or a JPEG: 1 is 1404 pixels across")
	fl.IntVar(&f.quality, "quality", 0, "quality of a JPEG, 1 to 100 (default: 0, the renderer's, 75)")
	fl.BoolVar(&f.noTemplate, "no-template", false, "draw the pages without their templates")
	fl.StringVar(&f.unreadable, "unreadable-pages", string(port.UnreadablePlaceholder), "a page that does not parse: placeholder, blank or strict")
	fl.StringVar(&f.id, "id", "", "the document of a local archive that holds several: an id or a unique prefix")
	completeValues(cmd)
	return cmd
}

// openResult is the file shown, whether it was drawn now, and what the
// drawing counted; a drawing kept from before counts nothing.
func openResult(r port.OpenReport) output.Result {
	return output.Fields(openView{
		Path: r.Path, Drawn: r.Drawn, Pages: r.Pages, SkippedBlocks: r.SkippedBlocks,
		RefusedPages: r.RefusedPages, LinksDropped: r.LinksDropped, UnreadablePages: r.UnreadablePages,
	},
		output.Field{Key: keyPath, Value: output.Path(r.Path)},
		output.Field{Key: "drawn", Value: output.Bool(r.Drawn)},
		output.Field{Key: keyPages, Value: output.Count(r.Pages)},
		output.Field{Key: "unreadable_pages", Value: output.Count(r.UnreadablePages)})
}
