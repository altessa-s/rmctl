// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package notes writes the highlights and the typed text of the documents
// below a folder as markdown notes for a notes app, one per document,
// keeping the folder tree, with a record of what each note was written
// from so that a later run writes only what changed. It implements
// port.Notes; the note itself is [domain.Note].
package notes
