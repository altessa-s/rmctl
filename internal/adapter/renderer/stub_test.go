//go:build !((darwin || linux || windows) && cgo)

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

func TestStubAnswersUnsupported(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.Available(); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
	if _, _, err := r.Render(t.Context(), nil, "", port.RenderOptions{}); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
}
