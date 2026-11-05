//go:build norender || !((darwin || linux || windows) && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"context"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Renderer answers every call with errNoRender, a
// domain.ErrRenderUnsupported: rmrender-go draws on macOS, Linux and
// Windows, through cgo, and this build is none of them, has no cgo, or is
// rmctl-lite (the norender build tag), which leaves rmrender-go out on
// purpose.
type Renderer struct{}

var _ port.Renderer = (*Renderer)(nil)

// New returns the stub.
func New() *Renderer { return &Renderer{} }

// Available implements port.Renderer.
func (*Renderer) Available() error { return errNoRender }

// Render implements port.Renderer.
func (*Renderer) Render(context.Context, []byte, domain.ItemID, port.RenderOptions) ([]byte, port.RenderReport, error) {
	return nil, port.RenderReport{}, errNoRender
}
