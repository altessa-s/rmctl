// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build cgo

package rmpages_test

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/rmpages"
	"github.com/altessa-s/rmctl/internal/domain"
)

func page(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadTypedText(t *testing.T) {
	t.Parallel()
	p, err := rmpages.New(nil).ReadPage(page(t, "Bold_Heading_Bullet_Normal.rm"))
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Paragraph{
		{Text: "A", Style: domain.StyleBold},
		{Text: "new line", Style: domain.StyleHeading},
		{Text: "B is a letter of the alphabet", Style: domain.StyleBullet},
		{Text: "C", Style: domain.StylePlain},
	}
	if !slices.Equal(p.Paragraphs, want) || len(p.Highlights) != 0 || p.Stats.Strokes != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestReadHighlights(t *testing.T) {
	t.Parallel()
	pages := rmpages.New(nil)
	data := page(t, "Wikipedia_highlighted_p1.rm")
	p, err := pages.ReadPage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Highlights) != 4 {
		t.Fatalf("%d highlights", len(p.Highlights))
	}
	first := p.Highlights[0]
	if first.ID != "1:14" || !first.HasText || first.Text != "The reMarkable uses electronic paper" || first.Color != "yellow" || len(first.Rects) != 1 {
		t.Fatalf("%+v", first)
	}
	if len(p.Highlights[3].Rects) != 2 {
		t.Fatalf("%+v", p.Highlights[3])
	}
	ids, err := pages.Highlights(data)
	if err != nil || !slices.Equal(ids, []string{"1:14", "1:15", "1:16", "1:17"}) {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestReadStrokes(t *testing.T) {
	t.Parallel()
	p, err := rmpages.New(nil).ReadPage(page(t, "highlighter-blue-orange.rm"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Stats.Strokes == 0 || len(p.Stats.StrokesByTool) == 0 || len(p.Stats.StrokesByColor) == 0 {
		t.Fatalf("%+v", p.Stats)
	}
	total := 0
	for _, n := range p.Stats.StrokesByTool {
		total += n
	}
	if total != p.Stats.Strokes {
		t.Fatalf("tools count %d of %d strokes", total, p.Stats.Strokes)
	}
}

func TestEraseHighlights(t *testing.T) {
	t.Parallel()
	pages := rmpages.New(nil)
	for _, name := range []string{"Wikipedia_highlighted_p1.rm", "highlighter-blue-orange.rm"} {
		data := page(t, name)
		ids, err := pages.Highlights(data)
		if err != nil || len(ids) == 0 {
			t.Fatalf("%s: %v %v", name, ids, err)
		}
		erased, err := pages.Erase(data, ids)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bytes.Equal(erased, data) {
			t.Fatalf("%s: nothing changed", name)
		}
		left, err := pages.Highlights(erased)
		if err != nil || len(left) != 0 {
			t.Fatalf("%s: left %v %v", name, left, err)
		}
	}
}

func TestEraseRefuses(t *testing.T) {
	t.Parallel()
	pages := rmpages.New(nil)
	data := page(t, "Wikipedia_highlighted_p1.rm")
	var malformed *domain.MalformedFileError
	if _, err := pages.Erase(data, []string{"9:999"}); !errors.As(err, &malformed) {
		t.Fatalf("an unknown item: %v", err)
	}
	if _, err := pages.Erase(data, []string{"not an id"}); !errors.As(err, &malformed) {
		t.Fatalf("a bad identifier: %v", err)
	}
}

func TestReadRefusesGarbage(t *testing.T) {
	t.Parallel()
	pages := rmpages.New(slog.New(slog.DiscardHandler))
	var malformed *domain.MalformedFileError
	for _, data := range [][]byte{nil, []byte("not a page")} {
		if _, err := pages.ReadPage(data); !errors.As(err, &malformed) {
			t.Errorf("%q: %v", data, err)
		}
		if _, err := pages.Highlights(data); err == nil {
			t.Errorf("%q: highlights of garbage", data)
		}
	}
	// A page cut short steps over the blocks it lost rather than failing:
	// whatever it answers, it answers without a panic.
	if p, err := pages.ReadPage(page(t, "Normal_AB.rm")[:50]); err == nil && len(p.Paragraphs) > 1 {
		t.Fatalf("a page cut short grew: %+v", p)
	}
}

func TestWriteRoundTrip(t *testing.T) {
	t.Parallel()
	pages := rmpages.New(nil)
	paras := []domain.Paragraph{
		{Text: "Title", Style: domain.StyleHeading},
		{Text: "Sub", Style: domain.StyleBold},
		{Text: "item", Style: domain.StyleBullet},
		{Text: "step", Style: domain.StyleNumbered},
		{Text: "deeper item", Style: domain.StyleBulletNested},
		{Text: "deeper step", Style: domain.StyleNumberedNested},
		{Text: "to do", Style: domain.StyleCheckbox},
		{Text: "done", Style: domain.StyleCheckboxDone},
		{Text: "plain text, ünïcode", Style: domain.StylePlain},
		{Text: "", Style: domain.StylePlain},
	}
	for _, d := range []domain.Device{domain.DeviceRM2, domain.DevicePro, domain.DeviceMove} {
		sheet := domain.SheetOf(d)
		draft := domain.PageDraft{Author: [16]byte{1, 2, 3}, Sheet: sheet, Column: domain.ColumnFor(sheet), Paragraphs: paras}
		data, err := pages.WritePage(draft)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		again, err := pages.WritePage(draft)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("%s: the same draft wrote other bytes", d)
		}
		got, err := pages.ReadPage(data)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if !slices.Equal(got.Paragraphs, paras) {
			t.Fatalf("%s: %+v", d, got.Paragraphs)
		}
	}
}

func TestWriteRefuses(t *testing.T) {
	t.Parallel()
	draft := domain.PageDraft{Sheet: domain.SheetOf(domain.DeviceRM2), Paragraphs: []domain.Paragraph{{Text: "a\x00b"}}}
	if _, err := rmpages.New(nil).WritePage(draft); err == nil {
		t.Fatal("a NUL byte was written")
	}
}

func BenchmarkReadPage(b *testing.B) {
	pages := rmpages.New(nil)
	for _, name := range []string{"Wikipedia_highlighted_p1.rm", "highlighter-blue-orange.rm"} {
		data := page(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := pages.ReadPage(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWritePage(b *testing.B) {
	pages := rmpages.New(nil)
	paras := make([]domain.Paragraph, 30)
	for i := range paras {
		paras[i] = domain.Paragraph{Text: "A line of typed text that fills most of a column on the page.", Style: domain.StylePlain}
	}
	sheet := domain.SheetOf(domain.DeviceRM2)
	draft := domain.PageDraft{Sheet: sheet, Column: domain.ColumnFor(sheet), Paragraphs: paras}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := pages.WritePage(draft); err != nil {
			b.Fatal(err)
		}
	}
}

// A page's marks are exactly what unmark erases: the highlighter's
// highlights and its strokes.
func TestReadCountsMarks(t *testing.T) {
	t.Parallel()
	p := rmpages.New(nil)
	for _, name := range []string{"highlighter-blue-orange.rm", "Wikipedia_highlighted_p1.rm", "Normal_AB.rm"} {
		data := page(t, name)
		got, err := p.ReadPage(data)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := p.Highlights(data)
		if err != nil {
			t.Fatal(err)
		}
		if got.Stats.Marks != len(ids) {
			t.Errorf("%s: %d marks, %d erasable items", name, got.Stats.Marks, len(ids))
		}
	}
	if marks, _ := p.ReadPage(page(t, "highlighter-blue-orange.rm")); marks.Stats.Marks <= len(marks.Highlights) {
		t.Fatalf("the highlighter strokes are not counted: %d marks, %d highlights", marks.Stats.Marks, len(marks.Highlights))
	}
}
