//go:build norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import "github.com/altessa-s/rmctl/internal/domain"

// errNoRender is what rmctl-lite answers: rendering is in the full build.
var errNoRender = domain.ErrRenderLite
