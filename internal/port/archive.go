// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"

	"github.com/altessa-s/rmctl/internal/domain"
)

// The .rmdoc container as driven ports. A build without cgo answers every
// call with domain.ErrArchivesUnsupported.

// Archive is an open .rmdoc archive.
type Archive interface {
	// Entries is the archive's directory, in archive order.
	Entries() []domain.ArchiveEntry
	// Read hands each entry that keep selects to body, one at a time. The
	// bytes are valid only during the call. Limits are checked before the
	// first body call.
	Read(keep func(domain.ArchiveEntry) bool, body func(domain.ArchiveEntry, []byte) error) error
	Close() error
}

// ArchiveReader opens local .rmdoc archives.
type ArchiveReader interface {
	OpenArchive(path string) (Archive, error)
}

// ArchiveSource is a local file going into an archive under Name.
type ArchiveSource struct {
	Name string
	Path string
}

// ArchiveWriter writes .rmdoc archives the way the tablet writes one:
// stored, in the given order, deterministic.
type ArchiveWriter interface {
	// CheckName reports whether a record name may be an entry of an archive
	// the tablet reads, so a caller refuses it before fetching anything.
	CheckName(name string) error
	// WriteArchive writes an archive of files at path, streaming each from
	// its local file: no entry is held in memory whole. A failure may leave
	// a partial file at path, for the caller to discard.
	WriteArchive(path string, files []ArchiveSource) error
}

// ExportReport is what an export wrote.
type ExportReport struct {
	Path      string
	Documents int
	Files     int
}

// ImportedDocument is one document an import added.
type ImportedDocument struct {
	Item domain.Item
	// From is the identifier the document had in the archive; it differs
	// from Item.ID when that identifier was taken in the library.
	From domain.ItemID
}

// ArchiveReport describes a local archive without the cloud.
type ArchiveReport struct {
	Path      string
	Documents []ArchiveDocumentReport
}

// ArchiveDocumentReport is one document of a local archive.
type ArchiveDocumentReport struct {
	ID   domain.ItemID
	Name string
	Kind domain.Kind
	// Entries are the document's files with their sizes.
	Entries []domain.ArchiveEntry
}

// Archives answers questions about local .rmdoc files, offline.
type Archives interface {
	Inspect(ctx context.Context, path string) (ArchiveReport, error)
	// Pages parses the pages of each document of the archive, in each
	// document's order; pages restricts them as Pages.Pages does.
	Pages(ctx context.Context, path string, pages []string) ([]DocumentText, error)
}
