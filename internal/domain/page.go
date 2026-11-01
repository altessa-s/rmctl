// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "errors"

// ErrPagesUnsupported means this build reads no .rm pages: it was built
// without cgo, and the page format library is C.
var ErrPagesUnsupported = errors.New("this build of rmctl reads no .rm pages (built without cgo)")

// ParagraphStyle is the style of a typed paragraph, as the page format
// numbers it.
type ParagraphStyle int

// The paragraph styles of the page format.
const (
	StylePlain          ParagraphStyle = 1
	StyleHeading        ParagraphStyle = 2
	StyleBold           ParagraphStyle = 3
	StyleBullet         ParagraphStyle = 4
	StyleBulletNested   ParagraphStyle = 5
	StyleCheckbox       ParagraphStyle = 6
	StyleCheckboxDone   ParagraphStyle = 7
	StyleNumbered       ParagraphStyle = 10
	StyleNumberedNested ParagraphStyle = 11
)

// Paragraph is one paragraph of typed text.
type Paragraph struct {
	Text  string
	Style ParagraphStyle
}

// Rect is a rectangle in the page's own coordinates.
type Rect struct {
	X, Y, Width, Height float64
}

// Highlight is the marker over the text of an imported PDF or EPUB.
type Highlight struct {
	// ID is what the page calls the highlight: the handle an edit erases.
	ID string
	// Text is the words under it, as the tablet copied them, when it did.
	Text    string
	HasText bool
	// Color is the pen color's name.
	Color string
	Rects []Rect
}

// PageStats count what a page holds, for diagnostics.
type PageStats struct {
	Strokes         int
	StrokesByTool   map[string]int
	StrokesByColor  map[string]int
	AnchoredStrokes int
	Images          int
	SkippedBlocks   int
	// Marks counts what the highlighter left on the page: its highlights
	// over a text layer and its strokes. Unmark erases all of them.
	Marks int
	// PaperWidth and PaperHeight are the sheet the page declares, or zero.
	PaperWidth, PaperHeight float64
}

// Page is one parsed .rm page.
type Page struct {
	// ID is the page's identifier: the name of its .rm file without the
	// extension.
	ID string
	// Number counts the pages from 1 in the document's order.
	Number     int
	Paragraphs []Paragraph
	Highlights []Highlight
	Stats      PageStats
}
