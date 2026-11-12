//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"sync"

	"github.com/altessa-s/rmrender-go/fonts"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmrender "github.com/altessa-s/rmrender-go"
)

// MaxPDFBytes bounds a PDF, which is written into its file as it is drawn
// and never held whole: 8 GiB, past the 256 MiB rmrender-go bounds a PDF it
// answers in memory to, and short of the 10 GB a PDF's classic
// cross-reference table can address. A preserving or recomposed drawing
// still reads the imported PDF and holds its overlays in memory first.
const MaxPDFBytes = min(8<<30, math.MaxInt)

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
// templates are the bundled ones, or none with opts.NoTemplates. A PDF goes
// to w as it is drawn, up to MaxPDFBytes; a page is drawn whole, then
// written.
func (r *Renderer) Render(ctx context.Context, archive []byte, id domain.ItemID, opts port.RenderOptions, w io.Writer) (port.RenderReport, error) {
	f, err := r.fonts()
	if err != nil {
		return port.RenderReport{}, fmt.Errorf("load the fonts: %w", err)
	}
	doc, err := rmrender.OpenDocument(archive, &rmrender.DocumentOptions{ID: string(id)})
	if err != nil {
		return port.RenderReport{}, fmt.Errorf("open the document: %w", err)
	}
	defer func() { _ = doc.Close() }()
	o := &rmrender.Options{Fonts: f, Paper: paper(opts.Paper), PDFGrain: grain(opts.Grain), Unreadable: unreadable(opts.Unreadable)}
	if opts.NoTemplates {
		o.Templates = rmrender.NoTemplates()
	}
	if opts.Page > 0 {
		if opts.Page > doc.Len() {
			return port.RenderReport{}, fmt.Errorf("page %d of %d: %w", opts.Page, doc.Len(), domain.ErrNoSuchPage)
		}
		// The page drawn alone has no report: CheckPage says whether it is
		// one the option drew in its own way.
		stand := 0
		if cerr := doc.CheckPage(opts.Page - 1); cerr != nil {
			if o.Unreadable == rmrender.UnreadableStrict || !unreadablePage(cerr) {
				return port.RenderReport{}, fmt.Errorf("draw page %d: %w", opts.Page, explained(cerr))
			}
			stand = 1
		}
		out, perr := page(ctx, doc, opts, o)
		if perr != nil {
			return port.RenderReport{}, fmt.Errorf("draw page %d: %w", opts.Page, explained(perr))
		}
		if _, werr := w.Write(out); werr != nil {
			return port.RenderReport{}, werr
		}
		return port.RenderReport{Pages: 1, UnreadablePages: stand}, nil
	}
	limits := rmrender.DefaultLimits()
	limits.MaximumPDFBytes = MaxPDFBytes
	o.Limits = &limits
	report, err := doc.WritePDF(ctx, w, mode(opts.Mode), o)
	if err != nil {
		return port.RenderReport{}, fmt.Errorf("draw the PDF: %w", explained(err))
	}
	return port.RenderReport{
		Pages: report.RenderedPages, SkippedBlocks: report.SkippedBlocks,
		RefusedPages: report.RefusedSources, LinksDropped: report.LinksDropped,
		UnreadablePages: report.UnreadablePages,
	}, nil
}

// unreadablePage reports whether err is that of a page that does not parse.
func unreadablePage(err error) bool {
	var p *rmrender.UnreadablePageError
	return errors.As(err, &p)
}

// explained is err, or, for a page that does not parse, which page it is,
// counted from 1, and why, as domain.ErrUnreadablePage.
func explained(err error) error {
	var p *rmrender.UnreadablePageError
	if !errors.As(err, &p) {
		return err
	}
	why := "does not parse"
	if errors.Is(err, rmrender.ErrUnsupported) {
		why = "is of a format this rmctl does not read"
	}
	return fmt.Errorf("page %d (%s) %s; %w", p.Page+1, p.ID, why, domain.ErrUnreadablePage)
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

// grain is rmrender's PDF grain; anything unknown is the mask.
func grain(g port.PDFGrain) rmrender.PDFGrain {
	if g == port.PDFGrainGroup {
		return rmrender.PDFGrainGroup
	}
	return rmrender.PDFGrainMask
}

// unreadable is rmrender's unreadable page policy; anything unknown is the
// placeholder.
func unreadable(u port.Unreadable) rmrender.Unreadable {
	switch u {
	case port.UnreadableBlank:
		return rmrender.UnreadableBlank
	case port.UnreadableStrict:
		return rmrender.UnreadableStrict
	default:
		return rmrender.UnreadablePlaceholder
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
