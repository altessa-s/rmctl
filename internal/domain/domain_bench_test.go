// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

var (
	sinkTree  *domain.Tree
	sinkItem  domain.Item
	sinkItems []domain.Item
	sinkBytes []byte
	sinkMeta  domain.Metadata
)

// bigLibrary is n items: a folder for every hundred documents.
func bigLibrary(b *testing.B, n int) []domain.Item {
	b.Helper()
	items := make([]domain.Item, 0, n)
	for i := range n {
		if i%100 == 0 {
			items = append(items, item(b, fmt.Sprintf("folder-%05d", i), fmt.Sprintf("Folder %d", i), true, ""))
			continue
		}
		parent := fmt.Sprintf("folder-%05d", i-i%100)
		items = append(items, item(b, fmt.Sprintf("doc-%05d", i), fmt.Sprintf("Doc %d", i), false, parent,
			domain.File{Name: "x.pdf", Size: 100}))
	}
	return items
}

func BenchmarkNewTree(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			items := bigLibrary(b, n)
			b.ReportAllocs()
			for b.Loop() {
				sinkTree = domain.NewTree(1, items)
			}
		})
	}
}

func BenchmarkResolve(b *testing.B) {
	tr := domain.NewTree(1, bigLibrary(b, 10000))
	for _, q := range []string{"/Folder 9900/Doc 9999", "doc-09999", "folder-099"} {
		b.Run(q, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				it, _, err := tr.Resolve(q, false)
				if err != nil {
					b.Fatal(err)
				}
				sinkItem = it
			}
		})
	}
}

func BenchmarkChildren(b *testing.B) {
	tr := domain.NewTree(1, bigLibrary(b, 10000))
	b.ReportAllocs()
	for b.Loop() {
		sinkItems = tr.Children("folder-05000", false)
	}
}

func BenchmarkMetadata(b *testing.B) {
	raw := domain.NewDocumentMetadata("A document with a name", "parent-id", at).Bytes()
	b.Run("parse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		for b.Loop() {
			m, err := domain.ParseMetadata(raw)
			if err != nil {
				b.Fatal(err)
			}
			sinkMeta = m
		}
	})
	m, err := domain.ParseMetadata(raw)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("rename-encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkBytes = m.Renamed("Another name", at).Bytes()
		}
	})
}
