// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// paper is an annotated PDF in the library: tags, two pages of highlights
// out of reading order, one over two lines, and typed text.
func paper() port.DocumentText {
	return port.DocumentText{
		Item: clitest.Item(clitest.Spec{
			ID: "3f2a8c1e-0000-4000-8000-000000000001", Name: "Deep Work: Rules.pdf", Kind: domain.KindAnnotatedPDF,
			Tags: []string{"to read", "#focus", "focus"}, Modified: time.Date(2026, 3, 4, 10, 20, 30, 500, time.UTC),
		}),
		Path: "/Books/Deep Work: Rules.pdf",
		Pages: []domain.Page{
			{ID: "p-1", Number: 1},
			{ID: "p-2", Number: 2, Highlights: []domain.Highlight{
				{ID: "1:9", Text: "lower on the page", HasText: true, Color: "green", Rects: []domain.Rect{{X: 10, Y: 900, Width: 5, Height: 5}}},
				{ID: "1:5", Text: "Deep work is\nrare and valuable.\n", HasText: true, Color: "yellow", Rects: []domain.Rect{{X: 10, Y: 100, Width: 5, Height: 5}}},
				{ID: "1:6"},
			}},
			{ID: "p-3", Number: 3,
				Paragraphs: []domain.Paragraph{{Text: "My take", Style: domain.StyleHeading}, {Text: "worth it", Style: domain.StyleBullet}},
				Highlights: []domain.Highlight{{ID: "1:7", Text: "shallow work", HasText: true, Color: "highlighter-blue"}},
			},
		},
	}
}

func TestHighlightsNote(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Document: paper()}
	r := run(t, f, "highlights", "/Books/Paper", "--format", "note")
	if r.Code != cliutil.CodeOK || r.Err != "" {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
	clitest.Golden(t, "highlights-note", r.Out)
	if got := f.Calls(); !slices.Equal(got, []string{"Pages /Books/Paper"}) {
		t.Fatalf("calls %v", got)
	}
	// The same document writes the same bytes.
	if again := run(t, &clitest.Fake{Document: paper()}, "highlights", "/Books/Paper", "--format", "note"); again.Out != r.Out {
		t.Fatalf("not stable:\n%s", again.Out)
	}
	// A link per page, its path made of the document's safe name.
	r = run(t, &clitest.Fake{Document: paper()}, "highlights", "/Books/Paper", "--format", "note", "--pdf-link", "{root}pdf/{path}.pdf#page={page}")
	if r.Code != cliutil.CodeOK || !strings.Contains(r.Out, "> — [p. 2](<pdf/Deep Work- Rules.pdf#page=2>) #highlight/yellow\n") {
		t.Fatalf("%d %v\n%s", r.Code, r.Error, r.Out)
	}
}

func TestHighlightsNoteOfAnArchive(t *testing.T) {
	t.Parallel()
	local := filepath.Join(t.TempDir(), "backup.rmdoc")
	if err := os.WriteFile(local, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	one := paper()
	one.Path = ""
	r := run(t, &clitest.Fake{Local: []port.DocumentText{one}}, "highlights", local, "--format", "note")
	if r.Code != cliutil.CodeOK || strings.Contains(r.Out, "remarkable-path") || !strings.HasPrefix(r.Out, "---\ntitle: ") {
		t.Fatalf("%d %v\n%s", r.Code, r.Error, r.Out)
	}
	// A note is of one document.
	r = run(t, &clitest.Fake{Local: []port.DocumentText{one, notebook()}}, "highlights", local, "--format", "note")
	if r.Code != cliutil.CodeUsage || !strings.Contains(r.Error.Error(), "holds 2 documents") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

// notesReport is a folder's notes: one document of each status, a note
// moved and one left.
func notesReport() port.NotesTreeReport {
	return port.NotesTreeReport{
		Documents: []port.NotedDocument{
			{Document: domain.Item{ID: "d1"}, LibraryPath: "/Books/A.pdf", Path: "vault/A.md", Status: port.NoteWritten, Highlights: 7},
			{Document: domain.Item{ID: "d2"}, LibraryPath: "/Books/B", Path: "vault/B.md", Status: port.NoteUnchanged},
			{Document: domain.Item{ID: "d3"}, LibraryPath: "/Books/C", Path: "vault/C.md", Status: port.NoteSkipped},
			{Document: domain.Item{ID: "d4"}, LibraryPath: "/Books/Sub/D", Path: "vault/Sub/D.md", Status: port.NoteFailed, Err: errors.New("page 2: bad")},
		},
		Moved: []string{"vault/.rmctl-removed/Old.md"},
		Left:  []string{"vault/Gone.md"},
	}
}

func TestHighlightsTree(t *testing.T) {
	t.Parallel()
	errFailed := errors.New("1 of 4 documents could not be read")
	f := &clitest.Fake{RenderFolders: []string{"/Books"}, NotedTree: notesReport(), NotesErr: errFailed}
	r := run(t, f, "highlights", "/Books", "-o", "vault", "--incremental", "--prune", "--all-docs", "-a", "-j", "3",
		"--pdf-link", "{root}../pdf/{path}.pdf#page={page}", "--format", "note", "--json")
	want := "NotesTree /Books vault link={root}../pdf/{path}.pdf#page={page} all all-docs incremental prune jobs=3"
	if r.Code != cliutil.CodeError || !slices.Equal(f.Calls(), []string{want}) {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	// The answer is written, and then the command fails.
	if r.Error.Error() != "highlights: 1 of 4 documents could not be read" {
		t.Fatal(r.Error)
	}
	clitest.Golden(t, "highlights-tree-json", r.Out)
	if !strings.Contains(r.Err, "1 notes of documents no longer below /Books were left where they are; --prune moves them into .rmctl-removed") {
		t.Fatalf("%q", r.Err)
	}
	// Plain for a pipe: a row per document, the totals, the notes moved.
	tree := notesReport()
	tree.Dir = "remarkable"
	f = &clitest.Fake{NotedTree: tree}
	r = run(t, f, "highlights", "/")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"NotesTree /  link=     jobs=0"}) {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "highlights-tree-plain", r.Out)
	// An empty folder says so.
	f = &clitest.Fake{RenderFolders: []string{"/Empty"}}
	if r = run(t, f, "highlights", "/Empty"); r.Code != cliutil.CodeOK || !strings.Contains(r.Err, "no document below /Empty") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
	// A failure before any document is the command's error alone.
	f = &clitest.Fake{RenderFolders: []string{"/Books"}, NotesErr: domain.ErrPagesUnsupported}
	if r = run(t, f, "highlights", "/Books"); r.Code != cliutil.CodeError || r.Out != "" || !errors.Is(r.Error, cliutil.ErrNoPages) {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
	// So is a library that cannot be read.
	f = &clitest.Fake{Err: port.ErrNotRegistered}
	if r = run(t, f, "highlights", "/Books"); r.Code != cliutil.CodeNotRegistered {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestHighlightsUsage(t *testing.T) {
	t.Parallel()
	local := filepath.Join(t.TempDir(), "backup.rmdoc")
	if err := os.WriteFile(local, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"/Books", "--page", "1"}, "leave --page out"},
		{[]string{"/Books", "--format", "markdown"}, "leave --format out"},
		{[]string{"/Books", "-o", "notes.md"}, "-o names the directory"},
		{[]string{"/Books", "--pdf-link", "{file}.pdf"}, "{file} is no placeholder"},
		{[]string{"/Books/Paper", "--incremental"}, "--incremental is for a folder; a document's highlights are printed"},
		{[]string{"/Books/Paper", "-o", "vault"}, "--output is for a folder"},
		{[]string{"/Books/Paper", "--prune"}, "--prune is for a folder"},
		{[]string{"/Books/Paper", "--pdf-link", "{path}.pdf"}, "--pdf-link is for a note"},
		{[]string{"/Books/Paper", "--format", "pdf"}, "use text, markdown, note or json"},
		{[]string{local, "--all-docs"}, "--all-docs is for a folder"},
	} {
		f := &clitest.Fake{RenderFolders: []string{"/Books"}, Document: paper()}
		r := run(t, f, append([]string{"highlights"}, c.args...)...)
		if r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 || !strings.Contains(r.Error.Error(), c.want) {
			t.Fatalf("%v: %d %v %v", c.args, r.Code, r.Error, f.Calls())
		}
	}
	// text has no note.
	if r := run(t, &clitest.Fake{}, "text", "/Books/Paper", "--format", "note"); r.Code != cliutil.CodeUsage ||
		!strings.Contains(r.Error.Error(), "use text, markdown or json") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}
