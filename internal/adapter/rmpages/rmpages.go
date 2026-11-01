// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build cgo

package rmpages

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmlines "github.com/altessa-s/rmlines-go"
)

// Pages implements the page ports on rmlines-go.
type Pages struct{}

var (
	_ port.PageReader = Pages{}
	_ port.PageEditor = Pages{}
	_ port.PageWriter = Pages{}
)

// loggerOnce routes rmlines-lib's diagnostics once per process: the C
// library keeps one sink.
var loggerOnce sync.Once //nolint:gochecknoglobals // the C library's sink is process-wide

// New returns the page adapter. log receives what the library steps over in
// a page; nil discards it. Only the first logger of a process is used.
func New(log *slog.Logger) Pages {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	loggerOnce.Do(func() {
		rmlines.SetLogger(func(level rmlines.LogLevel, message string) {
			if level == rmlines.LogError {
				log.Warn("rm page", "detail", message)
				return
			}
			log.Debug("rm page", "detail", message)
		})
	})
	return Pages{}
}

// ReadPage implements port.PageReader.
func (Pages) ReadPage(data []byte) (domain.Page, error) {
	page, err := rmlines.Parse(data)
	if err != nil {
		return domain.Page{}, mapErr(err)
	}
	out := domain.Page{
		Stats: domain.PageStats{
			Strokes:         len(page.Strokes),
			StrokesByTool:   map[string]int{},
			StrokesByColor:  map[string]int{},
			AnchoredStrokes: page.AnchoredStrokes,
			Images:          len(page.Images),
			SkippedBlocks:   page.SkippedBlocks,
		},
	}
	if page.PaperSize != nil {
		out.Stats.PaperWidth, out.Stats.PaperHeight = page.PaperSize.Width, page.PaperSize.Height
	}
	marks, err := rmlines.HighlighterItems(data)
	if err != nil {
		return domain.Page{}, mapErr(err)
	}
	out.Stats.Marks = len(marks)
	for _, s := range page.Strokes {
		out.Stats.StrokesByTool[s.Tool.String()]++
		out.Stats.StrokesByColor[s.Color.String()]++
	}
	if page.Text != nil {
		out.Paragraphs = make([]domain.Paragraph, 0, len(page.Text.Paragraphs))
		for _, p := range page.Text.Paragraphs {
			style := domain.StylePlain
			if p.Format != nil && p.Format.Style() != rmlines.StyleUnknown {
				style = domain.ParagraphStyle(p.Format.Style())
			}
			out.Paragraphs = append(out.Paragraphs, domain.Paragraph{Text: p.Text, Style: style})
		}
	}
	for _, h := range page.Highlights {
		hl := domain.Highlight{ID: h.ID.String(), Color: h.Color.String()}
		if h.Text != nil {
			hl.Text, hl.HasText = *h.Text, true
		}
		for _, r := range h.Rectangles {
			hl.Rects = append(hl.Rects, domain.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height})
		}
		out.Highlights = append(out.Highlights, hl)
	}
	return out, nil
}

// Highlights implements port.PageEditor: every living mark of the
// highlighter, its strokes and its highlights over a text layer.
func (Pages) Highlights(data []byte) ([]string, error) {
	ids, err := rmlines.HighlighterItems(data)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out, nil
}

// Erase implements port.PageEditor. The library reads the result back
// before it answers; a refusal means nothing was written.
func (Pages) Erase(data []byte, ids []string) ([]byte, error) {
	items := make([]rmlines.ItemID, 0, len(ids))
	for _, s := range ids {
		id, err := rmlines.ParseItemID(s)
		if err != nil {
			return nil, mapErr(err)
		}
		items = append(items, id)
	}
	out, err := rmlines.EraseItems(data, items)
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}

// WritePage implements port.PageWriter: a fresh page of typed text.
func (Pages) WritePage(d domain.PageDraft) ([]byte, error) {
	draft := &rmlines.Draft{
		Author: d.Author,
		Sheet: rmlines.Sheet{
			ScreenWidth:  uint32(d.Sheet.ScreenWidth),  //nolint:gosec // a sheet is a few thousand units wide
			ScreenHeight: uint32(d.Sheet.ScreenHeight), //nolint:gosec // see above
			CanvasWidth:  d.Sheet.CanvasWidth,
			CanvasHeight: d.Sheet.CanvasHeight,
		},
		Column:     rmlines.ColumnOf(column(d.Column)),
		Paragraphs: make([]rmlines.DraftParagraph, 0, len(d.Paragraphs)),
	}
	for _, p := range d.Paragraphs {
		para := rmlines.DraftParagraph{Text: p.Text}
		if style, ok := draftStyle(p.Style); ok {
			para.Style = &style
		}
		draft.Paragraphs = append(draft.Paragraphs, para)
	}
	out, err := rmlines.Write(draft)
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}

// column is the library's setting for a domain column.
func column(c domain.TextColumn) rmlines.TextColumn {
	switch c {
	case domain.ColumnWide:
		return rmlines.ColumnWide
	case domain.ColumnMedium:
		return rmlines.ColumnMedium
	default:
		return rmlines.ColumnNarrow
	}
}

// draftStyle is the style record of a paragraph the writer is given; ok
// false leaves the paragraph plain, which the writer writes by default.
func draftStyle(s domain.ParagraphStyle) (uint8, bool) {
	switch s {
	case domain.StyleHeading:
		return uint8(rmlines.StyleHeading), true
	case domain.StyleBold:
		return uint8(rmlines.StyleSubheading), true
	case domain.StyleBullet:
		return uint8(rmlines.StyleBullet), true
	case domain.StyleBulletNested:
		return uint8(rmlines.StyleBulletNested), true
	case domain.StyleCheckbox:
		return uint8(rmlines.StyleCheckbox), true
	case domain.StyleCheckboxDone:
		return uint8(rmlines.StyleCheckboxChecked), true
	case domain.StyleNumbered:
		return uint8(rmlines.StyleNumbered), true
	case domain.StyleNumberedNested:
		return uint8(rmlines.StyleNumberedNested), true
	default:
		return 0, false
	}
}

// mapErr names a page the format cannot read as a malformed file, keeping
// the library's error in the chain.
func mapErr(err error) error {
	if rmErr, ok := errors.AsType[*rmlines.Error](err); ok {
		return fmt.Errorf("%w: %w", domain.MalformedFile("page", rmErr.Error()), err)
	}
	return err
}
