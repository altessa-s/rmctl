// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package library_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// BenchmarkLoad builds the tree of a 10k-item library: cold reads every
// index and metadata, warm reuses every item by its unchanged hash.
func BenchmarkLoad(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := library.New(c, c, c).Load(b.Context()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("warm", func(b *testing.B) {
		lib := library.New(c, c, c)
		if _, _, err := lib.Load(b.Context()); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := lib.Load(b.Context()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
