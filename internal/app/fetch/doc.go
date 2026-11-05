// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fetch downloads from the library: a document's original, a
// document whole as an .rmdoc archive, a folder as a directory tree, and a
// backup: an archive per document with a manifest. It implements
// port.Fetcher.
//
// Every local name is derived from a visible name the cloud does not
// constrain, so each passes through [SafeName] before it touches a path.
package fetch
