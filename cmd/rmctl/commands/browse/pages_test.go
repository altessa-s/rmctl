// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// notebook is a two-page document with every paragraph style and a
// highlight with and without words.
func notebook() port.DocumentText {
	return port.DocumentText{
		Item: clitest.Item(clitest.Spec{ID: "d-notes", Name: "Notes", Kind: domain.KindNotebook}),
		Pages: []domain.Page{
			{ID: "p-1", Number: 1, Paragraphs: []domain.Paragraph{
				{Text: "Meeting", Style: domain.StyleHeading},
				{Text: "Agenda", Style: domain.StyleBold},
				{Text: "first point", Style: domain.StyleBullet},
				{Text: "detail", Style: domain.StyleBulletNested},
				{Text: "", Style: domain.StylePlain},
				{Text: "one", Style: domain.StyleNumbered},
				{Text: "two", Style: domain.StyleNumbered},
				{Text: "sub", Style: domain.StyleNumberedNested},
				{Text: "three", Style: domain.StyleNumbered},
				{Text: "open", Style: domain.StyleCheckbox},
				{Text: "done", Style: domain.StyleCheckboxDone},
				{Text: "Plain prose.", Style: domain.StylePlain},
			}},
			{ID: "p-2", Number: 2,
				Paragraphs: []domain.Paragraph{{Text: "again", Style: domain.StyleNumbered}, {Text: "odd", Style: domain.ParagraphStyle(99)}},
				Highlights: []domain.Highlight{
					{ID: "1:5", Text: "the marked words", HasText: true, Color: "yellow", Rects: []domain.Rect{{X: 1, Y: 2, Width: 3, Height: 4}}},
					{ID: "1:6", Color: ""},
				}},
		},
	}
}

func TestTextFormats(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"text", "markdown", "json"} {
		f := &clitest.Fake{Document: notebook()}
		r := run(t, f, "text", "/Notes", "--page", "1,2", "--page", " ", "--format", format)
		if r.Code != cliutil.CodeOK {
			t.Fatalf("%s: %d %v", format, r.Code, r.Error)
		}
		clitest.Golden(t, "text-"+format, r.Out)
		if got := f.Calls(); !slices.Equal(got, []string{"Pages /Notes 1 2"}) {
			t.Fatalf("%s: calls %v", format, got)
		}
	}
	// --json wins over --format.
	r := run(t, &clitest.Fake{Document: notebook()}, "text", "/Notes", "--format", "markdown", "--json")
	if !strings.HasPrefix(r.Out, "{") {
		t.Fatalf("%q", r.Out)
	}
}

func TestHighlightsFormats(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"text", "markdown", "json"} {
		r := run(t, &clitest.Fake{Document: notebook()}, "highlights", "/Notes", "--format", format)
		if r.Code != cliutil.CodeOK {
			t.Fatalf("%s: %d %v", format, r.Code, r.Error)
		}
		clitest.Golden(t, "highlights-"+format, r.Out)
	}
}

func TestPagesNotes(t *testing.T) {
	t.Parallel()
	empty := port.DocumentText{Item: clitest.Item(clitest.Spec{ID: "d-x", Name: "Empty", Kind: domain.KindPDF}),
		Pages: []domain.Page{{ID: "p", Number: 1, Paragraphs: []domain.Paragraph{{Text: "  "}}}}}
	r := run(t, &clitest.Fake{Document: empty}, "text", "d-x")
	if r.Code != cliutil.CodeOK || r.Err != "Empty has no typed text\n" || r.Out != "  \n" {
		t.Fatalf("%d %q %q", r.Code, r.Err, r.Out)
	}
	r = run(t, &clitest.Fake{Document: empty}, "highlights", "d-x")
	if r.Code != cliutil.CodeOK || r.Err != "Empty has no highlights\n" || r.Out != "" {
		t.Fatalf("%d %q %q", r.Code, r.Err, r.Out)
	}
}

func TestPagesErrors(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"text", "highlights"} {
		r := run(t, &clitest.Fake{PagesErr: domain.ErrPagesUnsupported}, cmd, "d-x")
		if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoPages) ||
			r.Error.Error() != cmd+": this build reads no pages: rebuild with cgo and a C23 compiler" {
			t.Fatalf("%s: %d %v", cmd, r.Code, r.Error)
		}
		r = run(t, &clitest.Fake{PagesErr: domain.ErrNotFound}, cmd, "d-x")
		if r.Code != cliutil.CodeError || !errors.Is(r.Error, domain.ErrNotFound) {
			t.Fatalf("%s: %d %v", cmd, r.Code, r.Error)
		}
		r = run(t, &clitest.Fake{Err: port.ErrNotRegistered}, cmd, "d-x")
		if r.Code != cliutil.CodeNotRegistered {
			t.Fatalf("%s: %d %v", cmd, r.Code, r.Error)
		}
		if r = run(t, &clitest.Fake{}, cmd, "d-x", "--format", "pdf"); r.Code != cliutil.CodeUsage {
			t.Fatalf("%s: %d %v", cmd, r.Code, r.Error)
		}
		if r = run(t, &clitest.Fake{}, cmd); r.Code != cliutil.CodeUsage {
			t.Fatalf("%s: %d %v", cmd, r.Code, r.Error)
		}
		if r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, cmd, "d-x"); !errors.Is(r.Error, cliutil.ErrNoService) {
			t.Fatalf("%s: %v", cmd, r.Error)
		}
	}
}

func TestFindText(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, "find", "--text", "quarterly report")
	if r.Code != cliutil.CodeOK || len(f.Calls()) != 1 || !strings.HasSuffix(f.Calls()[0], " quarterly report") {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	r = run(t, &clitest.Fake{Err: domain.ErrPagesUnsupported}, "find", "--text", "x")
	if !errors.Is(r.Error, cliutil.ErrNoPages) {
		t.Fatal(r.Error)
	}
	// Without --text, the error keeps its own words.
	r = run(t, &clitest.Fake{Err: errors.New("boom")}, "find", "x")
	if r.Error == nil || r.Error.Error() != "find: boom" {
		t.Fatal(r.Error)
	}
}

func TestStyleNames(t *testing.T) {
	t.Parallel()
	for style, want := range map[domain.ParagraphStyle]string{
		domain.StylePlain: "plain", domain.StyleHeading: "heading", domain.StyleBold: "bold", domain.StyleBullet: "bullet",
		domain.StyleBulletNested: "bullet-nested", domain.StyleCheckbox: "checkbox", domain.StyleCheckboxDone: "checkbox-done",
		domain.StyleNumbered: "numbered", domain.StyleNumberedNested: "numbered-nested", domain.ParagraphStyle(0): "plain",
	} {
		if got := StyleName(style); got != want {
			t.Errorf("%d: %q", style, got)
		}
	}
}

func FuzzMarkdown(f *testing.F) {
	f.Add("Heading", 2, "item", 4, "x\ny", 10)
	f.Add("", 1, "**", 3, "- [ ]", 6)
	f.Fuzz(func(t *testing.T, a string, sa int, b string, sb int, c string, sc int) {
		paras := []domain.Paragraph{
			{Text: a, Style: domain.ParagraphStyle(sa)},
			{Text: b, Style: domain.ParagraphStyle(sb)},
			{Text: c, Style: domain.ParagraphStyle(sc)},
		}
		pages := []domain.Page{{Number: 1, Paragraphs: paras}, {Number: 2, Paragraphs: paras[:1]}}
		md := Markdown(pages)
		// Every paragraph's text reaches the output, whatever its style.
		for _, p := range paras {
			if trimmed := strings.TrimSpace(p.Text); trimmed != "" && !strings.Contains(md, trimmed) {
				t.Fatalf("lost %q in %q", p.Text, md)
			}
		}
		if !strings.Contains(md, "## Page 2") || !strings.HasSuffix(md, "\n") {
			t.Fatalf("%q", md)
		}
	})
}

func BenchmarkRenderText(b *testing.B) {
	pages := make([]domain.Page, 500)
	for i := range pages {
		pages[i] = domain.Page{ID: fmt.Sprintf("p-%d", i), Number: i + 1}
		for j := range 30 {
			pages[i].Paragraphs = append(pages[i].Paragraphs, domain.Paragraph{
				Text: "A paragraph of typed text that fills a line or so", Style: domain.ParagraphStyle(1 + j%11),
			})
		}
	}
	doc := port.DocumentText{Pages: pages}
	for name, render := range map[string]func() string{
		"text":     func() string { return PlainText(pages) },
		"markdown": func() string { return Markdown(pages) },
		"json":     func() string { return fmt.Sprint(len(textView(doc).Pages)) },
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if render() == "" {
					b.Fatal("empty")
				}
			}
		})
	}
}

// text keeps its own --format, which shadows the global one; $RMCTL_FORMAT
// reaches it only as json, and a table or plain format leaves it text.
func TestTextKeepsItsOwnFormat(t *testing.T) {
	t.Parallel()
	exec := func(env map[string]string, args ...string) clitest.Result {
		return clitest.Exec(t, (&clitest.Fake{Document: notebook()}).Services(), clitest.Options{Env: env, OutTerminal: true}, Commands, args...)
	}
	md := exec(nil, "text", "/Notes", "--format", "markdown")
	if md.Code != 0 || !strings.HasPrefix(md.Out, "## Page 1") {
		t.Fatalf("%d %v %q", md.Code, md.Error, md.Out)
	}
	if r := exec(map[string]string{"RMCTL_FORMAT": "json"}, "text", "/Notes"); r.Code != 0 || !strings.HasPrefix(r.Out, "{") {
		t.Fatalf("%q", r.Out)
	}
	if r := exec(map[string]string{"RMCTL_FORMAT": "json"}, "text", "/Notes", "--format", "markdown"); r.Out != md.Out {
		t.Fatalf("its own flag loses to the environment: %q", r.Out)
	}
	if r := exec(map[string]string{"RMCTL_FORMAT": "table"}, "text", "/Notes"); r.Code != 0 || !strings.HasPrefix(r.Out, "Page 1") {
		t.Fatalf("%q", r.Out)
	}
}
