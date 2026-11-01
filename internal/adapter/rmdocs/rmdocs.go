//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdocs

import (
	"fmt"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmdoc "github.com/altessa-s/rmdoc-go"
)

// The bounds of reading an archive. An entry is at most what an upload may
// be, with room to spare; the archive at most a few documents of that.
// rmdoc-go's own defaults bound the entry count (65,535), each name and the
// expansion ratio of a compressed entry.
const (
	maxEntrySize int64 = 1 << 30
	maxTotalSize int64 = 4 << 30
)

// Archives implements port.ArchiveReader and port.ArchiveWriter.
type Archives struct {
	limits rmdoc.Limits
}

var (
	_ port.ArchiveReader = Archives{}
	_ port.ArchiveWriter = Archives{}
)

// New returns the archive adapter with the reading bounds above.
func New() Archives {
	limits := rmdoc.DefaultLimits()
	limits.MaximumEntrySize = uint64(maxEntrySize)
	limits.MaximumTotalSize = uint64(maxTotalSize)
	return Archives{limits: limits}
}

// CheckName implements port.ArchiveWriter: the names a device-layout
// archive may hold.
func (Archives) CheckName(name string) error {
	return rmdoc.ValidateName(name, rmdoc.LayoutDevice)
}

// WriteArchive implements port.ArchiveWriter: a stored, device-layout
// archive written one entry at a time, each read from its file in bounded
// chunks.
func (Archives) WriteArchive(path string, files []port.ArchiveSource) error {
	w, err := rmdoc.CreateWriter(path, rmdoc.CompressionStore, rmdoc.LayoutDevice)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	for _, f := range files {
		if err := w.AddFile(f.Name, f.Path); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return w.Finish()
}

// OpenArchive implements port.ArchiveReader. The file is mapped, not read:
// it must stay unchanged until the archive is closed.
func (a Archives) OpenArchive(path string) (port.Archive, error) {
	limits := a.limits
	r, err := rmdoc.OpenReaderFile(path, &limits)
	if err != nil {
		return nil, err
	}
	return &archive{r: r}, nil
}

type archive struct {
	r *rmdoc.Reader
}

func entryOf(e rmdoc.Entry) domain.ArchiveEntry {
	return domain.ArchiveEntry{Name: e.Name, Size: int64(e.UncompressedSize), DocumentID: domain.ItemID(e.DocumentID())}
}

func (a *archive) Entries() []domain.ArchiveEntry {
	entries := a.r.Entries()
	out := make([]domain.ArchiveEntry, len(entries))
	for i, e := range entries {
		out[i] = entryOf(e)
	}
	return out
}

func (a *archive) Read(keep func(domain.ArchiveEntry) bool, body func(domain.ArchiveEntry, []byte) error) error {
	return a.r.ForEach(
		func(e rmdoc.Entry) bool { return keep == nil || keep(entryOf(e)) },
		func(e rmdoc.Entry, data []byte) error { return body(entryOf(e), data) },
	)
}

func (a *archive) Close() error { return a.r.Close() }
