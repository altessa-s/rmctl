// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
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
		ValidArgsFunction: cliutil.CompleteRemote(env, false),
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
			if cliutil.JSON(cmd) {
				return cliutil.WriteJSON(cmd.OutOrStdout(), pageViews(doc.Pages, raw))
			}
			return printPages(cmd.OutOrStdout(), doc.Pages, raw)
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

// counts writes a map as "name n, name n", largest first, names breaking ties.
func counts(m map[string]int) string {
	if len(m) == 0 {
		return "-"
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
	return strings.Join(parts, ", ")
}

func paper(s domain.PageStats) string {
	if s.PaperWidth == 0 || s.PaperHeight == 0 {
		return "-"
	}
	return strconv.FormatFloat(s.PaperWidth, 'f', -1, 64) + "×" + strconv.FormatFloat(s.PaperHeight, 'f', -1, 64)
}

func printPages(w io.Writer, pages []domain.Page, raw bool) error {
	var b strings.Builder
	for i, p := range pages {
		if i > 0 {
			b.WriteByte('\n')
		}
		s := p.Stats
		fmt.Fprintf(&b, "page %d  %s\n", p.Number, p.ID)
		fmt.Fprintf(&b, "  paragraphs  %d\n", len(p.Paragraphs))
		fmt.Fprintf(&b, "  highlights  %d\n", len(p.Highlights))
		fmt.Fprintf(&b, "  strokes     %d (%s)\n", s.Strokes, counts(s.StrokesByTool))
		fmt.Fprintf(&b, "  colors      %s\n", counts(s.StrokesByColor))
		fmt.Fprintf(&b, "  anchored    %d\n", s.AnchoredStrokes)
		fmt.Fprintf(&b, "  images      %d\n", s.Images)
		fmt.Fprintf(&b, "  skipped     %d\n", s.SkippedBlocks)
		fmt.Fprintf(&b, "  paper       %s\n", paper(s))
		if !raw {
			continue
		}
		for _, para := range p.Paragraphs {
			fmt.Fprintf(&b, "  ¶ [%d] %s\n", para.Style, para.Text)
		}
		for _, h := range p.Highlights {
			fmt.Fprintf(&b, "  ▌ %s %s %s\n", h.ID, h.Color, h.Text)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
