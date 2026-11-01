// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pages_test

import (
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// BenchmarkPages reads a 50-page notebook: page order, fetch and parse.
func BenchmarkPages(b *testing.B) {
	bodies := make([][]byte, 50)
	for i := range bodies {
		paras := make([]domain.Paragraph, 20)
		for j := range paras {
			paras[j] = domain.Paragraph{Text: fmt.Sprintf("page %d line %d of typed text", i, j), Style: domain.StylePlain}
		}
		bodies[i] = porttest.PageBytes(paras, nil, 0)
	}
	f := newFixture(b, porttest.Doc{ID: "n1", Name: "Big", Pages: bodies})
	b.ReportAllocs()
	for b.Loop() {
		got, err := f.svc.Pages(b.Context(), "n1", nil)
		if err != nil || len(got.Pages) != 50 {
			b.Fatal(err)
		}
	}
}
