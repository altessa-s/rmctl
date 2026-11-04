// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"

	"github.com/altessa-s/rmctl/internal/domain"
)

// PDFMode is how a document becomes a PDF. The modes differ only for a
// document made from a PDF; a notebook comes out the same in each.
type PDFMode string

// The PDF modes.
const (
	// PDFFresh draws a new PDF: the imported pages as vectors, with their
	// links and outline, without forms, signatures or other annotations.
	PDFFresh PDFMode = "fresh"
	// PDFPreserving appends the marks to the imported PDF as an incremental
	// update, its own bytes untouched. A document whose pages were
	// reordered, added or hidden is refused.
	PDFPreserving PDFMode = "preserving"
	// PDFRecomposed writes a new PDF from the imported one's objects,
	// keeping forms and annotations; signatures stop verifying.
	PDFRecomposed PDFMode = "recomposed"
)

// Format is what a rendering is drawn as: the whole document as a PDF, or
// one page as an image.
type Format string

// The formats.
const (
	FormatPDF Format = "pdf"
	// FormatPNG and FormatJPEG are a page as pixels, on white.
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpg"
	// FormatSVG is a page as a standalone SVG document, without a
	// background.
	FormatSVG Format = "svg"
)

// Paper is the sheet a notebook page is laid out on; a page over an
// imported PDF keeps its PDF page's size.
type Paper string

// The papers.
const (
	PaperA4     Paper = "a4"
	PaperLetter Paper = "letter"
	// PaperTablet is the tablet's canvas at its own size.
	PaperTablet Paper = "tablet"
)

// RenderOptions shape a rendering.
type RenderOptions struct {
	// Page is the page drawn as an image, from 1; 0 draws the whole
	// document as a PDF.
	Page int
	// Format is what is drawn: FormatPDF for the whole document, an image
	// format for a page. Empty is FormatPDF without a page, FormatPNG with
	// one.
	Format Format
	// Mode is the PDF mode; empty is PDFFresh.
	Mode PDFMode
	// Paper is the sheet; empty is PaperA4.
	Paper Paper
	// Scale is a PNG's or a JPEG's pixels per .rm unit; 0 is the default,
	// at which a page is as wide as the tablet's screen.
	Scale float64
	// Quality is a JPEG's, from 1 to 100; 0 is the renderer's default.
	Quality int
	// NoTemplates draws the pages without their templates.
	NoTemplates bool
	// Document names the document of a local archive that holds several:
	// an identifier or a unique prefix of one.
	Document string
}

// RenderReport is what a rendering wrote, and what it could not keep.
type RenderReport struct {
	Path string
	// Pages is how many pages the file holds.
	Pages int
	// SkippedBlocks counts the blocks of pages that did not parse and were
	// left out; RefusedPages the pages naming a page the imported PDF does
	// not have; LinksDropped the links of an imported PDF a fresh PDF could
	// not carry over.
	SkippedBlocks, RefusedPages, LinksDropped int
}

// Renderer draws documents packed as .rmdoc archives. A build without the
// native renderer answers every call with domain.ErrRenderUnsupported.
type Renderer interface {
	// Available reports domain.ErrRenderUnsupported in a build that draws
	// nothing, so a caller fails before downloading anything.
	Available() error
	// Render draws the document id of the archive (the first one when id
	// is empty), which the caller has found in it: the page opts.Page as
	// the image opts.Format names, or the whole document as a PDF. Format,
	// Mode, Paper and Scale are set. A page beyond the document's is
	// domain.ErrNoSuchPage.
	Render(ctx context.Context, archive []byte, id domain.ItemID, opts RenderOptions) ([]byte, RenderReport, error)
}

// Rendering draws documents to local files.
type Rendering interface {
	// Render draws the document query names into dest, written whole or
	// not at all. An empty dest is a name made from the document's in the
	// working directory; a directory takes that name inside it.
	Render(ctx context.Context, query, dest string, opts RenderOptions) (RenderReport, error)
	// RenderFile draws a document of the local .rmdoc archive at path, as
	// Render does, without the cloud.
	RenderFile(ctx context.Context, path, dest string, opts RenderOptions) (RenderReport, error)
}
