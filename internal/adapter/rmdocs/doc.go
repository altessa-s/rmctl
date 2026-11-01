// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package rmdocs is the .rmdoc container through rmdoc-go: it opens local
// archives for reading and packs files into the archive the tablet writes.
//
// rmdoc-go compiles a C23 library through cgo. Built without cgo, the
// package is a stub whose every call answers domain.ErrArchivesUnsupported,
// so the rest of rmctl still builds.
package rmdocs
