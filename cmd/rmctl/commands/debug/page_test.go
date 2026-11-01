// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func pagesDoc() port.DocumentText {
	return port.DocumentText{
		Item: clitest.Item(clitest.Spec{ID: "d-notes", Name: "Notes", Kind: domain.KindNotebook}),
		Pages: []domain.Page{
			{
				ID: "p-1", Number: 1,
				Paragraphs: []domain.Paragraph{{Text: "Secret plan", Style: domain.StyleHeading}},
				Highlights: []domain.Highlight{{ID: "1:9", Text: "marked words", HasText: true, Color: "yellow", Rects: []domain.Rect{{}, {}}}},
				Stats: domain.PageStats{
					Strokes: 5, StrokesByTool: map[string]int{"fineliner": 3, "pencil": 2}, StrokesByColor: map[string]int{"black": 4, "red": 1},
					AnchoredStrokes: 1, Images: 2, SkippedBlocks: 1, PaperWidth: 1404, PaperHeight: 1872,
				},
			},
			{ID: "p-2", Number: 2},
		},
	}
}

func TestPage(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{Document: pagesDoc()}, "page", "/Notes", "--page", "1,2")
	if r.Code != cliutil.CodeOK {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	clitest.Golden(t, "page", r.Out)
	if strings.Contains(r.Out, "Secret") || strings.Contains(r.Out, "marked words") {
		t.Fatalf("the user's text without --raw:\n%s", r.Out)
	}
	raw := run(t, &clitest.Fake{Document: pagesDoc()}, "page", "/Notes", "--raw")
	if !strings.Contains(raw.Out, "¶ [2] Secret plan") || !strings.Contains(raw.Out, "▌ 1:9 yellow marked words") {
		t.Fatalf("%s", raw.Out)
	}
}

func TestPageJSON(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{Document: pagesDoc()}, "page", "d-notes", "--json")
	clitest.Golden(t, "page-json", r.Out)
	if strings.Contains(r.Out, "Secret") {
		t.Fatal("the user's text without --raw")
	}
	raw := run(t, &clitest.Fake{Document: pagesDoc()}, "page", "d-notes", "--json", "--raw")
	if !strings.Contains(raw.Out, `"text": "Secret plan"`) || !strings.Contains(raw.Out, `"text": "marked words"`) {
		t.Fatalf("%s", raw.Out)
	}
}

func TestPageErrors(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{PagesErr: domain.ErrPagesUnsupported}, "page", "d-x")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoPages) || !strings.HasPrefix(r.Error.Error(), "debug page: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, &clitest.Fake{}, "page"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d", r.Code)
	}
	if r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, "debug", "page", "x"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

func TestCounts(t *testing.T) {
	t.Parallel()
	if got := counts(map[string]int{"b": 1, "a": 1, "c": 3}); got != "c 3, a 1, b 1" {
		t.Fatal(got)
	}
	if counts(nil) != "-" || paper(domain.PageStats{PaperWidth: 1}) != "-" {
		t.Fatal("empty")
	}
}
