// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "errors"

// ErrRenderUnsupported means this build draws no documents: the renderer is
// native, CoreGraphics on macOS and Cairo with PDFium on Linux and Windows,
// and this build has neither.
var ErrRenderUnsupported = errors.New("this build of rmctl renders no documents (rendering is in the macOS, Linux and Windows builds)")

// ErrRenderLite is ErrRenderUnsupported in rmctl-lite, the build made
// without the renderer on purpose (the norender build tag). errors.Is
// matches both, so whatever handles a build that draws nothing handles
// this one too, and a caller that tells them apart can name the full build.
var ErrRenderLite error = renderLiteError{}

// renderLiteError is ErrRenderLite.
type renderLiteError struct{}

func (renderLiteError) Error() string {
	return "rmctl-lite renders no documents (rendering is in rmctl, the full build)"
}

// Is makes ErrRenderLite an ErrRenderUnsupported.
func (renderLiteError) Is(target error) bool { return target == ErrRenderUnsupported }
