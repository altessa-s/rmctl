// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// BenchmarkFind searches a 10k-item library whose items are already loaded.
func BenchmarkFind(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	s := newBrowse(c)
	if _, err := s.Tree(b.Context()); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		q    port.FindQuery
	}{
		{"pattern", port.FindQuery{Pattern: `/Folder 4\d/Doc 7$`}},
		{"tag", port.FindQuery{Tags: []string{"t3"}}},
		{"under", port.FindQuery{Under: "/Folder 50"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := s.Find(b.Context(), tc.q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkList lists one folder of a loaded 10k-item library.
func BenchmarkList(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	s := newBrowse(c)
	if _, err := s.Tree(b.Context()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := s.List(b.Context(), "/Folder 7", false); err != nil {
			b.Fatal(err)
		}
	}
}
