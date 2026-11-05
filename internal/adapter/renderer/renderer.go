//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"context"
	"fmt"
	"image/color"
	"sync"

	"github.com/altessa-s/rmrender-go/fonts"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmrender "github.com/altessa-s/rmrender-go"
)

// Renderer implements port.Renderer on rmrender-go. The bundled fonts are
// loaded on the first drawing and kept for the life of the process.
type Renderer struct {
	fonts func() (*rmrender.Fonts, error)
}

var _ port.Renderer = (*Renderer)(nil)

// New returns the renderer.
func New() *Renderer {
	return &Renderer{fonts: sync.OnceValues(func() (*rmrender.Fonts, error) {
		return rmrender.NewFonts(fonts.FS)
	})}
}

// Available implements port.Renderer: this build draws.
func (*Renderer) Available() error { return nil }

// Render implements port.Renderer. A PNG or a JPEG is drawn on white, so a
// page reads the same in any viewer; an SVG has no background. The page
// templates are the bundled ones, or none with opts.NoTemplates.
func (r *Renderer) Render(ctx context.Context, archive []byte, id domain.ItemID, opts port.RenderOptions) ([]byte, port.RenderReport, error) {
	f, err := r.fonts()
	if err != nil {
		return nil, port.RenderReport{}, fmt.Errorf("load the fonts: %w", err)
	}
	doc, err := rmrender.OpenDocument(archive, &rmrender.DocumentOptions{ID: string(id)})
	if err != nil {
		return nil, port.RenderReport{}, fmt.Errorf("open the document: %w", err)
	}
	defer func() { _ = doc.Close() }()
	o := &rmrender.Options{Fonts: f, Paper: paper(opts.Paper)}
	if opts.NoTemplates {
		o.Templates = rmrender.NoTemplates()
	}
	if opts.Page > 0 {
		if opts.Page > doc.Len() {
			return nil, port.RenderReport{}, fmt.Errorf("page %d of %d: %w", opts.Page, doc.Len(), domain.ErrNoSuchPage)
		}
		out, perr := page(ctx, doc, opts, o)
		if perr != nil {
			return nil, port.RenderReport{}, fmt.Errorf("draw page %d: %w", opts.Page, perr)
		}
		return out, port.RenderReport{Pages: 1}, nil
	}
	pdf, report, err := doc.PDF(ctx, mode(opts.Mode), o)
	if err != nil {
		return nil, port.RenderReport{}, fmt.Errorf("draw the PDF: %w", err)
	}
	return pdf, port.RenderReport{
		Pages: report.RenderedPages, SkippedBlocks: report.SkippedBlocks,
		RefusedPages: report.RefusedSources, LinksDropped: report.LinksDropped,
	}, nil
}

// page draws the page opts.Page of doc as opts.Format names: an SVG, a
// JPEG on white or, for any other format, a PNG on white.
func page(ctx context.Context, doc *rmrender.Document, opts port.RenderOptions, o *rmrender.Options) ([]byte, error) {
	i := opts.Page - 1
	switch opts.Format {
	case port.FormatSVG:
		return doc.PageSVG(ctx, i, o)
	case port.FormatJPEG:
		o.Background = color.White
		return doc.PageJPEG(ctx, i, opts.Scale, opts.Quality, o)
	default:
		o.Background = color.White
		return doc.PagePNG(ctx, i, opts.Scale, o)
	}
}

// paper is rmrender's paper; anything unknown is A4.
func paper(p port.Paper) rmrender.Paper {
	switch p {
	case port.PaperLetter:
		return rmrender.PaperLetter
	case port.PaperTablet:
		return rmrender.PaperTablet
	default:
		return rmrender.PaperA4
	}
}

// mode is rmrender's PDF mode; anything unknown is a fresh PDF.
func mode(m port.PDFMode) rmrender.PDFMode {
	switch m {
	case port.PDFPreserving:
		return rmrender.PDFPreserving
	case port.PDFRecomposed:
		return rmrender.PDFRecomposed
	default:
		return rmrender.PDFFresh
	}
}
