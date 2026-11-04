// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Renderer is the renderer double. A PDF is "PDF <id> <mode> <paper>" and a
// PNG "PNG <id> p<page> x<scale> <paper>", each followed by a newline and
// the archive it was drawn from, so a test sees both what was asked and
// what was drawn. Pages is how many pages every document has (0 is 3);
// Report is what a PDF reports beyond its pages. Unavailable makes it a
// build without the renderer; Fail fails Render.
type Renderer struct {
	Pages       int
	Report      port.RenderReport
	Unavailable bool
	Fail        error

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
func (r *Renderer) Render(ctx context.Context, archive []byte, id domain.ItemID, opts port.RenderOptions) ([]byte, port.RenderReport, error) {
	if err := r.Available(); err != nil {
		return nil, port.RenderReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, port.RenderReport{}, context.Cause(ctx)
	}
	if r.Fail != nil {
		return nil, port.RenderReport{}, r.Fail
	}
	pages := r.Pages
	if pages == 0 {
		pages = 3
	}
	report := r.Report
	head := fmt.Sprintf("PDF %s %s %s", id, opts.Mode, opts.Paper)
	if opts.Page > 0 {
		if opts.Page > pages {
			return nil, port.RenderReport{}, fmt.Errorf("page %d of %d: %w", opts.Page, pages, domain.ErrNoSuchPage)
		}
		head = fmt.Sprintf("PNG %s p%d x%g %s", id, opts.Page, opts.Scale, opts.Paper)
		report = port.RenderReport{Pages: 1}
	} else {
		report.Pages = pages
	}
	r.mu.Lock()
	r.calls = append(r.calls, head)
	r.mu.Unlock()
	return append([]byte(head+"\n"), archive...), report, nil
}
