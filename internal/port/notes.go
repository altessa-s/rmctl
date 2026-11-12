// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"

	"github.com/altessa-s/rmctl/internal/domain"
)

// NoteOptions shape the markdown notes of documents' highlights.
type NoteOptions struct {
	// PDFLink is the pattern of the link from each highlight to its page
	// in a PDF kept beside the notes, as domain.ParsePageLink reads it; ""
	// links none.
	PDFLink string
}

// NotesTreeOptions shape the notes of every document below a folder.
type NotesTreeOptions struct {
	// All takes the documents in the trash too.
	All bool
	// AllDocs writes a note for every document; otherwise a document gets
	// one when it has highlights, or when the directory's record shows it
	// had one, so a note follows its document.
	AllDocs bool
	// Incremental skips a document whose note is there with the document's
	// modification time, and which the directory's record shows written
	// from the same document with the same options; every note written
	// takes that time. A document the record shows had nothing to write is
	// skipped unread while it stays as it was.
	Incremental bool
	// Prune moves the notes the record lists whose document is no longer
	// below the folder — deleted, moved, renamed or in the trash — into the
	// directory's domain.NotesRemovedDir, keeping their paths; without it
	// they stay where they are. Nothing is ever deleted.
	Prune bool
	// Jobs is how many documents are read at a time; 0 is the default.
	Jobs int
}

// NoteStatus is what became of one document of a folder's notes.
type NoteStatus string

// The statuses of a document of a folder's notes.
const (
	// NoteWritten means the note was written.
	NoteWritten NoteStatus = "written"
	// NoteUnchanged means an incremental run found the note there with the
	// document's modification time.
	NoteUnchanged NoteStatus = "unchanged"
	// NoteSkipped means the document has no highlights, so it gets no
	// note.
	NoteSkipped NoteStatus = "skipped"
	// NoteFailed means the document could not be read; nothing was written
	// for it.
	NoteFailed NoteStatus = "failed"
)

// NotedDocument is one document of a folder's notes.
type NotedDocument struct {
	Document domain.Item
	// LibraryPath is the document's path in the library, and Path its
	// note's, whatever the status.
	LibraryPath, Path string
	Status            NoteStatus
	// Highlights counts the highlights of a note written.
	Highlights int
	// Err is why a document failed.
	Err error
}

// NotesTreeReport is what writing a folder's notes did: the directory
// written into, its documents in library path order, and the notes of
// documents no longer below the folder: Moved into the removed directory
// by Prune, or Left where they are without it.
type NotesTreeReport struct {
	Dir         string
	Documents   []NotedDocument
	Moved, Left []string
}

// Count is how many documents have the status.
func (r NotesTreeReport) Count(status NoteStatus) int {
	n := 0
	for _, d := range r.Documents {
		if d.Status == status {
			n++
		}
	}
	return n
}

// Notes writes the highlights and the typed text of documents as markdown
// notes for a notes app.
type Notes interface {
	// Folder reports whether query names a folder ("" and "/" are the top
	// level), whose documents NotesTree writes, rather than a document.
	// Items in the trash count.
	Folder(ctx context.Context, query string) (bool, error)
	// NotesTree writes a markdown note of each document below the folder
	// query names under dir, keeping the folder tree, each file written
	// whole or not at all, with a record of what each note was written
	// from. An empty dir is a directory named after the folder in the
	// working directory. A document that fails is reported and the others
	// go on; the error then says how many failed.
	NotesTree(ctx context.Context, query, dir string, opts NoteOptions, tree NotesTreeOptions) (NotesTreeReport, error)
}
