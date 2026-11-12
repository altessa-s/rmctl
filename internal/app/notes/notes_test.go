// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package notes_test

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/notes"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

type fixture struct {
	s     *notes.Service
	cloud *porttest.Cloud
	files *porttest.MemFiles
	pages *porttest.Pages
}

// marked is a page with one highlight of words.
func marked(words string) []byte {
	return porttest.PageBytes(nil, []domain.Highlight{{ID: "1:1", Text: words, HasText: true, Color: "yellow"}}, 0)
}

// typed is a page of typed text and no highlight.
var typed = porttest.PageBytes([]domain.Paragraph{{Text: "a thought", Style: domain.StylePlain}}, nil, 0) //nolint:gochecknoglobals // a fixed page

// docs are the library of setup:
//
//	/Books/Paper.pdf     d1   pdf, highlighted on page 2
//	/Books/Paper         d2   notebook, highlighted, the same note name as d1
//	/Books/Plain.pdf     d3   pdf never opened: no page file
//	/Books/Typed         d4   notebook of typed text, no highlight
//	/Books/Sub/a/b       d7   a slash in its name, highlighted
//	/Old                 d5   in the trash, which the tablet keeps at the top
//	/Top                 d0   highlighted, outside /Books
func docs() []porttest.Doc {
	return []porttest.Doc{
		{ID: "d0aaaaaa", Name: "Top", Pages: [][]byte{marked("top")}},
		{ID: "f1aaaaaa", Name: "Books", Folder: true},
		{ID: "d1aaaaaa", Name: "Paper.pdf", Parent: "f1aaaaaa", Ext: "pdf", Tags: []string{"to read"}, Pages: [][]byte{typed, marked("the marked words")}},
		{ID: "d2aaaaaa", Name: "Paper", Parent: "f1aaaaaa", Pages: [][]byte{marked("notebook words")}},
		{ID: "d3aaaaaa", Name: "Plain.pdf", Parent: "f1aaaaaa", Ext: "pdf"},
		{ID: "d4aaaaaa", Name: "Typed", Parent: "f1aaaaaa", Pages: [][]byte{typed}},
		{ID: "f2aaaaaa", Name: "Sub", Parent: "f1aaaaaa", Folder: true},
		{ID: "d7aaaaaa", Name: "a/b", Parent: "f2aaaaaa", Pages: [][]byte{marked("deep")}},
		{ID: "d5aaaaaa", Name: "Old", Parent: "f1aaaaaa", Trashed: true, Pages: [][]byte{marked("old")}},
	}
}

func setup(t *testing.T) fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(docs()...)
	lib := library.New(c, c, c)
	p := porttest.NewPages()
	files := porttest.NewMemFiles()
	return fixture{s: notes.New(lib, pages.NewReader(lib, p), files), cloud: c, files: files, pages: p}
}

// put stores d in place of the document of its identifier, or adds it.
func (f fixture) put(d porttest.Doc) {
	records := f.cloud.Records()
	rec := f.cloud.Record(d)
	i := slices.IndexFunc(records, func(r domain.Record) bool { return r.ID == d.ID })
	if i < 0 {
		records = append(records, rec)
	} else {
		records[i] = rec
	}
	f.cloud.SetRoot(records)
}

// drop takes the document id out of the library.
func (f fixture) drop(id domain.ItemID) {
	f.cloud.SetRoot(slices.DeleteFunc(f.cloud.Records(), func(r domain.Record) bool { return r.ID == id }))
}

func (f fixture) run(t *testing.T, query string, opts port.NoteOptions, tree port.NotesTreeOptions) port.NotesTreeReport {
	t.Helper()
	report, err := f.s.NotesTree(t.Context(), query, "out", opts, tree)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// rows are a report's documents as "status path document", in order.
func rows(r port.NotesTreeReport) []string {
	out := make([]string, len(r.Documents))
	for i, d := range r.Documents {
		out[i] = fmt.Sprintf("%s %s %s", d.Status, d.Path, d.LibraryPath)
	}
	return out
}

// written are the files written, the record aside.
func written(f fixture) []string {
	var out []string
	for _, p := range f.files.Paths() {
		if path.Base(p) != notes.RecordName {
			out = append(out, p)
		}
	}
	return out
}

func (f fixture) note(t *testing.T, p string) string {
	t.Helper()
	data, ok := f.files.File(p)
	if !ok {
		t.Fatalf("no %s in %v", p, f.files.Paths())
	}
	return string(data)
}

func TestNotesTreeMirrorsTheFolders(t *testing.T) {
	t.Parallel()
	for _, jobs := range []int{0, 1, 8} {
		f := setup(t)
		report := f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{Jobs: jobs})
		want := []string{
			"written out/Paper (d2aaaaaa).md /Books/Paper",
			"written out/Paper.md /Books/Paper.pdf",
			"skipped out/Plain.md /Books/Plain.pdf",
			"written out/Sub/a-b.md /Books/Sub/a/b",
			"skipped out/Typed.md /Books/Typed",
		}
		if got := rows(report); report.Dir != "out" || !slices.Equal(got, want) {
			t.Fatalf("jobs %d: %s %q", jobs, report.Dir, got)
		}
		if got := written(f); !slices.Equal(got, []string{"out/Paper (d2aaaaaa).md", "out/Paper.md", "out/Sub/a-b.md"}) {
			t.Fatalf("jobs %d: %v", jobs, got)
		}
		for _, d := range report.Documents {
			if d.Status != port.NoteWritten {
				continue
			}
			// Each note takes the document's modification time.
			if mtime, ok, _ := f.files.ModTime(d.Path); !ok || !mtime.Equal(porttest.Epoch) || d.Highlights != 1 {
				t.Fatalf("%s: %v %v %d", d.Path, mtime, ok, d.Highlights)
			}
		}
		note := f.note(t, "out/Paper.md")
		for _, part := range []string{
			"title: \"Paper\"\n", "remarkable-id: \"d1aaaaaa\"\n", "remarkable-path: \"/Books/Paper.pdf\"\n", "type: pdf\n",
			"pages: 2\n", "highlights: 1\n", "tags:\n  - \"to-read\"\n", "### Page 2\n\n> the marked words\n>\n> — p. 2 #highlight/yellow\n",
			"## Typed text\n\n### Page 1\n\na thought\n",
		} {
			if !strings.Contains(note, part) {
				t.Fatalf("jobs %d: %q lacks %q", jobs, note, part)
			}
		}
	}
}

func TestNotesTreeDefaultDirectoryAndTrash(t *testing.T) {
	t.Parallel()
	f := setup(t)
	report, err := f.s.NotesTree(t.Context(), "f2", "", port.NoteOptions{}, port.NotesTreeOptions{})
	if err != nil || report.Dir != "Sub" || !slices.Equal(written(f), []string{"Sub/a-b.md"}) {
		t.Fatalf("%+v %v %v", report, err, written(f))
	}
	// The whole library goes into a directory of its own; the trash with
	// --all.
	f = setup(t)
	report, err = f.s.NotesTree(t.Context(), "/", "", port.NoteOptions{}, port.NotesTreeOptions{All: true})
	// The tablet keeps an item in the trash at the top.
	want := []string{"remarkable/Books/Paper (d2aaaaaa).md", "remarkable/Books/Paper.md", "remarkable/Books/Sub/a-b.md", "remarkable/Old.md", "remarkable/Top.md"}
	if err != nil || report.Dir != "remarkable" || !slices.Equal(written(f), want) {
		t.Fatalf("%+v %v %v", report, err, written(f))
	}
}

func TestNotesTreeAllDocs(t *testing.T) {
	t.Parallel()
	f := setup(t)
	report := f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{AllDocs: true})
	if report.Count(port.NoteWritten) != 5 || report.Count(port.NoteSkipped) != 0 {
		t.Fatalf("%q", rows(report))
	}
	// A PDF never opened has its pages, empty.
	plain := f.note(t, "out/Plain.md")
	if !strings.Contains(plain, "pages: 1\nhighlights: 0\n") || strings.Contains(plain, "## Highlights") || strings.Contains(plain, "## Typed text") {
		t.Fatalf("%q", plain)
	}
}

func TestNotesTreeLinks(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.run(t, "/Books", port.NoteOptions{PDFLink: "{root}../pdf/{path}.pdf#page={page}&id={id}&n={name}"}, port.NotesTreeOptions{})
	if note := f.note(t, "out/Sub/a-b.md"); !strings.Contains(note, "> — [p. 1](<../../pdf/Sub/a-b.pdf#page=1&id=d7aaaaaa&n=a-b>) #highlight/yellow\n") {
		t.Fatalf("%q", note)
	}
	if _, err := f.s.NotesTree(t.Context(), "/Books", "out", port.NoteOptions{PDFLink: "{nope}"}, port.NotesTreeOptions{}); !errors.Is(err, domain.ErrBadPageLink) {
		t.Fatal(err)
	}
}

func TestNotesTreeIncremental(t *testing.T) {
	t.Parallel()
	f := setup(t)
	inc := port.NotesTreeOptions{Incremental: true}
	f.run(t, "/Books", port.NoteOptions{}, inc)
	reads := f.pages.Count("ReadPage")
	report := f.run(t, "/Books", port.NoteOptions{}, inc)
	want := []string{
		"unchanged out/Paper (d2aaaaaa).md /Books/Paper",
		"unchanged out/Paper.md /Books/Paper.pdf",
		"skipped out/Plain.md /Books/Plain.pdf",
		"unchanged out/Sub/a-b.md /Books/Sub/a/b",
		"skipped out/Typed.md /Books/Typed",
	}
	// Nothing is read again: not even the document with no highlight.
	if got := rows(report); !slices.Equal(got, want) || f.pages.Count("ReadPage") != reads {
		t.Fatalf("%q %d %d", got, f.pages.Count("ReadPage"), reads)
	}
	// A note of another time is written again, and so is a missing one.
	if err := f.files.SetModTime("out/Paper.md", porttest.Epoch.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.files.Remove("out/Sub/a-b.md"); err != nil {
		t.Fatal(err)
	}
	// A document changed is read again: Typed now has a highlight.
	d := docs()[5]
	d.Pages, d.Modified = [][]byte{marked("now marked")}, porttest.Epoch.Add(time.Hour)
	f.put(d)
	report = f.run(t, "/Books", port.NoteOptions{}, inc)
	want = []string{
		"unchanged out/Paper (d2aaaaaa).md /Books/Paper",
		"written out/Paper.md /Books/Paper.pdf",
		"skipped out/Plain.md /Books/Plain.pdf",
		"written out/Sub/a-b.md /Books/Sub/a/b",
		"written out/Typed.md /Books/Typed",
	}
	if got := rows(report); !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	// Other options write every note again.
	report = f.run(t, "/Books", port.NoteOptions{PDFLink: "{path}.pdf"}, inc)
	if report.Count(port.NoteWritten) != 4 || report.Count(port.NoteUnchanged) != 0 {
		t.Fatalf("%q", rows(report))
	}
	// A record damaged keeps nothing.
	f.files.Write("out/"+notes.RecordName, []byte("{not json"))
	if report = f.run(t, "/Books", port.NoteOptions{PDFLink: "{path}.pdf"}, inc); report.Count(port.NoteWritten) != 4 {
		t.Fatalf("%q", rows(report))
	}
	// Without --incremental every document is read.
	reads = f.pages.Count("ReadPage")
	if report = f.run(t, "/Books", port.NoteOptions{PDFLink: "{path}.pdf"}, port.NotesTreeOptions{}); report.Count(port.NoteWritten) != 4 ||
		f.pages.Count("ReadPage") == reads {
		t.Fatalf("%q", rows(report))
	}
}

// A note follows its document: one whose highlights were all erased gets
// a note without them, rather than keeping the old one.
func TestNotesTreeNoteFollowsItsDocument(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.run(t, "/Books/Sub", port.NoteOptions{}, port.NotesTreeOptions{Incremental: true})
	d := docs()[7]
	d.Pages, d.Modified = [][]byte{typed}, porttest.Epoch.Add(time.Hour)
	f.put(d)
	report := f.run(t, "/Books/Sub", port.NoteOptions{}, port.NotesTreeOptions{Incremental: true})
	if got := rows(report); !slices.Equal(got, []string{"written out/a-b.md /Books/Sub/a/b"}) || report.Documents[0].Highlights != 0 {
		t.Fatalf("%q", got)
	}
	if note := f.note(t, "out/a-b.md"); !strings.Contains(note, "highlights: 0\n") || strings.Contains(note, "deep") {
		t.Fatalf("%q", note)
	}
}

func TestNotesTreeOrphans(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{})
	// d2 is deleted and d7 renamed; a record entry naming a path outside
	// the directory is never followed.
	f.drop("d2aaaaaa")
	d := docs()[7]
	d.Name = "c"
	f.put(d)
	record, _ := f.files.File("out/" + notes.RecordName)
	f.files.Write("out/"+notes.RecordName, []byte(strings.Replace(string(record), `"documents":{`,
		`"documents":{"../escape.md":{"id":"x","modified":"2026-01-01T00:00:00Z","options":"note-1"},"Gone.txt":{"id":"y","modified":"2026-01-01T00:00:00Z","options":"note-1"},`, 1)))
	f.files.Write("escape.md", []byte("not mine"))
	report := f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{})
	if want := []string{"out/Paper (d2aaaaaa).md", "out/Sub/a-b.md"}; !slices.Equal(report.Left, want) || len(report.Moved) != 0 {
		t.Fatalf("%v %v", report.Left, report.Moved)
	}
	// They stay recorded, so a later --prune finds them.
	report = f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{Prune: true})
	want := []string{"out/.rmctl-removed/Paper (d2aaaaaa).md", "out/.rmctl-removed/Sub/a-b.md"}
	if !slices.Equal(report.Moved, want) || len(report.Left) != 0 {
		t.Fatalf("%v %v", report.Moved, report.Left)
	}
	wantFiles := []string{"escape.md", "out/.rmctl-removed/Paper (d2aaaaaa).md", "out/.rmctl-removed/Sub/a-b.md", "out/Paper.md", "out/Sub/c.md"}
	if got := written(f); !slices.Equal(got, wantFiles) {
		t.Fatalf("%v", got)
	}
	record, _ = f.files.File("out/" + notes.RecordName)
	for _, gone := range []string{"escape", "Gone.txt", "d2aaaaaa", "a-b"} {
		if strings.Contains(string(record), gone) {
			t.Fatalf("%s still in %s", gone, record)
		}
	}
	// Nothing is left to prune; a note already gone is only forgotten.
	f.drop("d1aaaaaa")
	if err := f.files.Remove("out/Paper.md"); err != nil {
		t.Fatal(err)
	}
	if report = f.run(t, "/Books", port.NoteOptions{}, port.NotesTreeOptions{Prune: true}); len(report.Moved) != 0 || len(report.Left) != 0 {
		t.Fatalf("%v %v", report.Moved, report.Left)
	}
}

func TestNotesTreePruneFailures(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Rename"} {
		f := setup(t)
		f.run(t, "/Books/Sub", port.NoteOptions{}, port.NotesTreeOptions{})
		f.drop("d7aaaaaa")
		f.files.Fail[method] = errBoom
		report, err := f.s.NotesTree(t.Context(), "/Books/Sub", "out", port.NoteOptions{}, port.NotesTreeOptions{Prune: true})
		if !errors.Is(err, errBoom) || len(report.Moved) != 0 {
			t.Fatalf("%s: %v %v", method, err, report.Moved)
		}
	}
}

func TestNotesTreeRefusals(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for query, want := range map[string]error{"/Books/Paper.pdf": notes.ErrNotFolder, "/Nope": domain.ErrNotFound} {
		if _, err := f.s.NotesTree(t.Context(), query, "out", port.NoteOptions{}, port.NotesTreeOptions{}); !errors.Is(err, want) {
			t.Fatalf("%s: %v", query, err)
		}
	}
	// Folder tells a folder from a document.
	for query, want := range map[string]bool{"/": true, "": true, "/Books": true, "/Books/Paper.pdf": false} {
		if got, err := f.s.Folder(t.Context(), query); err != nil || got != want {
			t.Fatalf("%s: %v %v", query, got, err)
		}
	}
	if _, err := f.s.Folder(t.Context(), "/Nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	f.files.Fail["MkdirAll"] = errBoom
	if _, err := f.s.NotesTree(t.Context(), "/Books", "out", port.NoteOptions{}, port.NotesTreeOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestNotesTreeLibraryFails(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.cloud.Fail = map[string]error{"Root": errBoom}
	if _, err := f.s.NotesTree(t.Context(), "/Books", "out", port.NoteOptions{}, port.NotesTreeOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if _, err := f.s.Folder(t.Context(), "/Books"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestNotesTreeDocumentsFail(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		set  func(f fixture)
	}{
		{"read", func(f fixture) { f.pages.Fail["ReadPage"] = errBoom }},
		{"write", func(f fixture) { f.files.Fail["Produce"] = errBoom }},
		{"time", func(f fixture) { f.files.Fail["SetModTime"] = errBoom }},
		{"mtime", func(f fixture) { f.files.Fail["ModTime"] = errBoom }},
	} {
		f := setup(t)
		if c.name == "mtime" {
			f.run(t, "/Books/Sub", port.NoteOptions{}, port.NotesTreeOptions{})
		}
		c.set(f)
		report, err := f.s.NotesTree(t.Context(), "/Books/Sub", "out", port.NoteOptions{}, port.NotesTreeOptions{Incremental: true})
		if !errors.Is(err, notes.ErrDocumentsFailed) || report.Count(port.NoteFailed) != 1 || !errors.Is(report.Documents[0].Err, errBoom) {
			t.Fatalf("%s: %v %q", c.name, err, rows(report))
		}
	}
}

func TestNotesTreeRecordFails(t *testing.T) {
	t.Parallel()
	// A record that must drop entries before anything is written, and
	// cannot, stops the run.
	f := setup(t)
	f.run(t, "/Books/Sub", port.NoteOptions{}, port.NotesTreeOptions{})
	f.files.Fail["Produce"] = errBoom
	if _, err := f.s.NotesTree(t.Context(), "/Books/Sub", "out", port.NoteOptions{PDFLink: "{path}"}, port.NotesTreeOptions{}); !errors.Is(err, errBoom) ||
		!strings.Contains(err.Error(), "record the notes in out/"+notes.RecordName) {
		t.Fatal(err)
	}
}

func TestNotesTreeInterrupted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := f.s.NotesTree(ctx, "/Books", "out", port.NoteOptions{}, port.NotesTreeOptions{Prune: true})
	if !errors.Is(err, context.Canceled) || len(report.Documents) == len(docs()) {
		t.Fatalf("%v %q", err, rows(report))
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	lib := library.New(c, c, c)
	reader := pages.NewReader(lib, porttest.NewPages())
	files := porttest.NewMemFiles()
	for name, build := range map[string]func(){
		"lib":    func() { notes.New(nil, reader, files) },
		"reader": func() { notes.New(lib, nil, files) },
		"files":  func() { notes.New(lib, reader, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			build()
		}()
	}
}
