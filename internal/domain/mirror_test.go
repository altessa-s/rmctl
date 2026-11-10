// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestMirrorIgnored(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{
		"Paper.pdf":                  false,
		"Книги/Книга.epub":           false,
		"Books/_annotated/Novel.pdf": false,
		"_annotated/Novel.pdf":       true,
		".rmctl-mirror.json":         true,
		".rmctl-removed/Old.pdf":     true,
		".git/HEAD":                  true,
		"Sub/.hidden/Paper.pdf":      true,
		".DS_Store":                  true,
		"Sub/Thumbs.db":              true,
		"desktop.ini":                true,
		"~$Report.pdf":               true,
		"Download.pdf.part":          true,
		"Download.pdf.crdownload":    true,
		"Draft.pdf.tmp":              true,
		"Draft.pdf~":                 true,
		"../outside.pdf":             true,
		"":                           true,
	} {
		if got := domain.MirrorIgnored(rel); got != want {
			t.Errorf("%q: %v, want %v", rel, got, want)
		}
	}
}

func TestMirrorUploadable(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{"a.pdf": true, "B.PDF": true, "c.epub": true, "d.md": false, "e": false} {
		if got := domain.MirrorUploadable(rel); got != want {
			t.Errorf("%q: %v", rel, got)
		}
	}
}

func TestMirrorNames(t *testing.T) {
	t.Parallel()
	if got := domain.MirrorStem("Books/Novel.v2.epub"); got != "Novel.v2" {
		t.Error(got)
	}
	if got := domain.MirrorAnnotatedPath("Книги/Книга.epub", "pdf"); got != "_annotated/Книги/Книга.pdf" {
		t.Error(got)
	}
	if got := domain.MirrorAnnotatedPath("Paper.pdf", "rmdoc"); got != "_annotated/Paper.rmdoc" {
		t.Error(got)
	}
	for name, want := range map[string]bool{"papers": true, "a-b_1": true, "Papers": false, "-x": false, "": false} {
		if got := domain.ValidMirrorName(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}

func TestAnnotatedAndFingerprint(t *testing.T) {
	t.Parallel()
	plain := domain.Item{ID: "d1", Files: []domain.File{
		{Name: "d1.metadata", Hash: "m1"}, {Name: "d1.content", Hash: "c1"}, {Name: "d1.pdf", Hash: "p1"},
	}}
	if domain.Annotated(plain) {
		t.Fatal("a plain PDF is annotated")
	}
	inked := plain
	inked.Files = append(append([]domain.File{}, plain.Files...), domain.File{Name: "d1/page.rm", Hash: "r1"})
	highlighted := plain
	highlighted.Files = append(append([]domain.File{}, plain.Files...), domain.File{Name: "d1.highlights/page.json", Hash: "h1"})
	if !domain.Annotated(inked) || !domain.Annotated(highlighted) {
		t.Fatal("marks were not seen")
	}
	opened := plain
	opened.Files = []domain.File{
		{Name: "d1.pdf", Hash: "p1"}, {Name: "d1.content", Hash: "c1"}, {Name: "d1.metadata", Hash: "m2"},
		{Name: "d1.thumbnails/page.png", Hash: "t1"},
	}
	if domain.DrawnFingerprint(plain) != domain.DrawnFingerprint(opened) {
		t.Fatal("the metadata, the order or the thumbnails changed the fingerprint")
	}
	if domain.DrawnFingerprint(plain) == domain.DrawnFingerprint(inked) {
		t.Fatal("a page of ink did not change the fingerprint")
	}
}
