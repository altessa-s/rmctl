//go:build norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// rmctl-lite names itself, so the command line can point at the full build.
func TestLiteAnswersLite(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.Available(); !errors.Is(err, domain.ErrRenderLite) {
		t.Fatal(err)
	}
	if _, _, err := r.Render(t.Context(), nil, "", port.RenderOptions{}); !errors.Is(err, domain.ErrRenderLite) {
		t.Fatal(err)
	}
}
