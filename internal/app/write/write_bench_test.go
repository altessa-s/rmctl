// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// BenchmarkRename commits one rename on a 10k-item root: load (warm), edit
// one metadata, rebuild one index, write the root.
func BenchmarkRename(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	s := write.New(libraryOf(c), c, c, porttest.NewMemFiles(), porttest.PDF{}, porttest.NewArchiver(porttest.NewMemFiles()), porttest.NewIDs("b"), porttest.NewClock(porttest.Epoch))
	names := [2]string{"/Folder 3/Renamed", "/Folder 3/Doc 5"}
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Move(b.Context(), "d0030005", names[i%2]); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkMkdir commits one new folder on a 10k-item root.
func BenchmarkMkdir(b *testing.B) {
	c := porttest.NewCloud()
	c.AddLarge(100, 100)
	s := write.New(libraryOf(c), c, c, porttest.NewMemFiles(), porttest.PDF{}, porttest.NewArchiver(porttest.NewMemFiles()), porttest.NewIDs("b"), porttest.NewClock(porttest.Epoch))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Mkdir(b.Context(), "/Folder 1/Sub", true); err != nil {
			b.Fatal(err)
		}
	}
}
