// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !cgo

package rmpages_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/rmpages"
	"github.com/altessa-s/rmctl/internal/domain"
)

func TestStubRefusesEverything(t *testing.T) {
	t.Parallel()
	p := rmpages.New(nil)
	_, e1 := p.ReadPage(nil)
	_, e2 := p.Highlights(nil)
	_, e3 := p.Erase(nil, nil)
	_, e4 := p.WritePage(domain.PageDraft{})
	for _, err := range []error{e1, e2, e3, e4} {
		if !errors.Is(err, domain.ErrPagesUnsupported) {
			t.Fatal(err)
		}
	}
}
