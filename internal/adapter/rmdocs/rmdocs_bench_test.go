//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

// notebook is the files of a 200-page notebook with 8 KiB pages.
func notebook(b *testing.B) []domain.ArchiveFile {
	b.Helper()
	page := make([]byte, 8<<10)
	files := []domain.ArchiveFile{
		{Name: "n1.metadata", Data: []byte(`{"visibleName":"Notes","type":"DocumentType"}`)},
		{Name: "n1.content", Data: []byte(`{"formatVersion":2}`)},
	}
	for i := range 200 {
		files = append(files, domain.ArchiveFile{Name: fmt.Sprintf("n1/p%03d.rm", i), Data: page})
	}
	return files
}

func BenchmarkWriteArchive(b *testing.B) {
	files := notebook(b)
	var size int64
	for _, f := range files {
		size += int64(len(f.Data))
	}
	src := sources(b, files)
	path := filepath.Join(b.TempDir(), "n.rmdoc")
	a := New()
	b.ReportAllocs()
	b.SetBytes(size)
	for b.Loop() {
		if err := a.WriteArchive(path, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead(b *testing.B) {
	a := New()
	path := filepath.Join(b.TempDir(), "n.rmdoc")
	if err := a.WriteArchive(path, sources(b, notebook(b))); err != nil {
		b.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(info.Size())
	for b.Loop() {
		arc, err := a.OpenArchive(path)
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		if err := arc.Read(nil, func(_ domain.ArchiveEntry, d []byte) error { n += len(d); return nil }); err != nil {
			b.Fatal(err)
		}
		_ = arc.Close()
	}
}
