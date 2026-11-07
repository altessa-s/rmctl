// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"errors"
	"fmt"
	"strings"
)

// The failures the library model names. Each is matched with errors.Is or
// recovered with errors.As.
var (
	// ErrNotFound means no item answers to the path or identifier.
	ErrNotFound = errors.New("not found")
	// ErrNotAFolder means a path goes through, or a destination is, a document.
	ErrNotAFolder = errors.New("not a folder")
	// ErrIntoOwnSubtree means a folder would move into itself or below itself.
	ErrIntoOwnSubtree = errors.New("a folder cannot move into itself")
	// ErrDestinationGone means the destination folder is missing or in the trash.
	ErrDestinationGone = errors.New("the destination folder is gone or in the trash")
	// ErrVanished means the item left the library while the change was made.
	ErrVanished = errors.New("the item is no longer in the library")
	// ErrReplaceTargetChanged means the document a replacement chose was
	// moved or renamed before the change could be committed.
	ErrReplaceTargetChanged = errors.New("the document to replace was moved or renamed meanwhile; run the command again")
	// ErrInTrash means a trashed item was asked to be renamed or moved.
	ErrInTrash = errors.New("the item is in the trash; restore it first")
	// ErrNotInTrash means restore was asked of an item outside the trash.
	ErrNotInTrash = errors.New("the item is not in the trash")
	// ErrNoOriginal means a document has no PDF or EPUB to download.
	ErrNoOriginal = errors.New("the document has no PDF or EPUB")
	// ErrUnsupportedFile means a file cannot be uploaded.
	ErrUnsupportedFile = errors.New("unsupported file")
	// ErrEmptyName means a name or path is empty.
	ErrEmptyName = errors.New("the name is empty")
	// ErrNoContentTarget means put --content-only found no document of the
	// name in the folder: there is nothing whose annotations to keep.
	ErrNoContentTarget = errors.New("no document of this name in the folder to replace the PDF of; upload it without --content-only")
	// ErrNotPDFDocument means put --content-only was pointed at a document
	// that is not a PDF.
	ErrNotPDFDocument = errors.New("the document is not a PDF; only a PDF's content can be replaced")
	// ErrAnnotationsDropped means the new PDF has fewer pages and some of the
	// pages it no longer has carry annotations.
	ErrAnnotationsDropped = errors.New("the new PDF has fewer pages, and pages it no longer has carry annotations; pass --drop-annotations to drop them")
	// ErrNoSuchPage means a page number is beyond a document's pages.
	ErrNoSuchPage = errors.New("no such page")
	// ErrUnreadablePage means a page of a document drawn does not parse,
	// and the drawing was asked to fail on one; its words say how to draw
	// the document anyway, after the page and why it cannot be read.
	ErrUnreadablePage = errors.New("--unreadable-pages placeholder or blank draws the rest of the document")
	// ErrFolderTags means tags were asked of a folder: the tablet tags
	// documents and their pages, and a folder's .content holds no tags.
	ErrFolderTags = errors.New("a folder takes no tags; tag the documents in it")
	// ErrSlashInName means a name holds a slash, which no path could reach.
	ErrSlashInName = errors.New("a name cannot hold a slash: no path would reach it")
)

// AmbiguousError means a path or an identifier prefix names several items.
type AmbiguousError struct {
	Query      string
	Candidates []Item
}

func (e *AmbiguousError) Error() string {
	ids := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		ids = append(ids, string(c.ID))
	}
	return fmt.Sprintf("%q is ambiguous: %s", e.Query, strings.Join(ids, ", "))
}

// MalformedFileError means a file of the library does not decode.
type MalformedFileError struct {
	File   string
	Reason string
}

func (e *MalformedFileError) Error() string {
	return fmt.Sprintf("malformed %s: %s", e.File, e.Reason)
}

// MalformedFile builds a [MalformedFileError].
func MalformedFile(file, reason string) *MalformedFileError {
	return &MalformedFileError{File: file, Reason: reason}
}
