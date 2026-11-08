// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"path"
	"strings"
	"time"
)

// ItemID is the identifier of a document or a folder: the documentID of its
// root index entry, a lowercase UUID for everything this tool creates.
type ItemID string

// RootID is the parent of a top-level item. TrashID is the parent the tablet
// writes for an item in the trash.
const (
	RootID  ItemID = ""
	TrashID ItemID = "trash"
)

// Kind is what an item holds.
type Kind string

// The kinds a library holds.
const (
	KindFolder       Kind = "folder"
	KindPDF          Kind = "pdf"
	KindAnnotatedPDF Kind = "annotated-pdf"
	KindEPUB         Kind = "epub"
	KindNotebook     Kind = "notebook"
)

// IsFolder reports whether the kind is a folder.
func (k Kind) IsFolder() bool { return k == KindFolder }

// Extension is the file extension of the kind's original, or "".
func (k Kind) Extension() string {
	switch k {
	case KindPDF, KindAnnotatedPDF:
		return "pdf"
	case KindEPUB:
		return "epub"
	default:
		return ""
	}
}

// File is one file of a document as its index lists it.
type File struct {
	// Name is the record name: <id>.metadata, <id>/<page>.rm and so on.
	Name string
	Hash string
	Size int64
}

// Extension is the part of the record name after its last dot, or "".
func (f File) Extension() string {
	ext := path.Ext(f.Name)
	return strings.TrimPrefix(ext, ".")
}

// Record is one entry of the root index: an item and the hash of its own
// index.
type Record struct {
	ID       ItemID
	Hash     string
	Subfiles int
	Size     int64
}

// Item is a document or a folder as the library shows it.
type Item struct {
	ID       ItemID
	Name     string
	Kind     Kind
	Parent   ItemID
	Pinned   bool
	Modified time.Time
	Created  time.Time
	Size     int64
	Tags     []string
	// Hash is the hash of the item's own index: it changes whenever any of
	// the item's files does.
	Hash string
	// Files is the item's index, in index order.
	Files []File
	// trashedHere is the item's own trash flag; Tree.IsTrashed also counts
	// a trashed ancestor.
	trashedHere bool
}

// TrashedHere reports whether the item itself is in the trash, not counting
// a trashed ancestor.
func (i Item) TrashedHere() bool { return i.trashedHere }

// File returns the first file whose extension is ext.
func (i Item) File(ext string) (File, bool) {
	for _, f := range i.Files {
		if f.Extension() == ext {
			return f, true
		}
	}
	return File{}, false
}

// Original is the file the document was made from: its PDF or EPUB.
func (i Item) Original() (File, bool) {
	if f, ok := i.File("pdf"); ok {
		return f, true
	}
	return i.File("epub")
}

// NewItem builds an item from its record, its index and its decoded
// metadata. tags are the tag names of its .content, if any.
func NewItem(rec Record, files []File, meta Metadata, tags []string) Item {
	var size int64
	hasRM := false
	for _, f := range files {
		size += f.Size
		if f.Extension() == "rm" {
			hasRM = true
		}
	}
	item := Item{
		ID:          rec.ID,
		Name:        meta.VisibleName(),
		Parent:      meta.ParentID(),
		Pinned:      meta.Pinned(),
		Modified:    meta.LastModified(),
		Created:     meta.Created(),
		Size:        max(size, 0),
		Tags:        tags,
		Hash:        rec.Hash,
		Files:       files,
		trashedHere: meta.Trashed(),
	}
	item.Kind = kindOf(meta, item, hasRM)
	return item
}

// NewListedItem is an item as a listing of the library keeps it, for
// shell completion: its identifier, name, kind, parent and whether it is in
// the trash itself; no files, times or tags.
func NewListedItem(id ItemID, name string, kind Kind, parent ItemID, trashed bool) Item {
	return Item{ID: id, Name: name, Kind: kind, Parent: parent, trashedHere: trashed}
}

func kindOf(meta Metadata, item Item, hasRM bool) Kind {
	if meta.Type() == TypeCollection {
		return KindFolder
	}
	if _, ok := item.File("pdf"); ok {
		if hasRM {
			return KindAnnotatedPDF
		}
		return KindPDF
	}
	if _, ok := item.File("epub"); ok {
		return KindEPUB
	}
	return KindNotebook
}
