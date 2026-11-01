// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxNotebookPages is the most pages a notebook made from text may have.
const MaxNotebookPages = 2000

// DefaultTemplate is the background of a page made from text.
const DefaultTemplate = "Blank"

// ErrTooManyPages means a text needs more pages than MaxNotebookPages.
var ErrTooManyPages = fmt.Errorf("the text needs more than %d pages", MaxNotebookPages)

// ParseDevice reads a device name; "" is the reMarkable 2.
func ParseDevice(name string) (Device, error) {
	switch d := Device(strings.ToLower(name)); d {
	case "", "remarkable-2", "remarkable2":
		return DeviceRM2, nil
	case DeviceRM2, DevicePure, DevicePro, DeviceMove:
		return d, nil
	case "paper-pure":
		return DevicePure, nil
	case "paper-pro":
		return DevicePro, nil
	case "paper-pro-move":
		return DeviceMove, nil
	default:
		return "", fmt.Errorf("%w: unknown device %q (rm2, pure, pro, move)", ErrUnsupportedFile, name)
	}
}

// SheetOf is the sheet a page laid out for d declares. The Move's numbers
// are the ones the device writes itself: its panel pixels would put the text
// beside the page.
func SheetOf(d Device) Sheet {
	switch d {
	case DevicePro:
		return Sheet{ScreenWidth: proWidth, ScreenHeight: proHeight, CanvasWidth: proWidth, CanvasHeight: proHeight}
	case DeviceMove:
		return Sheet{ScreenWidth: moveWidth, ScreenHeight: moveHeight, CanvasWidth: moveCanvasWidth, CanvasHeight: moveCanvasHeight}
	default:
		return Sheet{ScreenWidth: rm2Width, ScreenHeight: rm2Height, CanvasWidth: rm2Width, CanvasHeight: rm2Height}
	}
}

// The sheets the tablets declare, in the page's units.
const (
	rm2Width         = 1404
	rm2Height        = 1872
	proWidth         = 1620
	proHeight        = 2160
	moveWidth        = 820
	moveHeight       = 1458
	moveCanvasWidth  = 820.2954545454545
	moveCanvasHeight = 1458.3030303030303
)

// The widths of the three text column settings.
const (
	narrowWidth = 740
	mediumWidth = 936
	wideWidth   = 1152
)

// Width is the column's width in the page's units.
func (c TextColumn) Width() float64 {
	switch c {
	case ColumnWide:
		return wideWidth
	case ColumnMedium:
		return mediumWidth
	default:
		return narrowWidth
	}
}

// columnShare is how much of the sheet's width a column may take.
const columnShare = 0.85

// ColumnFor is the widest column that takes at most 85% of the sheet's
// width, or the narrow one when none does.
func ColumnFor(s Sheet) TextColumn {
	for _, c := range []TextColumn{ColumnWide, ColumnMedium} {
		if c.Width() <= columnShare*s.ScreenWidth {
			return c
		}
	}
	return ColumnNarrow
}

// The pagination estimate: lines a page holds and characters a line holds.
// They are not measured; they are the converter's own estimate.
const (
	pageMargin   = 220
	lineHeight   = 52
	minLines     = 8
	glyphWidth   = 20
	minLineChars = 20
)

// Paginate splits paragraphs into pages for a device. A page breaks before
// a paragraph that would overflow it; there is always at least one page.
func Paginate(paragraphs []Paragraph, d Device) ([][]Paragraph, error) {
	sheet := SheetOf(d)
	linesPerPage := max(minLines, int(sheet.CanvasHeight-pageMargin)/lineHeight)
	charsPerLine := max(minLineChars, int(ColumnFor(sheet).Width())/glyphWidth)
	var pages [][]Paragraph
	page := make([]Paragraph, 0, min(len(paragraphs), linesPerPage))
	used := 0
	for _, p := range paragraphs {
		lines := max(1, (utf8.RuneCountInString(p.Text)+charsPerLine-1)/charsPerLine)
		if used > 0 && used+lines > linesPerPage {
			pages = append(pages, page)
			page, used = make([]Paragraph, 0, linesPerPage), 0
		}
		page = append(page, p)
		used += lines
	}
	if len(page) > 0 || len(pages) == 0 {
		if len(page) == 0 {
			page = []Paragraph{{Style: StylePlain}}
		}
		pages = append(pages, page)
	}
	if len(pages) > MaxNotebookPages {
		return nil, ErrTooManyPages
	}
	return pages, nil
}

// idxLetters are the letters of a page's ordering key.
const idxLetters = "abcdefghijklmnopqrstuvwxyz"

// idxBase is how many keys one run of letters gives before a "z" is added.
const idxBase = 25

// IndexKey is the ordering key of the i-th page: bb, bc, …, bz, bzb, ….
func IndexKey(i int) string {
	return "b" + strings.Repeat("z", i/idxBase) + string(idxLetters[i%idxBase+1])
}

type stamped[T any] struct {
	Timestamp string `json:"timestamp"`
	Value     T      `json:"value"`
}

type notebookPage struct {
	ID       string          `json:"id"`
	Idx      stamped[string] `json:"idx"`
	Template stamped[string] `json:"template"`
}

type authorID struct {
	First  string `json:"first"`
	Second int    `json:"second"`
}

type cPages struct {
	LastOpened stamped[string] `json:"lastOpened"`
	Original   stamped[int]    `json:"original"`
	Pages      []notebookPage  `json:"pages"`
	UUIDs      []authorID      `json:"uuids"`
}

// notebookContent is the .content of a notebook of typed text, fields in
// sorted order: formatVersion 2 with cPages, and no margins, which would
// reflow the text.
type notebookContent struct {
	CPages           cPages         `json:"cPages"`
	CoverPageNumber  int            `json:"coverPageNumber"`
	DocumentMetadata map[string]any `json:"documentMetadata"`
	ExtraMetadata    map[string]any `json:"extraMetadata"`
	FileType         string         `json:"fileType"`
	FontName         string         `json:"fontName"`
	FormatVersion    int            `json:"formatVersion"`
	LineHeight       int            `json:"lineHeight"`
	Orientation      string         `json:"orientation"`
	PageCount        int            `json:"pageCount"`
	PageTags         []any          `json:"pageTags"`
	SizeInBytes      string         `json:"sizeInBytes"`
	Tags             []Tag          `json:"tags"`
	TextAlignment    string         `json:"textAlignment"`
	TextScale        int            `json:"textScale"`
	ZoomMode         string         `json:"zoomMode"`
}

// typedLineHeight is the line height of typed text, and notebookFormat the
// .content layout that keeps pages in cPages.
const (
	typedLineHeight = 100
	notebookFormat  = 2
)

// NewNotebookContent is the .content of a notebook whose pages, in order,
// are pageIDs, written by author (a lowercase UUID).
func NewNotebookContent(pageIDs []string, author string, tags []Tag) []byte {
	pages := make([]notebookPage, len(pageIDs))
	for i, id := range pageIDs {
		pages[i] = notebookPage{
			ID:       id,
			Idx:      stamped[string]{Timestamp: "1:1", Value: IndexKey(i)},
			Template: stamped[string]{Timestamp: "1:1", Value: DefaultTemplate},
		}
	}
	first := ""
	if len(pageIDs) > 0 {
		first = pageIDs[0]
	}
	if tags == nil {
		tags = []Tag{}
	}
	c := notebookContent{
		CPages: cPages{
			LastOpened: stamped[string]{Timestamp: "0:0", Value: first},
			Original:   stamped[int]{Timestamp: "0:0", Value: -1},
			Pages:      pages,
			UUIDs:      []authorID{{First: author, Second: 1}},
		},
		CoverPageNumber:  -1,
		DocumentMetadata: map[string]any{},
		ExtraMetadata:    map[string]any{},
		FileType:         "notebook",
		FormatVersion:    notebookFormat,
		LineHeight:       typedLineHeight,
		Orientation:      "portrait",
		PageCount:        len(pageIDs),
		PageTags:         []any{},
		SizeInBytes:      "0",
		Tags:             tags,
		TextAlignment:    "justify",
		TextScale:        1,
		ZoomMode:         "bestFit",
	}
	out, err := json.Marshal(c, json.Deterministic(true))
	if err != nil {
		panic("domain: notebook content does not encode: " + err.Error())
	}
	return out
}
