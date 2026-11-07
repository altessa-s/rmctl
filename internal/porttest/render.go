// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Renderer is the renderer double. A PDF is "PDF <id> <mode> <paper>", a
// PNG "PNG <id> p<page> x<scale> <paper>", a JPEG "JPG <id> p<page>
// x<scale> q<quality> <paper>" and an SVG "SVG <id> p<page> <paper>", with
// " grain-<grain>" after for a grain other than the default and
// " no-template" when the templates are left out, each followed by a
// newline and the archive it was drawn from, so a test sees both what was
// asked and what was drawn. Pages is how many pages every document has (0
// is 3); Report is what a PDF reports beyond its pages. Unavailable makes
// it a build without the renderer; Fail fails Render before it writes, and
// FailIDs fails it for the documents it names alone; FailWritten fails it
// once it has written the first line, as a drawing that stops partway.
type Renderer struct {
	Pages       int
	Report      port.RenderReport
	Unavailable bool
	Fail        error
	FailIDs     map[domain.ItemID]error
	FailWritten error

	mu    sync.Mutex
	calls []string
}

var _ port.Renderer = (*Renderer)(nil)

// Calls are the documents drawn, as their first lines.
func (r *Renderer) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// Available implements port.Renderer.
func (r *Renderer) Available() error {
	if r.Unavailable {
		return domain.ErrRenderUnsupported
	}
	return nil
}

// Render implements port.Renderer.
func (r *Renderer) Render(ctx context.Context, archive []byte, id domain.ItemID, opts port.RenderOptions, w io.Writer) (port.RenderReport, error) {
	if err := r.Available(); err != nil {
		return port.RenderReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return port.RenderReport{}, context.Cause(ctx)
	}
	if r.Fail != nil {
		return port.RenderReport{}, r.Fail
	}
	if err := r.FailIDs[id]; err != nil {
		return port.RenderReport{}, err
	}
	pages := r.Pages
	if pages == 0 {
		pages = 3
	}
	report := r.Report
	head := fmt.Sprintf("PDF %s %s %s", id, opts.Mode, opts.Paper)
	if opts.Page > 0 {
		if opts.Page > pages {
			return port.RenderReport{}, fmt.Errorf("page %d of %d: %w", opts.Page, pages, domain.ErrNoSuchPage)
		}
		switch opts.Format {
		case port.FormatJPEG:
			head = fmt.Sprintf("JPG %s p%d x%g q%d %s", id, opts.Page, opts.Scale, opts.Quality, opts.Paper)
		case port.FormatSVG:
			head = fmt.Sprintf("SVG %s p%d %s", id, opts.Page, opts.Paper)
		default:
			head = fmt.Sprintf("PNG %s p%d x%g %s", id, opts.Page, opts.Scale, opts.Paper)
		}
		report = port.RenderReport{Pages: 1}
	} else {
		report.Pages = pages
	}
	if opts.Grain != "" && opts.Grain != port.PDFGrainMask {
		head += " grain-" + string(opts.Grain)
	}
	if opts.NoTemplates {
		head += " no-template"
	}
	r.mu.Lock()
	r.calls = append(r.calls, head)
	r.mu.Unlock()
	if _, err := io.WriteString(w, head+"\n"); err != nil {
		return port.RenderReport{}, err
	}
	if r.FailWritten != nil {
		return port.RenderReport{}, r.FailWritten
	}
	if _, err := w.Write(archive); err != nil {
		return port.RenderReport{}, err
	}
	return report, nil
}
