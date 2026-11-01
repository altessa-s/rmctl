// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package rmpages reads, edits and writes .rm v6 pages through rmlines-go:
// it implements port.PageReader, port.PageEditor and port.PageWriter.
//
// rmlines-go compiles rmlines-lib, a C23 library, through cgo. A build
// without cgo gets the same type whose every call answers
// domain.ErrPagesUnsupported, so rmctl still builds where no C23 compiler
// is found, without its page commands.
package rmpages
