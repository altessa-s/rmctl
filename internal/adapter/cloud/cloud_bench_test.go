// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// BenchmarkRoot reads and maps the root of a large library over loopback.
func BenchmarkRoot(b *testing.B) {
	for _, n := range []int{100, 10000} {
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			double, c := registered(b)
			items := make([]clouddouble.Item, 0, n)
			for i := range n {
				items = append(items, clouddouble.Item{ID: fmt.Sprintf("doc-%05d", i), Name: "D", Kind: clouddouble.Notebook, Minimal: true})
			}
			double.Seed(b, items...)
			b.ReportAllocs()
			for b.Loop() {
				state, err := c.Root(b.Context())
				if err != nil || len(state.Records) != n {
					b.Fatal(len(state.Records), err)
				}
			}
		})
	}
}
