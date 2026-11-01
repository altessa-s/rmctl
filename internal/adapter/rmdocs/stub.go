//go:build !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdocs

import (
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// Archives answers every call with domain.ErrArchivesUnsupported: this
// build has no C compiler behind it.
type Archives struct{}

var (
	_ port.ArchiveReader = Archives{}
	_ port.ArchiveWriter = Archives{}
)

// New returns the stub.
func New() Archives { return Archives{} }

// CheckName implements port.ArchiveWriter.
func (Archives) CheckName(string) error { return domain.ErrArchivesUnsupported }

// WriteArchive implements port.ArchiveWriter.
func (Archives) WriteArchive(string, []port.ArchiveSource) error {
	return domain.ErrArchivesUnsupported
}

// OpenArchive implements port.ArchiveReader.
func (Archives) OpenArchive(string) (port.Archive, error) { return nil, domain.ErrArchivesUnsupported }
