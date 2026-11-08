// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestDerivedFileName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"Paper.pdf": "Paper.md", "Book.EPUB": "Book.md", "Notes": "Notes.md", ".pdf": ".pdf.md", "a/b: c": "a-b- c.md", "..": "unnamed.md",
	} {
		if got := domain.DerivedFileName(name, "md"); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestPageLink(t *testing.T) {
	t.Parallel()
	l, err := domain.ParsePageLink("{root}../pdf/{path}.pdf#page={page}&id={id}&name={name}")
	if err != nil || l.IsZero() || l.String() != "{root}../pdf/{path}.pdf#page={page}&id={id}&name={name}" {
		t.Fatalf("%v %v", l, err)
	}
	for rel, want := range map[string]string{
		"Paper":           "../pdf/Paper.pdf#page=3&id=d1&name=Paper",
		"Sub/Deep/<a>":    "../../../pdf/Sub/Deep/%3Ca%3E.pdf#page=3&id=d1&name=%3Ca%3E",
		"Sub/Paper (d1x)": "../../pdf/Sub/Paper (d1x).pdf#page=3&id=d1&name=Paper (d1x)",
	} {
		if got := l.Target(rel, "d1", 3); got != want {
			t.Errorf("%q: %q, want %q", rel, got, want)
		}
	}
	for _, bad := range []string{"{file}.pdf", "{path}.pdf#page={page", "{{page}}x{"} {
		if _, err := domain.ParsePageLink(bad); !errors.Is(err, domain.ErrBadPageLink) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if zero, err := domain.ParsePageLink(""); err != nil || !zero.IsZero() {
		t.Fatal(zero, err)
	}
}

func TestNoteTagsAndType(t *testing.T) {
	t.Parallel()
	if got := domain.NoteTags([]string{"to read", " #focus ", "focus", "", "#", "a\tb"}); !slices.Equal(got, []string{"a-b", "focus", "to-read"}) {
		t.Fatalf("%q", got)
	}
	for k, want := range map[domain.Kind]string{
		domain.KindPDF: "pdf", domain.KindAnnotatedPDF: "pdf", domain.KindEPUB: "epub", domain.KindNotebook: "notebook",
	} {
		if got := domain.NoteType(k); got != want {
			t.Errorf("%s: %s", k, got)
		}
	}
}

func TestReadingOrder(t *testing.T) {
	t.Parallel()
	hs := []domain.Highlight{
		{ID: "none"},
		{ID: "low", Rects: []domain.Rect{{X: 1, Y: 50}}},
		{ID: "right", Rects: []domain.Rect{{X: 9, Y: 10}}},
		{ID: "left", Rects: []domain.Rect{{X: 5, Y: 30}, {X: 2, Y: 10}}},
	}
	got := domain.ReadingOrder(hs)
	ids := make([]string, len(got))
	for i, h := range got {
		ids[i] = h.ID
	}
	if !slices.Equal(ids, []string{"left", "right", "low", "none"}) || hs[0].ID != "none" {
		t.Fatalf("%q", ids)
	}
}

func TestNoteMarkdown(t *testing.T) {
	t.Parallel()
	n := domain.Note{
		Item: domain.Item{ID: "d1", Name: "Line\none \"quoted\"", Kind: domain.KindEPUB, Modified: time.Date(2026, 3, 4, 10, 20, 30, 0, time.FixedZone("x", 3600))},
		Pages: []domain.Page{{Number: 7, Highlights: []domain.Highlight{
			{Text: "first  \n\n  second\r\n", HasText: true, Color: "light blue"},
			{Text: "   ", HasText: true},
		}}},
	}
	want := `---
title: "Line\none \"quoted\""
remarkable-id: "d1"
type: epub
modified: 2026-03-04T09:20:30Z
pages: 1
highlights: 2
---

# Line one "quoted"

## Highlights

### Page 7

> first
>
>   second
>
> — p. 7 #highlight/light-blue

> (no text)
>
> — p. 7
`
	if got := n.Markdown(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// Nothing but the front matter and the title when there is nothing.
	empty := domain.Note{Item: domain.Item{ID: "d2", Name: "Empty.pdf", Kind: domain.KindPDF}}
	if got := empty.Markdown(); strings.Contains(got, "##") || strings.Contains(got, "modified") || !strings.HasSuffix(got, "---\n\n# Empty\n") {
		t.Fatalf("%q", got)
	}
}

func TestPageMarkdown(t *testing.T) {
	t.Parallel()
	p := domain.Page{Paragraphs: []domain.Paragraph{{Text: "Title", Style: domain.StyleHeading}, {Text: "body", Style: domain.StylePlain}}}
	if got := domain.PageMarkdown(p, "####"); got != "#### Title\n\nbody\n" {
		t.Fatalf("%q", got)
	}
	if got := domain.PageMarkdown(domain.Page{}, "#"); got != "" {
		t.Fatalf("%q", got)
	}
	if got := domain.HighlightText(domain.Highlight{Text: "x"}); got != domain.NoText {
		t.Fatal(got)
	}
}
