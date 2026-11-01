// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"

	"github.com/altessa-s/rmctl/internal/domain"
)

// The .rm page format, as three narrow driven ports: a use case that only
// reads asks for the reader alone. A build without cgo answers every call
// with domain.ErrPagesUnsupported.

// PageReader parses .rm pages.
type PageReader interface {
	ReadPage(data []byte) (domain.Page, error)
}

// PageEditor changes an existing .rm page and verifies the result.
type PageEditor interface {
	// Highlights are the identifiers of the page's highlights.
	Highlights(data []byte) ([]string, error)
	// Erase marks items erased the way the tablet's eraser does; every other
	// byte stays as it was.
	Erase(data []byte, ids []string) ([]byte, error)
}

// PageWriter writes fresh .rm pages.
type PageWriter interface {
	WritePage(d domain.PageDraft) ([]byte, error)
}

// DocumentText is the typed text of a document, page by page.
type DocumentText struct {
	Item  domain.Item
	Pages []domain.Page
}

// UnmarkReport says what unmark erased: every mark of the highlighter,
// Highlights over a text layer and Strokes drawn with it.
type UnmarkReport struct {
	Item       domain.Item
	Erased     int
	Highlights int
	Strokes    int
	Pages      int
}

// NotebookOptions shape a notebook made from text.
type NotebookOptions struct {
	Name   string
	Device domain.Device
	Tags   []string
}

// Pages answers questions about the pages of a document, and changes them.
type Pages interface {
	// Pages parses every page of a notebook or annotated document, in the
	// document's order. pages restricts them to these numbers (from 1) or
	// identifiers; empty is every page.
	Pages(ctx context.Context, query string, pages []string) (DocumentText, error)
	// Unmark erases the highlights of a document's pages and commits them.
	Unmark(ctx context.Context, query string, pages []string) (UnmarkReport, error)
	// Notebook makes a notebook of typed text from a text or markdown file
	// and uploads it into the folder dest.
	Notebook(ctx context.Context, local, dest string, opts NotebookOptions) (domain.Item, error)
	// PlanNotebook checks what Notebook would make — the file read and
	// parsed, its pages laid out and written in memory, the folder
	// resolved — and uploads and commits nothing. The item has its name,
	// parent and kind; it has no identifier yet.
	PlanNotebook(ctx context.Context, local, dest string, opts NotebookOptions) (domain.Item, error)
}
