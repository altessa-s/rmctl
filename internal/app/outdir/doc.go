// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package outdir lays a library folder out as a directory tree, one file
// per document, and keeps the record of what each file was written from:
// the ground the use cases that write a folder's documents into a
// directory share — render's PDFs and the highlight notes. It is not an
// input port itself.
//
// Every local name is derived from a visible name the cloud does not
// constrain, so each passes through [domain.SafeName] before it touches a
// path.
package outdir
