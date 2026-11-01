//go:build !cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdocs

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestStubAnswersUnsupported(t *testing.T) {
	t.Parallel()
	a := New()
	if err := a.CheckName("x.metadata"); !errors.Is(err, domain.ErrArchivesUnsupported) {
		t.Fatal(err)
	}
	if err := a.WriteArchive("x", nil); !errors.Is(err, domain.ErrArchivesUnsupported) {
		t.Fatal(err)
	}
	if _, err := a.OpenArchive("x"); !errors.Is(err, domain.ErrArchivesUnsupported) {
		t.Fatal(err)
	}
}
