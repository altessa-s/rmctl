// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// New files go up into the folders of their directories, created on the
// way, and a second pass finds everything in step.
func TestPassUploadsNewFiles(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("Paper.pdf", "%PDF one", 1, now.Add(-time10s))
	f.epubFile("Книги/Книга.epub", "book")
	f.files.Write(filepath.Join(dir, "notes.txt"), []byte("not uploaded"))
	f.files.Write(filepath.Join(dir, ".hidden.pdf"), []byte("%PDF"))
	f.files.Write(filepath.Join(dir, "~$draft.pdf"), []byte("%PDF"))
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "upload Paper.pdf", "upload Книги/Книга.epub")
	if p.Err != nil || p.Pending != 0 || p.Lite {
		t.Fatalf("%+v", p)
	}
	paper, ok := f.doc("Paper")
	if !ok {
		t.Fatal("Paper was not uploaded")
	}
	if path, _ := f.path(paper.ID); path != "/Papers/Paper" {
		t.Fatalf("Paper went to %s", path)
	}
	book, ok := f.doc("Книга")
	if !ok || book.Kind != domain.KindEPUB {
		t.Fatalf("%+v %v", book, ok)
	}
	if path, _ := f.path(book.ID); path != "/Papers/Книги/Книга" {
		t.Fatalf("the book went to %s", path)
	}
	if len(f.items()) != 2 || f.journal()["folder"] != "f0aaaaaa" {
		t.Fatalf("journal: %v", f.journal())
	}
	want(t, f.pass(port.MirrorRunOptions{}))
}

// time10s is ten seconds.
const time10s = 10 * 1e9

// A file a document of its name already holds is paired with it, and the
// document, annotated, is drawn into _annotated.
func TestPassAdoptsAndDraws(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Clean", Parent: "f0aaaaaa", Ext: "pdf"},
		porttest.Doc{ID: "d3aaaaaa", Name: "Tablet notes", Parent: "f0aaaaaa"},
	)
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pdfFile("Clean.pdf", "%PDF clean", 1, time10)
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "adopt Clean.pdf", "adopt Paper.pdf", "draw Paper.pdf -> _annotated/Paper.pdf")
	got, ok := f.local("_annotated/Paper.pdf")
	if !ok || !strings.HasPrefix(got, "PDF d1aaaaaa fresh a4") {
		t.Fatalf("the drawing: %q %v", got, ok)
	}
	if _, ok := f.local("_annotated/Clean.pdf"); ok {
		t.Fatal("a document without annotations was drawn")
	}
	if len(f.cloud.Records()) != 4 {
		t.Fatal("an adoption uploaded something")
	}
	// Nothing changed: no drawing again.
	want(t, f.pass(port.MirrorRunOptions{}))
	// Annotated anew on the tablet: drawn again.
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true,
		Extra: []domain.File{f.cloud.Put("d1aaaaaa/page-2.rm", []byte("more ink"))}})
	want(t, f.pass(port.MirrorRunOptions{}), "draw Paper.pdf -> _annotated/Paper.pdf")
	// Opened, which changes its metadata only: nothing to draw.
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true, Pinned: true,
		Extra: []domain.File{f.cloud.Put("d1aaaaaa/page-2.rm", []byte("more ink"))}})
	want(t, f.pass(port.MirrorRunOptions{}))
}

// time10 is a time well before now: a settled file.
var time10 = now.Add(-time10s) //nolint:gochecknoglobals // a fixed test instant

// A PDF changed locally replaces the document's PDF and keeps its
// annotations; changed on the tablet too, it is a conflict, drawn again.
func TestPassUpdatesContent(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.pdfFile("Paper.pdf", "%PDF two", 1, time10.Add(time10s/2))
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "update Paper.pdf", "draw Paper.pdf -> _annotated/Paper.pdf")
	paper, _ := f.doc("Paper")
	pdf, _ := paper.File("pdf")
	if body, _ := f.cloud.Blob(pdf.Hash); string(body) != "%PDF two" {
		t.Fatalf("the PDF is %q", body)
	}
	if !domain.Annotated(paper) {
		t.Fatal("the annotations were lost")
	}
	// Annotated on the tablet and changed locally: a conflict, the local PDF
	// goes up, the annotations stay.
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF two"), RM: true,
		Extra: []domain.File{f.cloud.Put("d1aaaaaa/page-1-metadata.json", []byte("{}"))}})
	f.pdfFile("Paper.pdf", "%PDF three", 2, time10.Add(time10s*3/4))
	p = f.pass(port.MirrorRunOptions{})
	want(t, p, "conflict Paper.pdf", "draw Paper.pdf -> _annotated/Paper.pdf")
	if !strings.Contains(p.Actions[0].Detail, "the PDF has 2 pages, it had 1") {
		t.Fatalf("detail: %q", p.Actions[0].Detail)
	}
}

// An update that would drop annotated pages is refused, and stays pending.
func TestPassRefusesToDropAnnotations(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf",
		Pages: [][]byte{[]byte("one"), []byte("two")}})
	f.pdfFile("Paper.pdf", "%PDF one", 2, time10)
	f.pass(port.MirrorRunOptions{})
	f.pdfFile("Paper.pdf", "%PDF short", 1, time10.Add(time10s/2))
	p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err == nil || p.Pending != 1 || !errors.Is(p.Actions[0].Err, domain.ErrAnnotationsDropped) ||
		!strings.Contains(p.Actions[0].Err.Error(), "--drop-annotations") {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	if j := f.journal(); j["last_error"] == nil || j["pending"] != float64(1) {
		t.Fatalf("journal: %v", j)
	}
}

// An EPUB changed locally replaces its document whole when the document
// carries no annotations.
func TestPassReplacesAnUnannotatedEPUB(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.epubFile("Book.epub", "one")
	f.pass(port.MirrorRunOptions{})
	before, _ := f.doc("Book")
	f.epubFile("Book.epub", "two, longer")
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "replace Book.epub")
	if p.Pending != 0 || !strings.Contains(p.Actions[0].Detail, "replaced whole") {
		t.Fatalf("%+v", p)
	}
	after, _ := f.doc("Book")
	epub, _ := after.File("epub")
	if body, _ := f.cloud.Blob(epub.Hash); string(body) != "PK\x03\x04two, longer" || after.ID != before.ID {
		t.Fatalf("%q %s %s", body, after.ID, before.ID)
	}
	want(t, f.pass(port.MirrorRunOptions{}))
}

// An EPUB changed locally whose document carries annotations is left
// alone, and stays pending: replacing it would lose them.
func TestPassSkipsAnAnnotatedEPUB(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Book", Parent: "f0aaaaaa", Ext: "epub", Body: []byte("PK\x03\x04one"), RM: true})
	f.epubFile("Book.epub", "one")
	want(t, f.pass(port.MirrorRunOptions{}), "adopt Book.epub", "draw Book.epub -> _annotated/Book.pdf")
	f.epubFile("Book.epub", "two, longer")
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "skip Book.epub")
	if p.Pending != 1 || !strings.Contains(p.Actions[0].Detail, "would lose its annotations") {
		t.Fatalf("%+v", p)
	}
	book, _ := f.doc("Book")
	epub, _ := book.File("epub")
	if body, _ := f.cloud.Blob(epub.Hash); string(body) != "PK\x03\x04one" {
		t.Fatalf("the EPUB was replaced: %q", body)
	}
}

// A replacement the cloud refuses fails that step.
func TestPassReplaceFails(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.epubFile("Book.epub", "one")
	f.pass(port.MirrorRunOptions{})
	f.epubFile("Book.epub", "two, longer")
	f.cloud.Fail["CommitRoot"] = errBoom
	if p, err := f.engine.Pass(t.Context(), f.spec, port.MirrorRunOptions{}); !errors.Is(err, errBoom) || p.Pending != 1 {
		t.Fatalf("%v\n%s", err, actions(p))
	}
}

// A file still being written waits for a later pass.
func TestPassWaitsForFilesBeingWritten(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("Fresh.pdf", "%PDF", 1, now)
	want(t, f.pass(port.MirrorRunOptions{}), "skip Fresh.pdf")
	f.pdfFile("Fresh.pdf", "%PDF", 1, time10)
	want(t, f.pass(port.MirrorRunOptions{}), "upload Fresh.pdf")
	f.pdfFile("Fresh.pdf", "%PDF more", 1, now)
	want(t, f.pass(port.MirrorRunOptions{}), "skip Fresh.pdf")
}

// A document renamed or moved on the tablet takes its file and its drawing
// along; a file renamed or moved locally takes its document along.
func TestPassFollowsRenames(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "f1aaaaaa", Name: "Read", Parent: "f0aaaaaa", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Other", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF two")},
	)
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pdfFile("Other.pdf", "%PDF two", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "Paper (read)", Parent: "f1aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true})
	want(t, f.pass(port.MirrorRunOptions{}), "move-local Paper.pdf -> Read/Paper (read).pdf")
	if _, ok := f.local("Read/Paper (read).pdf"); !ok {
		t.Fatal("the file did not move")
	}
	if _, ok := f.local("_annotated/Read/Paper (read).pdf"); !ok {
		t.Fatal("the drawing did not move")
	}
	// Renamed and moved locally: the document follows.
	body, _ := f.local("Other.pdf")
	if err := f.files.Remove(filepath.Join(dir, "Other.pdf")); err != nil {
		t.Fatal(err)
	}
	f.pdfFile("New/Renamed.pdf", body, 1, time10)
	want(t, f.pass(port.MirrorRunOptions{}), "move-cloud Other.pdf -> New/Renamed.pdf")
	if path, _ := f.path("d2aaaaaa"); path != "/Papers/New/Renamed" {
		t.Fatalf("the document is at %s", path)
	}
	want(t, f.pass(port.MirrorRunOptions{}))
}

// A file deleted locally puts its document in the trash; a document
// deleted on the tablet moves its file and drawing into .rmctl-removed.
func TestPassCarriesDeletions(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF a"), RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "B", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF b")},
		porttest.Doc{ID: "d3aaaaaa", Name: "C", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF c")},
		porttest.Doc{ID: "d4aaaaaa", Name: "D", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF d")},
	)
	for _, n := range []string{"A", "B", "C", "D"} {
		f.pdfFile(n+".pdf", "%PDF "+strings.ToLower(n), 1, time10)
	}
	f.pass(port.MirrorRunOptions{})
	f.drop("d1aaaaaa")
	if err := f.files.Remove(filepath.Join(dir, "B.pdf")); err != nil {
		t.Fatal(err)
	}
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "remove A.pdf -> .rmctl-removed/A.pdf", "trash B.pdf")
	if _, ok := f.local(".rmctl-removed/A.pdf"); !ok {
		t.Fatal("A was not moved aside")
	}
	if _, ok := f.local(".rmctl-removed/_annotated/A.pdf"); !ok {
		t.Fatal("A's drawing was not moved aside")
	}
	if _, trashed := f.path("d2aaaaaa"); !trashed {
		t.Fatal("B is not in the trash")
	}
	if len(f.items()) != 2 {
		t.Fatalf("journal: %v", f.items())
	}
	// Trashed on the tablet, the same as deleted; a name taken in the
	// removed directory gets a number.
	f.pdfFile(".rmctl-removed/C.pdf", "%PDF old c", 1, time10)
	f.replace(porttest.Doc{ID: "d3aaaaaa", Name: "C", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF c"), Trashed: true})
	want(t, f.pass(port.MirrorRunOptions{}), "remove C.pdf -> .rmctl-removed/C (2).pdf")
}

// The guard refuses a mass of deletions on one side, and an empty side,
// unless forced; --no-delete detaches instead.
func TestPassGuardsDeletions(t *testing.T) {
	t.Parallel()
	docs := []porttest.Doc{}
	for _, n := range []string{"A", "B", "C"} {
		docs = append(docs, porttest.Doc{ID: domain.ItemID("d" + strings.ToLower(n) + "aaaaaa"), Name: n, Parent: "f0aaaaaa", Ext: "pdf"})
	}
	f := setup(t, true, docs...)
	for _, n := range []string{"A", "B", "C"} {
		f.pdfFile(n+".pdf", "%PDF "+n, 1, time10)
	}
	f.pdfFile("Keep.pdf", "%PDF keep", 1, time10)
	f.pass(port.MirrorRunOptions{})
	for _, n := range []string{"A", "B", "C"} {
		if err := f.files.Remove(filepath.Join(dir, n+".pdf")); err != nil {
			t.Fatal(err)
		}
	}
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "refuse A.pdf", "refuse B.pdf", "refuse C.pdf")
	if p.Pending != 3 || !strings.Contains(p.Actions[0].Detail, "more than half of the 4 documents") {
		t.Fatalf("%+v", p.Actions[0])
	}
	if _, trashed := f.path("daaaaaaa"); trashed {
		t.Fatal("a refused deletion was carried out")
	}
	want(t, f.pass(port.MirrorRunOptions{ForceDelete: true}), "trash A.pdf", "trash B.pdf", "trash C.pdf")

	// An empty directory, as an unmounted drive is.
	if err := f.files.Remove(filepath.Join(dir, "Keep.pdf")); err != nil {
		t.Fatal(err)
	}
	p = f.pass(port.MirrorRunOptions{})
	want(t, p, "refuse Keep.pdf")
	if !strings.Contains(p.Actions[0].Detail, "local directory is empty") {
		t.Fatal(p.Actions[0].Detail)
	}
	// Without deletions: the document stays, and is followed no more.
	f.spec.NoDelete = true
	want(t, f.pass(port.MirrorRunOptions{}), "detach Keep.pdf")
	want(t, f.pass(port.MirrorRunOptions{}))
	if keep, ok := f.doc("Keep"); !ok || keep.TrashedHere() {
		t.Fatal("the detached document went")
	}
}

// The cloud side has its guard too: an emptied folder deletes nothing
// locally.
func TestPassGuardsAnEmptiedFolder(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "A", Parent: "f0aaaaaa", Ext: "pdf"})
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.drop("d1aaaaaa")
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "refuse A.pdf")
	if !strings.Contains(p.Actions[0].Detail, "cloud folder is empty") {
		t.Fatal(p.Actions[0].Detail)
	}
	if _, ok := f.local("A.pdf"); !ok {
		t.Fatal("the file went")
	}
}

// With PullNew the documents created on the tablet are drawn too, and
// their drawings go aside when they go.
func TestPassPullsNew(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "f1aaaaaa", Name: "Meetings", Parent: "f0aaaaaa", Folder: true},
		porttest.Doc{ID: "d1aaaaaa", Name: "Retro", Parent: "f1aaaaaa", Pages: [][]byte{[]byte("ink")}},
		porttest.Doc{ID: "d2aaaaaa", Name: "Kept", Parent: "f0aaaaaa", Pages: [][]byte{[]byte("ink")}},
	)
	want(t, f.pass(port.MirrorRunOptions{}))
	f.spec.PullNew = true
	want(t, f.pass(port.MirrorRunOptions{DryRun: true}), "draw  -> _annotated/Kept.pdf", "draw  -> _annotated/Meetings/Retro.pdf")
	want(t, f.pass(port.MirrorRunOptions{}), "draw  -> _annotated/Kept.pdf", "draw  -> _annotated/Meetings/Retro.pdf")
	want(t, f.pass(port.MirrorRunOptions{}))
	f.drop("d1aaaaaa")
	want(t, f.pass(port.MirrorRunOptions{}), "remove _annotated/Meetings/Retro.pdf")
	if _, ok := f.local(".rmctl-removed/_annotated/Meetings/Retro.pdf"); !ok {
		t.Fatal("the drawing was not moved aside")
	}
}

// A dry run reports and changes nothing, journal included.
func TestPassDryRun(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", RM: true})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pdfFile("New.pdf", "%PDF new", 1, time10)
	p := f.pass(port.MirrorRunOptions{DryRun: true})
	want(t, p, "upload New.pdf", "adopt Paper.pdf", "draw Paper.pdf -> _annotated/Paper.pdf")
	if !p.DryRun || f.journal() != nil || len(f.cloud.Records()) != 2 {
		t.Fatalf("a dry run changed something: %v %d", f.journal(), len(f.cloud.Records()))
	}
	if _, ok := f.local("_annotated/Paper.pdf"); ok {
		t.Fatal("a dry run drew")
	}
}

// rmctl-lite draws nothing: the annotated documents are exported as
// .rmdoc.
func TestPassLiteExports(t *testing.T) {
	t.Parallel()
	f := setup(t, false, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", RM: true})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	p := f.pass(port.MirrorRunOptions{})
	want(t, p, "adopt Paper.pdf", "export Paper.pdf -> _annotated/Paper.rmdoc")
	if !p.Lite {
		t.Fatal("not lite")
	}
	if _, ok := f.local("_annotated/Paper.rmdoc"); !ok {
		t.Fatal("no archive")
	}
}
