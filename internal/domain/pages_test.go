// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestPageOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content string
		want          []string
	}{
		{name: "v1", content: `{"formatVersion":1,"pages":["a","b",""]}`, want: []string{"a", "b"}},
		{name: "v1 none", content: `{"formatVersion":1}`},
		{name: "v1 null", content: `{"pages":null}`},
		{
			name: "v2 by idx",
			content: `{"formatVersion":2,"cPages":{"pages":[` +
				`{"id":"c","idx":{"value":"bd"}},{"id":"a","idx":{"value":"bb"}},{"id":"b","idx":{"value":"bc"}}]}}`,
			want: []string{"a", "b", "c"},
		},
		{
			name: "v2 deleted",
			content: `{"cPages":{"pages":[{"id":"a","idx":{"value":"bb"},"deleted":{"timestamp":"1:2","value":1}},` +
				`{"id":"b","idx":{"value":"bc"},"deleted":{"value":0}},{"id":"c","idx":{"value":"bd"},"deleted":{"value":false}},` +
				`{"id":"","idx":{"value":"ba"}},{"id":"d","idx":{"value":"be"},"deleted":{}}]}}`,
			want: []string{"b", "c", "d"},
		},
		{name: "v2 ties keep file order", content: `{"cPages":{"pages":[{"id":"x","idx":{"value":"bb"}},{"id":"y","idx":{"value":"bb"}}]}}`, want: []string{"x", "y"}},
		{name: "v2 wins over v1", content: `{"pages":["v1"],"cPages":{"pages":[{"id":"v2","idx":{"value":"bb"}}]}}`, want: []string{"v2"}},
		{name: "empty object", content: `{}`},
	}
	for _, tt := range tests {
		got, err := domain.PageOrder([]byte(tt.content))
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v %v", tt.name, got, err)
		}
	}
	var malformed *domain.MalformedFileError
	for _, bad := range []string{`{`, `[]`, `{"pages":"x"}`, `{"pages":[1]}`, `{"cPages":{"pages":"x"}}`} {
		if _, err := domain.PageOrder([]byte(bad)); !errors.As(err, &malformed) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if domain.PageFileName("doc", "p1") != "doc/p1.rm" {
		t.Fatal("page file name")
	}
}

func FuzzPageOrder(f *testing.F) {
	f.Add(`{"pages":["a","b"]}`)
	f.Add(`{"cPages":{"pages":[{"id":"a","idx":{"value":"bc"}},{"id":"b","idx":{"value":"bb"},"deleted":{"value":1}}]}}`)
	f.Fuzz(func(t *testing.T, content string) {
		ids, err := domain.PageOrder([]byte(content))
		if err != nil {
			return
		}
		if slices.Contains(ids, "") {
			t.Fatalf("an empty identifier in %q", content)
		}
	})
}

func TestIndexKey(t *testing.T) {
	t.Parallel()
	want := map[int]string{0: "bb", 1: "bc", 24: "bz", 25: "bzb", 49: "bzz", 50: "bzzb"}
	for i, key := range want {
		if got := domain.IndexKey(i); got != key {
			t.Errorf("%d: %s", i, got)
		}
	}
	// The keys sort as the pages do.
	keys := make([]string, 200)
	for i := range keys {
		keys[i] = domain.IndexKey(i)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys do not sort in page order")
	}
}

func TestDevicesAndColumns(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]domain.Device{
		"": domain.DeviceRM2, "rm2": domain.DeviceRM2, "remarkable-2": domain.DeviceRM2, "PURE": domain.DevicePure,
		"paper-pure": domain.DevicePure, "pro": domain.DevicePro, "paper-pro": domain.DevicePro, "move": domain.DeviceMove,
		"paper-pro-move": domain.DeviceMove,
	} {
		if got, err := domain.ParseDevice(name); err != nil || got != want {
			t.Errorf("%q: %v %v", name, got, err)
		}
	}
	if _, err := domain.ParseDevice("kindle"); !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatal(err)
	}
	for d, col := range map[domain.Device]domain.TextColumn{
		domain.DeviceRM2: domain.ColumnWide, domain.DevicePure: domain.ColumnWide, domain.DevicePro: domain.ColumnWide, domain.DeviceMove: domain.ColumnNarrow,
	} {
		if got := domain.ColumnFor(domain.SheetOf(d)); got != col {
			t.Errorf("%s: %v", d, got)
		}
	}
	if domain.ColumnFor(domain.Sheet{ScreenWidth: 1200}) != domain.ColumnMedium {
		t.Fatal("medium")
	}
	if domain.SheetOf(domain.DeviceMove).CanvasWidth != 820.2954545454545 || domain.SheetOf(domain.DevicePro).ScreenHeight != 2160 {
		t.Fatal("sheets")
	}
	for c, w := range map[domain.TextColumn]float64{domain.ColumnNarrow: 740, domain.ColumnMedium: 936, domain.ColumnWide: 1152} {
		if c.Width() != w {
			t.Errorf("%d: %v", c, c.Width())
		}
	}
}

func TestPaginate(t *testing.T) {
	t.Parallel()
	// rM2: 31 lines a page, 57 characters a line.
	line := domain.Paragraph{Text: "x", Style: domain.StylePlain}
	paras := slices.Repeat([]domain.Paragraph{line}, 62)
	pages, err := domain.Paginate(paras, domain.DeviceRM2)
	if err != nil || len(pages) != 2 || len(pages[0]) != 31 || len(pages[1]) != 31 {
		t.Fatalf("%d pages %v", len(pages), err)
	}
	// A paragraph of three lines does not split across pages.
	long := domain.Paragraph{Text: strings.Repeat("y", 57*3)}
	pages, err = domain.Paginate(append(slices.Repeat([]domain.Paragraph{line}, 30), long), domain.DeviceRM2)
	if err != nil || len(pages) != 2 || len(pages[1]) != 1 || pages[1][0].Text != long.Text {
		t.Fatalf("%d pages %v", len(pages), err)
	}
	// Nothing at all is one empty page.
	pages, err = domain.Paginate(nil, domain.DeviceMove)
	if err != nil || len(pages) != 1 || len(pages[0]) != 1 || pages[0][0].Text != "" {
		t.Fatalf("%+v %v", pages, err)
	}
	if _, err := domain.Paginate(slices.Repeat([]domain.Paragraph{{Text: strings.Repeat("z", 57*31)}}, domain.MaxNotebookPages+1), domain.DeviceRM2); !errors.Is(err, domain.ErrTooManyPages) {
		t.Fatalf("%v", err)
	}
}

func TestNotebookContent(t *testing.T) {
	t.Parallel()
	got := string(domain.NewNotebookContent([]string{"p1", "p2"}, "author-1", []domain.Tag{{Name: "t", Timestamp: 5}}))
	want := `{"cPages":{"lastOpened":{"timestamp":"0:0","value":"p1"},"original":{"timestamp":"0:0","value":-1},"pages":[` +
		`{"id":"p1","idx":{"timestamp":"1:1","value":"bb"},"template":{"timestamp":"1:1","value":"Blank"}},` +
		`{"id":"p2","idx":{"timestamp":"1:1","value":"bc"},"template":{"timestamp":"1:1","value":"Blank"}}],` +
		`"uuids":[{"first":"author-1","second":1}]},"coverPageNumber":-1,"documentMetadata":{},"extraMetadata":{},` +
		`"fileType":"notebook","fontName":"","formatVersion":2,"lineHeight":100,"orientation":"portrait","pageCount":2,` +
		`"pageTags":[],"sizeInBytes":"0","tags":[{"name":"t","timestamp":5}],"textAlignment":"justify","textScale":1,"zoomMode":"bestFit"}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	ids, err := domain.PageOrder([]byte(got))
	if err != nil || !slices.Equal(ids, []string{"p1", "p2"}) {
		t.Fatalf("%v %v", ids, err)
	}
	if empty := string(domain.NewNotebookContent(nil, "a", nil)); !strings.Contains(empty, `"value":""`) || !strings.Contains(empty, `"tags":[]`) {
		t.Fatal(empty)
	}
}

func TestParseMarkdown(t *testing.T) {
	t.Parallel()
	in := "# Title #\n\n## Section\n\nA line\nwrapped **bold** and `code` and [a link](http://x).\n\n**All bold**\n\n" +
		"- one\n* two\n+ three\n  - nested\n- [ ] task\n\n1. first\n2) second\n\n---\n\n> quoted\n\n```\ncode line\n  kept\n```\n\n### deep\n"
	got, err := domain.ParseMarkdown(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Paragraph{
		{Text: "Title", Style: domain.StyleHeading},
		{Text: "Section", Style: domain.StyleBold},
		{Text: "A line wrapped bold and code and a link.", Style: domain.StylePlain},
		{Text: "All bold", Style: domain.StyleBold},
		{Text: "one", Style: domain.StyleBullet},
		{Text: "two", Style: domain.StyleBullet},
		{Text: "three", Style: domain.StyleBullet},
		{Text: "nested", Style: domain.StyleBullet},
		{Text: "[ ] task", Style: domain.StyleBullet},
		{Text: "first", Style: domain.StyleNumbered},
		{Text: "second", Style: domain.StyleNumbered},
		{Text: "———", Style: domain.StylePlain},
		{Text: "quoted", Style: domain.StylePlain},
		{Text: "code line", Style: domain.StylePlain},
		{Text: "  kept", Style: domain.StylePlain},
		{Text: "deep", Style: domain.StyleBold},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %+v\nwant %+v", got, want)
	}
	for _, bad := range []string{"text\n| a | b |\n", "![pic](x.png)\n"} {
		_, err := domain.ParseMarkdown(bad)
		if !errors.Is(err, domain.ErrUnsupportedFile) || !strings.Contains(err.Error(), "line ") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if got, _ := domain.ParseMarkdown("**a** and **b**\r\n"); !slices.Equal(got, []domain.Paragraph{{Text: "a and b", Style: domain.StylePlain}}) {
		t.Fatalf("%+v", got)
	}
}

// Inline emphasis loses its marks; an underscore inside a word, a lone
// mark and a mark with a space inside it are text.
func TestMarkdownInline(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"*one* and _two_":                      "one and two",
		"**bold** _em_ __strong__ *x*":         "bold em strong x",
		"_a_ _b_,_c_":                          "a b,c",
		"***both***":                           "both",
		"call snake_case_name and __init__":    "call snake_case_name and init",
		"my_var and _private":                  "my_var and _private",
		"2 * 3 * 4":                            "2 * 3 * 4",
		"a * b and a _ b":                      "a * b and a _ b",
		"a * not em*":                          "a * not em*",
		"_not em _":                            "_not em _",
		"_a b_c and _d_":                       "_a b_c and d",
		"__a b__c and __d__":                   "__a b__c and d",
		"(_wrapped_) and _ünïcode_":            "(wrapped) and ünïcode",
		"word_ and _word":                      "word_ and _word",
		"_x__y_":                               "_x__y_",
		"a ____ b":                             "a ____ b",
		"__a __b__ c__":                        "__a b c__",
		"`code_with_underscores` and *it*":     "code_with_underscores and it",
		"[*link*](http://x) **[b](http://y)**": "link b",
	} {
		got, err := domain.ParseMarkdown("text\n" + in + "\n")
		if err != nil || len(got) != 1 || got[0].Text != "text "+want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
}

func FuzzParseMarkdown(f *testing.F) {
	f.Add("# a\n- b\n1. c\n---\n```\nx\n```\n")
	f.Add("| t |\n")
	for _, seed := range []string{"*a* _b_ **c** __d__\n", "snake_case_name __init__ _x_\n", "_a b_c _d_ __e f__g\n", "2 * 3 _ 4 * _\n", "___x___ ***y***\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := domain.ParseMarkdown(in)
		if err != nil {
			return
		}
		for _, p := range got {
			if strings.Contains(p.Text, "\n") {
				t.Fatalf("a paragraph holds a line break: %q", p.Text)
			}
		}
	})
}

func TestParsePlainText(t *testing.T) {
	t.Parallel()
	got := domain.ParsePlainText("a\r\n\nb\n")
	if !slices.Equal(got, []domain.Paragraph{{Text: "a", Style: domain.StylePlain}, {Style: domain.StylePlain}, {Text: "b", Style: domain.StylePlain}}) {
		t.Fatalf("%+v", got)
	}
}

func TestContentCoverAndTags(t *testing.T) {
	t.Parallel()
	if !strings.Contains(string(domain.NewPDFContentWithCover([]string{"p"}, 1, nil, domain.CoverFirstPage)), `"coverPageNumber":0`) ||
		!strings.Contains(string(domain.NewEPUBContentWithCover(1, nil, domain.CoverFirstPage)), `"coverPageNumber":0`) ||
		!strings.Contains(string(domain.NewEPUBContent(1, nil)), `"coverPageNumber":-1`) {
		t.Fatal("cover")
	}
	tags := domain.ContentTagList([]byte(`{"tags":[{"name":"a","timestamp":1},{"name":""},{"name":"a","timestamp":2},{"name":"b","timestamp":3}]}`))
	if !slices.Equal(tags, []domain.Tag{{Name: "a", Timestamp: 1}, {Name: "b", Timestamp: 3}}) {
		t.Fatalf("%+v", tags)
	}
	if domain.ContentTagList([]byte("{")) != nil {
		t.Fatal("garbage has tags")
	}
	merged := domain.MergeTags(tags, []string{"b", "c", "", "c"}, 9)
	if !slices.Equal(merged, []domain.Tag{{Name: "a", Timestamp: 1}, {Name: "b", Timestamp: 3}, {Name: "c", Timestamp: 9}}) {
		t.Fatalf("%+v", merged)
	}
}

func TestRewrittenAndEdited(t *testing.T) {
	t.Parallel()
	m, err := domain.ParseMetadata([]byte(`{"visibleName":"A","type":"DocumentType","version":3,"lastOpenedPage":9,"metadatamodified":false}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1730000000000)
	var got map[string]any
	if err := json.Unmarshal(m.Rewritten(4, now).Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(4) || got["lastOpenedPage"] != float64(3) || got["metadatamodified"] != false || got["lastModified"] != "1730000000000" {
		t.Fatalf("%v", got)
	}
	if err := json.Unmarshal(m.Rewritten(20, now).Bytes(), &got); err != nil || got["lastOpenedPage"] != float64(9) {
		t.Fatalf("%v %v", got, err)
	}
	if err := json.Unmarshal(m.Edited(now).Bytes(), &got); err != nil || got["metadatamodified"] != true || got["version"] != float64(4) {
		t.Fatalf("%v %v", got, err)
	}
	if string(m.Bytes()) != `{"lastOpenedPage":9,"metadatamodified":false,"type":"DocumentType","version":3,"visibleName":"A"}` {
		t.Fatalf("the original changed: %s", m.Bytes())
	}
}

func BenchmarkPageOrder(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`{"formatVersion":2,"cPages":{"pages":[`)
	for i := range 2000 {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":"%08d-0000-4000-8000-000000000000","idx":{"timestamp":"1:1","value":%q},"template":{"timestamp":"1:1","value":"Blank"}}`,
			1999-i, domain.IndexKey(1999-i))
	}
	sb.WriteString(`]}}`)
	content := []byte(sb.String())
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		ids, err := domain.PageOrder(content)
		if err != nil || len(ids) != 2000 {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarkdownToPages(b *testing.B) {
	var sb strings.Builder
	for sb.Len() < 100_000 {
		sb.WriteString("## A section heading\n\nA paragraph of prose that runs over a few lines of the column and then ends.\n\n- an item\n- another item\n\n")
	}
	text := sb.String()
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		paras, err := domain.ParseMarkdown(text)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := domain.Paginate(paras, domain.DeviceRM2); err != nil {
			b.Fatal(err)
		}
	}
}

// A GFM table needs no outer pipes: its delimiter row marks it, and the
// refusal names the header line. A pipe in prose is not a table.
func TestParseMarkdownRefusesEveryTable(t *testing.T) {
	t.Parallel()
	for in, line := range map[string]string{
		"intro\na | b\n--- | ---\none | two\n":      "line 2",
		"a | b\n:--- | ---:\n":                      "line 1",
		"| a | b |\n|---|---|\n":                    "line 1",
		"a | b |\n---|---|\n":                       "line 1",
		"one column\n| --- |\n":                     "line 2",
		"text\n\n  x  |  y  \n  -  |  -  \nz | w\n": "line 3",
	} {
		_, err := domain.ParseMarkdown(in)
		if !errors.Is(err, domain.ErrUnsupportedFile) || !strings.Contains(err.Error(), line+":") {
			t.Errorf("%q: %v, want %s", in, err, line)
		}
	}
	for _, prose := range []string{"a | b is not a table\n", "x | y\nnext line\n", "a | b\n---\n", "use a|b or c\n- item\n"} {
		if _, err := domain.ParseMarkdown(prose); err != nil {
			t.Errorf("%q refused: %v", prose, err)
		}
	}
}
