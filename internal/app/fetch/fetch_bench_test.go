// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch_test

import (
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// BenchmarkPlan maps a loaded 10k-item library onto local paths.
func BenchmarkPlan(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	tree, _, err := library.New(c, c, c).Load(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if p := fetch.Plan(tree, domain.RootID, "out"); len(p.Files) != 10000 {
			b.Fatalf("%d files", len(p.Files))
		}
	}
}

// BenchmarkMirrorIncremental mirrors a 1k-document folder tree that is
// already on disk: every file is a size check and a skip.
func BenchmarkMirrorIncremental(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(10, 100)
	files := porttest.NewMemFiles()
	s := fetch.New(library.New(c, c, c), c, files, porttest.NewArchiver(files))
	if _, err := s.Mirror(b.Context(), "", "out", port.MirrorOptions{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Mirror(b.Context(), "", "out", port.MirrorOptions{Incremental: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSafeName(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = fetch.SafeName("Quarterly report: Q3/2026 \x01 final")
	}
}

// BenchmarkExportPlan plans the export of a folder of 1000 documents in ten
// subfolders, one of them trashed.
func BenchmarkExportPlan(b *testing.B) {
	items := []domain.Item{}
	parse := func(js string) domain.Metadata {
		m, err := domain.ParseMetadata([]byte(js))
		if err != nil {
			b.Fatal(err)
		}
		return m
	}
	items = append(items, domain.NewItem(domain.Record{ID: "top"}, nil, parse(`{"visibleName":"Top","type":"CollectionType"}`), nil))
	for f := range 10 {
		parent := "trash"
		if f > 0 {
			parent = "top"
		}
		id := domain.ItemID(fmt.Sprintf("folder-%d", f))
		items = append(items, domain.NewItem(domain.Record{ID: id}, nil,
			parse(fmt.Sprintf(`{"visibleName":"F%d","type":"CollectionType","parent":%q}`, f, parent)), nil))
		for d := range 100 {
			items = append(items, domain.NewItem(domain.Record{ID: domain.ItemID(fmt.Sprintf("doc-%d-%d", f, d))},
				[]domain.File{{Name: "x.pdf", Size: 1}},
				parse(fmt.Sprintf(`{"visibleName":"D%d","type":"DocumentType","parent":%q}`, d, id)), nil))
		}
	}
	tree := domain.NewTree(1, items)
	b.ReportAllocs()
	for b.Loop() {
		docs, _, err := fetch.ExportPlan(tree, []string{"top"})
		if err != nil || len(docs) != 900 {
			b.Fatal(len(docs), err)
		}
	}
}
