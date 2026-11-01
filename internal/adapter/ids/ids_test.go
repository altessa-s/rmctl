// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package ids_test

import (
	"regexp"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/ids"
	"github.com/altessa-s/rmctl/internal/domain"
)

var v4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDIsLowercaseV4(t *testing.T) {
	t.Parallel()
	gen := ids.New()
	seen := map[domain.ItemID]bool{}
	for range 1000 {
		id := gen.NewID()
		if !v4.MatchString(string(id)) {
			t.Fatalf("%q is not a lowercase UUID v4", id)
		}
		if seen[id] {
			t.Fatalf("%q repeated", id)
		}
		seen[id] = true
	}
}

func BenchmarkNewID(b *testing.B) {
	gen := ids.New()
	b.ReportAllocs()
	for b.Loop() {
		_ = gen.NewID()
	}
}
