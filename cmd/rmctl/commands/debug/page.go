// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func newPage(env *cliutil.Env) *cobra.Command {
	var pages []string
	var raw bool
	cmd := &cobra.Command{
		Use:   "page <path|id>",
		Short: "What the .rm pages of a document hold",
		Long: heredoc.Doc(`
			Reads every .rm page of a document and says what each holds: paragraphs
			of typed text, highlights, strokes by tool and by color, strokes anchored
			to the text, images, the blocks the reader could not understand, and the
			paper size the page declares. Only counts are printed: --raw adds the
			paragraphs and the highlighted words, which are the user's own text and
			not for a report.
		`),
		Example: heredoc.Doc(`
			rmctl debug page /Notes/Meeting
			rmctl debug page 3f2a --page 1 --json
		`),
		Args:              cliutil.Args(cobra.ExactArgs(1)),
		ValidArgsFunction: cliutil.UpTo(1, cliutil.CompleteRemote(env, false)),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Pages { return s.Pages })
			if err != nil {
				return err
			}
			doc, err := p.Pages(cmd.Context(), args[0], cliutil.SplitPages(pages))
			if err != nil {
				return cliutil.PagesErr("debug page", err)
			}
			return env.Emit(cmd, output.Result{JSON: pageViews(doc.Pages, raw), Doc: pagesDoc(doc.Pages, raw)})
		},
	}
	cmd.Flags().StringArrayVar(&pages, "page", nil, "only this page: a number from 1 or a page id (repeat or comma-separate)")
	cmd.Flags().BoolVar(&raw, "raw", false, "also print the paragraphs and the highlighted words")
	return cmd
}

type pageParagraphView struct {
	Text  string `json:"text"`
	Style int    `json:"style"`
}

type pageHighlightView struct {
	ID    string `json:"id"`
	Color string `json:"color"`
	Rects int    `json:"rects"`
	Text  string `json:"text,omitempty"`
}

type pageView struct {
	Number          int                 `json:"number"`
	ID              string              `json:"id"`
	Paragraphs      int                 `json:"paragraphs"`
	Highlights      int                 `json:"highlights"`
	Strokes         int                 `json:"strokes"`
	StrokesByTool   map[string]int      `json:"strokes_by_tool"`
	StrokesByColor  map[string]int      `json:"strokes_by_color"`
	AnchoredStrokes int                 `json:"anchored_strokes"`
	Images          int                 `json:"images"`
	SkippedBlocks   int                 `json:"skipped_blocks"`
	PaperWidth      float64             `json:"paper_width"`
	PaperHeight     float64             `json:"paper_height"`
	Text            []pageParagraphView `json:"text,omitempty"`
	Marks           []pageHighlightView `json:"marks,omitempty"`
}

func pageViews(pages []domain.Page, raw bool) []pageView {
	out := make([]pageView, 0, len(pages))
	for _, p := range pages {
		s := p.Stats
		v := pageView{
			Number: p.Number, ID: p.ID, Paragraphs: len(p.Paragraphs), Highlights: len(p.Highlights),
			Strokes: s.Strokes, StrokesByTool: orEmpty(s.StrokesByTool), StrokesByColor: orEmpty(s.StrokesByColor),
			AnchoredStrokes: s.AnchoredStrokes, Images: s.Images, SkippedBlocks: s.SkippedBlocks,
			PaperWidth: s.PaperWidth, PaperHeight: s.PaperHeight,
		}
		for _, h := range p.Highlights {
			hv := pageHighlightView{ID: h.ID, Color: h.Color, Rects: len(h.Rects)}
			if raw {
				hv.Text = h.Text
			}
			v.Marks = append(v.Marks, hv)
		}
		if raw {
			for _, para := range p.Paragraphs {
				v.Text = append(v.Text, pageParagraphView{Text: para.Text, Style: int(para.Style)})
			}
		}
		out = append(out, v)
	}
	return out
}

func orEmpty(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

// counts writes a map as "name n,name n", largest first, names breaking
// ties; "" when it is empty.
func counts(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	keys := slices.SortedFunc(maps.Keys(m), func(a, b string) int {
		if c := cmp.Compare(m[b], m[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" "+strconv.Itoa(m[k]))
	}
	return strings.Join(parts, ",")
}

func paper(s domain.PageStats) string {
	if s.PaperWidth == 0 || s.PaperHeight == 0 {
		return ""
	}
	return strconv.FormatFloat(s.PaperWidth, 'f', -1, 64) + "×" + strconv.FormatFloat(s.PaperHeight, 'f', -1, 64)
}

// pagesDoc is each page as a record of what it holds; --raw adds its
// paragraphs and its highlights as tables.
func pagesDoc(pages []domain.Page, raw bool) output.Doc {
	doc := make(output.Doc, 0, len(pages))
	for _, p := range pages {
		s := p.Stats
		doc = append(doc, output.Record{Fields: []output.Field{
			{Key: "number", Value: output.Count(p.Number)},
			{Key: "id", Value: output.ID(p.ID)},
			{Key: "paragraphs", Value: output.Count(len(p.Paragraphs))},
			{Key: "highlights", Value: output.Count(len(p.Highlights))},
			{Key: "strokes", Value: output.Count(s.Strokes)},
			{Key: "strokes_by_tool", Value: output.Text(counts(s.StrokesByTool))},
			{Key: "strokes_by_color", Value: output.Text(counts(s.StrokesByColor))},
			{Key: "anchored_strokes", Value: output.Count(s.AnchoredStrokes)},
			{Key: "images", Value: output.Count(s.Images)},
			{Key: "skipped_blocks", Value: output.Count(s.SkippedBlocks)},
			{Key: "paper", Value: output.Text(paper(s))},
		}})
		if !raw {
			continue
		}
		text := output.Table{Columns: []output.Column{{Title: "style", Role: output.RoleCount}, {Title: "text"}}}
		for _, para := range p.Paragraphs {
			text.Rows = append(text.Rows, []output.Cell{output.Count(int(para.Style)), output.Text(para.Text)})
		}
		marks := output.Table{Columns: []output.Column{{Title: "highlight", Role: output.RoleID}, {Title: "color"}, {Title: "text"}}}
		for _, h := range p.Highlights {
			marks.Rows = append(marks.Rows, []output.Cell{output.ID(h.ID), output.Text(h.Color), output.Text(h.Text)})
		}
		doc = append(doc, text, marks)
	}
	return doc
}
