// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "errors"

// ErrRenderUnsupported means this build draws no documents: the renderer is
// native, CoreGraphics on macOS and Cairo with PDFium on Linux and Windows,
// and this build has neither.
var ErrRenderUnsupported = errors.New("this build of rmctl renders no documents (rendering is in the macOS, Linux and Windows builds)")
