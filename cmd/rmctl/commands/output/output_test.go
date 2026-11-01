// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
)

func render(t *testing.T, r output.Renderer, res output.Result, s output.Style) string {
	t.Helper()
	var b strings.Builder
	if err := r.Render(&b, res, s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func doc(blocks ...output.Block) output.Result { return output.Result{Doc: blocks} }

func TestWidth(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]int{
		"Paper":           5,
		"Тетрадь":         7, // Cyrillic: one column a rune
		"日本語":             6, // CJK: two columns a rune
		"📓 Notes":         8, // an emoji takes two
		"e\u0301te\u0301": 3, // combining acute accents take none
		"":                0,
	} {
		if got := output.Width(s); got != want {
			t.Errorf("Width(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"Paper", 10, "Paper"},
		{"Quarterly report", 8, "Quarter…"},
		{"日本語の本", 5, "日本…"},                  // never half a wide rune
		{"e\u0301te\u0301s", 3, "e\u0301t…"}, // a combining mark stays with its rune
		{"Paper", 0, ""},
	}
	for _, tc := range tests {
		got := output.Truncate(tc.in, tc.w)
		if got != tc.want || output.Width(got) > max(tc.w, 0) {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 5 << 20: "5.0 MiB", 1 << 62: "4096.0 PiB"} {
		if got := output.HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func items() output.Table {
	mod := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	return output.Table{
		Columns: []output.Column{
			{Title: "name", Role: output.RoleName},
			{Title: "size", Role: output.RoleSize},
			{Title: "modified", Role: output.RoleTime},
			{Title: "id", Role: output.RoleID},
			{Title: "pinned", Detail: true},
		},
		Rows: [][]output.Cell{
			{output.Text("Books").With(output.Folder), output.Text(""), output.Time(mod), output.ID("f-1"), output.Bool(false)},
			{output.Text("日本語の本").With(output.Pinned), output.Size(4200), output.Time(time.Time{}), output.ID("d-1"), output.Bool(true)},
			{output.Text("e\u0301te\u0301"), output.Size(12), output.Time(mod), output.ID("d-2"), output.Bool(false)},
		},
	}
}

func TestTableAlignsWideAndCombiningRunes(t *testing.T) {
	t.Parallel()
	got := render(t, output.TableRenderer{}, doc(items()), output.Style{})
	want := "" +
		"NAME             SIZE  MODIFIED          ID\n" +
		"────────────  ───────  ────────────────  ───\n" +
		"Books/              -  2026-10-31 12:00  f-1\n" +
		"日本語の本 *  4.1 KiB  -                 d-1\n" +
		"e\u0301te\u0301              12 B  2026-10-31 12:00  d-2\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	for line := range strings.Lines(got) {
		if w := output.Width(strings.TrimSuffix(line, "\n")); w > 44 {
			t.Fatalf("line of %d columns: %q", w, line)
		}
	}
}

func TestTableTruncatesTheNameOnly(t *testing.T) {
	t.Parallel()
	got := render(t, output.TableRenderer{}, doc(items()), output.Style{Width: 40})
	for line := range strings.Lines(got) {
		if w := output.Width(strings.TrimSuffix(line, "\n")); w > 40 {
			t.Fatalf("line of %d columns in 40:\n%s", w, got)
		}
	}
	if !strings.Contains(got, "日本… *") || !strings.Contains(got, "2026-10-31 12:00") || !strings.Contains(got, "d-1") {
		t.Fatalf("%s", got)
	}
	// Never narrower than the floor: a very narrow terminal wraps instead.
	if narrow := render(t, output.TableRenderer{}, doc(items()), output.Style{Width: 5}); !strings.Contains(narrow, "Books/") {
		t.Fatalf("%s", narrow)
	}
}

func TestPlain(t *testing.T) {
	t.Parallel()
	rec := output.Record{Fields: []output.Field{
		{Key: "name", Value: output.Text("a\tb\nc\\d")},
		{Key: "size", Value: output.Size(4200)},
		{Key: "modified", Value: output.Time(time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600)))},
		{Key: "pinned", Value: output.Bool(true)},
		{Key: "tags", Value: output.Text("")},
		{Key: "pages", Value: output.Count(3)},
	}}
	got := render(t, output.PlainRenderer{}, doc(items(), rec, output.Body{Text: "free text"}), output.Style{Color: true, Width: 10})
	want := "Books\t-\t2026-10-31T12:00:00Z\tf-1\tfalse\n" +
		"日本語の本\t4200\t-\td-1\ttrue\n" +
		"e\u0301te\u0301\t12\t2026-10-31T12:00:00Z\td-2\tfalse\n" +
		"\n" +
		"name\ta\\tb\\nc\\\\d\nsize\t4200\nmodified\t2026-01-02T02:04:05Z\npinned\ttrue\ntags\t-\npages\t3\n" +
		"\n" +
		"free text\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestEmptyBlocksPrintNothing(t *testing.T) {
	t.Parallel()
	empty := output.Table{Columns: []output.Column{{Title: "name"}}}
	for _, r := range []output.Renderer{output.PlainRenderer{}, output.TableRenderer{}} {
		if got := render(t, r, doc(empty, output.Record{}, output.Tree{}, output.Body{}), output.Style{}); got != "" {
			t.Fatalf("%T: %q", r, got)
		}
		// An empty block leaves no blank line between the others.
		if got := render(t, r, doc(output.Body{Text: "a"}, empty, output.Body{Text: "b\n"}), output.Style{}); got != "a\n\nb\n" {
			t.Fatalf("%T: %q", r, got)
		}
	}
}

func TestTree(t *testing.T) {
	t.Parallel()
	single := output.Tree{Nodes: []output.Node{{Cells: []output.Cell{output.Text("Only"), output.ID("o")}}}}
	if got := render(t, output.TableRenderer{}, doc(single), output.Style{}); got != "Only  o\n" {
		t.Fatalf("%q", got)
	}
	leaf := func(name string, children ...output.Node) output.Node {
		return output.Node{Cells: []output.Cell{output.Text(name)}, Children: children}
	}
	deep := output.Tree{Nodes: []output.Node{
		leaf("a", leaf("b", leaf("c", leaf("d")), leaf("e")), leaf("f")),
		leaf("g"),
	}}
	want := "a\n" +
		"├── b\n" +
		"│   ├── c\n" +
		"│   │   └── d\n" +
		"│   └── e\n" +
		"└── f\n" +
		"g\n"
	if got := render(t, output.TableRenderer{}, doc(deep), output.Style{}); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := render(t, output.PlainRenderer{}, doc(deep), output.Style{}); got != "a\n  b\n    c\n      d\n    e\n  f\ng\n" {
		t.Fatalf("%q", got)
	}
}

func TestRecordAndColor(t *testing.T) {
	t.Parallel()
	rec := output.Record{Fields: []output.Field{{Key: "sync_scope", Value: output.Text("x")}, {Key: "id", Value: output.ID("d")}}}
	if got := render(t, output.TableRenderer{}, doc(rec), output.Style{}); got != "sync scope  x\nid          d\n" {
		t.Fatalf("%q", got)
	}
	got := render(t, output.TableRenderer{}, doc(items()), output.Style{Color: true})
	for _, want := range []string{"\x1b[1mNAME\x1b[0m", "\x1b[1;34mBooks/\x1b[0m", "\x1b[33m *\x1b[0m", "\x1b[2md-1\x1b[0m"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	trashed := output.Table{Columns: []output.Column{{Title: "name"}, {Title: "n", Role: output.RoleCount}},
		Rows: [][]output.Cell{{output.Text("Old").With(output.Trashed), output.Count(1)}}}
	if got := render(t, output.TableRenderer{}, doc(trashed), output.Style{Color: true}); !strings.Contains(got, "\x1b[2mOld\x1b[0m   \x1b[2m1\x1b[0m") {
		t.Fatalf("a trashed row is not dim: %q", got)
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()
	got := render(t, output.JSONRenderer{}, output.Result{JSON: map[string]int{"b": 2, "a": 1}}, output.Style{})
	if got != "{\n  \"a\": 1,\n  \"b\": 2\n}\n" {
		t.Fatalf("%q", got)
	}
	// An answer with no JSON value is its text.
	if got := render(t, output.JSONRenderer{}, doc(output.Body{Text: "registered"}), output.Style{}); got != "registered\n" {
		t.Fatalf("%q", got)
	}
}

func stream(t *testing.T, r output.Renderer, s output.Style) string {
	t.Helper()
	var b strings.Builder
	cols := []output.Column{{Title: "time", Role: output.RoleTime}, {Title: "event", Role: output.RoleKind}, {Title: "name", Role: output.RoleName}}
	em, err := output.Stream(&b, r, cols, s)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	for i, name := range []string{"Notes", "A name far longer than the column it is given"} {
		row := output.Row{Cells: []output.Cell{output.Time(at), output.Text("Sync"), output.Text(name)}, JSON: map[string]int{"n": i}}
		if err := em.Row(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := em.End([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestStreams(t *testing.T) {
	t.Parallel()
	if got := stream(t, output.PlainRenderer{}, output.Style{}); got != "2026-11-01T09:30:00Z\tSync\tNotes\n"+
		"2026-11-01T09:30:00Z\tSync\tA name far longer than the column it is given\n" {
		t.Fatalf("%q", got)
	}
	got := stream(t, output.TableRenderer{}, output.Style{Width: 50})
	lines := slices.Collect(strings.Lines(got))
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "TIME") || !strings.HasPrefix(lines[1], "────") ||
		!strings.Contains(lines[3], "…") {
		t.Fatalf("%q", got)
	}
	for _, l := range lines {
		if w := output.Width(strings.TrimSuffix(l, "\n")); w > 50 {
			t.Fatalf("line of %d columns in 50: %q", w, l)
		}
	}
	// json writes each row's value as it comes, then the final one.
	if got := stream(t, output.JSONRenderer{}, output.Style{}); got != "{\n  \"n\": 0\n}\n{\n  \"n\": 1\n}\n[\n  1,\n  2\n]\n" {
		t.Fatalf("%q", got)
	}
}

// csvish is a renderer that only writes whole answers, as a later format
// might: a stream reaches it as one table at the end.
type csvish struct{}

func (csvish) Render(w io.Writer, r output.Result, _ output.Style) error {
	for _, b := range r.Doc {
		t, ok := b.(output.Table)
		if !ok {
			return errors.New("not a table")
		}
		for _, row := range t.Rows {
			vals := make([]string, len(row))
			for i, c := range row {
				vals[i] = output.PlainValue(c)
			}
			if _, err := io.WriteString(w, strings.Join(vals, ",")+"\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestRegistryAndBufferedStream(t *testing.T) {
	t.Parallel()
	output.Register("csvish-test", csvish{})
	r, ok := output.Lookup("csvish-test")
	if !ok || !slices.Contains(output.Names(), "csvish-test") || !slices.Contains(output.Names(), output.FormatTable) {
		t.Fatal(output.Names())
	}
	if _, ok := output.Lookup("nope"); ok {
		t.Fatal("found an unregistered renderer")
	}
	if got := stream(t, r, output.Style{}); got != "2026-11-01T09:30:00Z,Sync,Notes\n2026-11-01T09:30:00Z,Sync,A name far longer than the column it is given\n" {
		t.Fatalf("%q", got)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestFailingWriter(t *testing.T) {
	t.Parallel()
	for _, r := range []output.Renderer{output.PlainRenderer{}, output.TableRenderer{}, output.JSONRenderer{}} {
		if r.Render(failWriter{}, output.Result{JSON: 1, Doc: output.Doc{output.Body{Text: "x"}}}, output.Style{}) == nil {
			t.Fatalf("%T: a failing writer went unnoticed", r)
		}
	}
	if _, err := (output.TableRenderer{}).Stream(failWriter{}, []output.Column{{Title: "a"}}, output.Style{}); err == nil {
		t.Fatal("a failing header went unnoticed")
	}
}
