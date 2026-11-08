// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"fmt"
	"io"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// The output formats of the page commands.
const (
	formatText     = "text"
	formatMarkdown = "markdown"
	formatJSON     = "json"
	// formatNote is a markdown note for a notes app, highlights alone.
	formatNote = "note"
)

func pagesService(cmd *cobra.Command, env *cliutil.Env) (port.Pages, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Pages { return s.Pages })
}

// pageFlags are the flags text and highlights share.
type pageFlags struct {
	pages  []string
	format string
	// note says the command writes notes too: highlights does.
	note bool
}

func (f *pageFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.pages, "page", nil, "only this page: a number from 1 or a page id (repeat or comma-separate)")
	formats := "text, markdown, json"
	if f.note {
		formats = "text, markdown, note, json"
	}
	cmd.Flags().StringVar(&f.format, "format", formatText, "output format: "+formats)
}

// resolved is the output format: --json, else this command's own --format,
// else json when $RMCTL_FORMAT asks for it, else text. The global formats
// table and plain are for records; a document's text has its own.
func (f *pageFlags) resolved(cmd *cobra.Command, env *cliutil.Env) (string, error) {
	if asJSON, _ := cmd.Flags().GetBool(cliutil.FlagJSON); asJSON { //nolint:errcheck // the flag is persistent on the root
		return formatJSON, nil
	}
	if !cmd.Flags().Changed("format") && env.JSON(cmd) {
		return formatJSON, nil
	}
	switch {
	case f.format == formatText, f.format == formatMarkdown, f.format == formatJSON, f.note && f.format == formatNote:
		return f.format, nil
	case f.note:
		return "", cliutil.Usagef("%s: unknown --format %q: use text, markdown, note or json", cmd.CommandPath(), f.format)
	default:
		return "", cliutil.Usagef("%s: unknown --format %q: use text, markdown or json", cmd.CommandPath(), f.format)
	}
}

// load reads the pages of the document query names in the cloud, or of
// every document of a local .rmdoc archive. local says which it was.
func (f *pageFlags) load(cmd *cobra.Command, env *cliutil.Env, query string) (docs []port.DocumentText, format string, local bool, err error) {
	if format, err = f.resolved(cmd, env); err != nil {
		return nil, "", false, err
	}
	selected := cliutil.SplitPages(f.pages)
	if cliutil.LocalArchive(query) {
		a, nerr := cliutil.Need(cmd, env, func(s cliutil.Services) port.Archives { return s.Archives })
		if nerr != nil {
			return nil, "", true, nerr
		}
		docs, perr := a.Pages(cmd.Context(), query, selected)
		if perr != nil {
			return nil, "", true, cliutil.PagesErr(cmd.Name(), perr)
		}
		return docs, format, true, nil
	}
	p, err := pagesService(cmd, env)
	if err != nil {
		return nil, "", false, err
	}
	doc, err := p.Pages(cmd.Context(), query, selected)
	if err != nil {
		return nil, "", false, cliutil.PagesErr(cmd.Name(), err)
	}
	return []port.DocumentText{doc}, format, false, nil
}

// heading separates the documents of an archive in text and markdown.
func heading(out io.Writer, docs []port.DocumentText, i int, format string) error {
	if len(docs) <= 1 {
		return nil
	}
	sep := ""
	if i > 0 {
		sep = "\n"
	}
	mark := "=="
	if format == formatMarkdown {
		mark = "#"
		_, err := fmt.Fprintf(out, "%s%s %s\n\n", sep, mark, docs[i].Item.Name)
		return err
	}
	_, err := fmt.Fprintf(out, "%s%s %s %s\n", sep, mark, docs[i].Item.Name, mark)
	return err
}

// writeJSON writes one document as an object, and the documents of an
// archive as an array whatever their number, so a shape never depends on
// the count.
func writeJSON[V any](out io.Writer, docs []port.DocumentText, local bool, view func(port.DocumentText) V) error {
	if !local {
		return cliutil.WriteJSON(out, view(docs[0]))
	}
	views := make([]V, len(docs))
	for i, d := range docs {
		views[i] = view(d)
	}
	return cliutil.WriteJSON(out, views)
}

func newText(env *cliutil.Env) *cobra.Command {
	var f pageFlags
	cmd := &cobra.Command{
		Use:     "text <path|id|file.rmdoc>",
		GroupID: GroupID,
		Short:   "Print the typed text of a notebook",
		Long: heredoc.Doc(`
			Prints the typed text of a notebook, page by page, as the tablet keeps it.
			Handwriting is ink, not text, and is not recognized. --format markdown
			turns the paragraph styles into markdown: headings, bold lines, bullets,
			numbered lists and checkboxes. --page keeps some pages, by number from 1
			or by page id. An argument ending with .rmdoc that is a local file is
			read from that archive, offline, every document of it in turn.
		`),
		Example: heredoc.Doc(`
			rmctl text /Notes/Meeting
			rmctl text /Notes/Meeting --format markdown > meeting.md
			rmctl text 3f2a --page 2,3 --json
			rmctl text backup.rmdoc --format markdown
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			docs, format, local, err := f.load(cmd, env, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if format == formatJSON {
				return writeJSON(out, docs, local, textView)
			}
			for i, doc := range docs {
				if !hasText(doc) {
					cliutil.Notef(cmd.ErrOrStderr(), "%s has no typed text", doc.Item.Name)
				}
				if err := heading(out, docs, i, format); err != nil {
					return err
				}
				text := PlainText(doc.Pages)
				if format == formatMarkdown {
					text = Markdown(doc.Pages)
				}
				if _, err := io.WriteString(out, text); err != nil {
					return err
				}
			}
			return nil
		},
	}
	f.add(cmd)
	return cmd
}

func hasText(doc port.DocumentText) bool {
	for _, p := range doc.Pages {
		for _, para := range p.Paragraphs {
			if strings.TrimSpace(para.Text) != "" {
				return true
			}
		}
	}
	return false
}

// StyleName is the stable name of a paragraph style in JSON.
func StyleName(s domain.ParagraphStyle) string {
	switch s {
	case domain.StylePlain:
		return "plain"
	case domain.StyleHeading:
		return "heading"
	case domain.StyleBold:
		return "bold"
	case domain.StyleBullet:
		return "bullet"
	case domain.StyleBulletNested:
		return "bullet-nested"
	case domain.StyleCheckbox:
		return "checkbox"
	case domain.StyleCheckboxDone:
		return "checkbox-done"
	case domain.StyleNumbered:
		return "numbered"
	case domain.StyleNumberedNested:
		return "numbered-nested"
	default:
		return "plain"
	}
}

// PlainText is the pages' paragraphs as they are, a "Page N" line before
// each page when there are several.
func PlainText(pages []domain.Page) string {
	var b strings.Builder
	for i, p := range pages {
		if len(pages) > 1 {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "Page %d\n", p.Number)
		}
		for _, para := range p.Paragraphs {
			b.WriteString(para.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Markdown is the pages as markdown: a "## Page N" heading per page when
// there are several, and each page's paragraphs as [domain.PageMarkdown]
// writes them.
func Markdown(pages []domain.Page) string {
	var b strings.Builder
	for i, p := range pages {
		if len(pages) > 1 {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "## Page %d\n\n", p.Number)
		}
		b.WriteString(domain.PageMarkdown(p, "#"))
	}
	return b.String()
}

// HighlightsText is one line per highlight: page, color, words.
func HighlightsText(pages []domain.Page) string {
	var b strings.Builder
	for _, p := range pages {
		for _, h := range p.Highlights {
			fmt.Fprintf(&b, "p.%d  %s  %s\n", p.Number, colorOr(h.Color), domain.HighlightText(h))
		}
	}
	return b.String()
}

// HighlightsMarkdown is a "## Page N" heading for each page that has
// highlights, and each highlight as a quote.
func HighlightsMarkdown(pages []domain.Page) string {
	var b strings.Builder
	first := true
	for _, p := range pages {
		if len(p.Highlights) == 0 {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		fmt.Fprintf(&b, "## Page %d\n", p.Number)
		for _, h := range p.Highlights {
			fmt.Fprintf(&b, "\n> %s\n", strings.ReplaceAll(domain.HighlightText(h), "\n", "\n> "))
		}
	}
	return b.String()
}

func colorOr(c string) string {
	if c == "" {
		return "unknown"
	}
	return c
}

type paragraphView struct {
	Text  string `json:"text"`
	Style string `json:"style"`
}

type textPageView struct {
	Number     int             `json:"number"`
	ID         string          `json:"id"`
	Paragraphs []paragraphView `json:"paragraphs"`
}

type textDocView struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Pages []textPageView `json:"pages"`
}

func textView(doc port.DocumentText) textDocView {
	v := textDocView{ID: string(doc.Item.ID), Name: doc.Item.Name, Pages: []textPageView{}}
	for _, p := range doc.Pages {
		pv := textPageView{Number: p.Number, ID: p.ID, Paragraphs: []paragraphView{}}
		for _, para := range p.Paragraphs {
			pv.Paragraphs = append(pv.Paragraphs, paragraphView{Text: para.Text, Style: StyleName(para.Style)})
		}
		v.Pages = append(v.Pages, pv)
	}
	return v
}

type highlightView struct {
	Page    int    `json:"page"`
	PageID  string `json:"page_id"`
	ID      string `json:"id"`
	Color   string `json:"color"`
	Text    string `json:"text"`
	HasText bool   `json:"has_text"`
}

type highlightsDocView struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Highlights []highlightView `json:"highlights"`
}

func highlightsView(doc port.DocumentText) highlightsDocView {
	v := highlightsDocView{ID: string(doc.Item.ID), Name: doc.Item.Name, Highlights: []highlightView{}}
	for _, p := range doc.Pages {
		for _, h := range p.Highlights {
			v.Highlights = append(v.Highlights, highlightView{
				Page: p.Number, PageID: p.ID, ID: h.ID, Color: colorOr(h.Color), Text: h.Text, HasText: h.HasText,
			})
		}
	}
	return v
}
