// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import "errors"

// ErrArchivesUnsupported means this build reads and writes no .rmdoc
// archives: it was built without cgo, and the container library is C.
var ErrArchivesUnsupported = errors.New("this build of rmctl reads and writes no .rmdoc archives (built without cgo)")

// ArchiveFile is one file going into an .rmdoc archive: its record name and
// its bytes.
type ArchiveFile struct {
	Name string
	Data []byte
}

// ArchiveEntry is one entry of an .rmdoc archive, as its directory lists it.
type ArchiveEntry struct {
	// Name is the record name: <id>.metadata, <id>/<page>.rm and so on.
	Name string
	Size int64
	// DocumentID is the document the entry belongs to: the name up to its
	// first dot or slash.
	DocumentID ItemID
}

// ArchiveDocument is one document of an archive and its entries.
type ArchiveDocument struct {
	ID      ItemID
	Entries []ArchiveEntry
}

// GroupArchive groups entries by document, in the order each document first
// appears. An entry with no document identifier is its own group under "".
func GroupArchive(entries []ArchiveEntry) []ArchiveDocument {
	var docs []ArchiveDocument
	index := map[ItemID]int{}
	for _, e := range entries {
		i, ok := index[e.DocumentID]
		if !ok {
			i = len(docs)
			index[e.DocumentID] = i
			docs = append(docs, ArchiveDocument{ID: e.DocumentID})
		}
		docs[i].Entries = append(docs[i].Entries, e)
	}
	return docs
}

// RenameRecord is name filed under the document to instead of from: the
// identifier is the name's prefix, up to a dot or a slash. A name that is
// not one of from's is returned unchanged with false.
func RenameRecord(name string, from, to ItemID) (string, bool) {
	rest, ok := cutID(name, from)
	if !ok {
		return name, false
	}
	return string(to) + rest, true
}

// cutID is name without the document identifier id, when name is one of
// id's records: id, then a dot or a slash.
func cutID(name string, id ItemID) (string, bool) {
	if id == "" || len(name) <= len(id) || name[:len(id)] != string(id) {
		return "", false
	}
	rest := name[len(id):]
	if rest[0] != '.' && rest[0] != '/' {
		return "", false
	}
	return rest, true
}
