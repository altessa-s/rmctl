// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package renderer draws .rmdoc documents through rmrender-go: it
// implements port.Renderer, a whole document to a PDF or one page to a PNG,
// with the fonts and page templates rmrender-go bundles.
//
// rmrender-go draws natively, with CoreGraphics on macOS and with Cairo,
// Pango and PDFium on Linux, through cgo, and does not build anywhere else.
// Elsewhere, or without cgo, the package is a stub whose every call answers
// domain.ErrRenderUnsupported, so the rest of rmctl still builds.
package renderer
