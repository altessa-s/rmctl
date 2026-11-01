// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package archives_test

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/archives"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func page(text string) []byte {
	return porttest.PageBytes([]domain.Paragraph{{Text: text, Style: domain.StylePlain}},
		[]domain.Highlight{{ID: "1:1", Text: "marked " + text, HasText: true}}, 0)
}

// fixture packs two notebooks, a folder and a document whose metadata does
// not decode into one archive.
func fixture(t *testing.T) (*archives.Service, *porttest.MemFiles, *porttest.Archiver, *porttest.Pages) {
	t.Helper()
	files := porttest.NewMemFiles()
	arc := porttest.NewArchiver(files)
	data, err := arc.Pack([]domain.ArchiveFile{
		{Name: "n1.metadata", Data: []byte(`{"visibleName":"Notes","type":"DocumentType"}`)},
		{Name: "n1.content", Data: []byte(`{"formatVersion":1,"pages":["b","a"],"tags":[{"name":"x","timestamp":1}]}`)},
		{Name: "n1/a.rm", Data: page("first")},
		{Name: "n1/b.rm", Data: page("second")},
		{Name: "n2.metadata", Data: []byte(`{"visibleName":"Other","type":"DocumentType"}`)},
		{Name: "n2/p.rm", Data: page("only")},
		{Name: "f1.metadata", Data: []byte(`{"visibleName":"Folder","type":"CollectionType"}`)},
		{Name: "bad.metadata", Data: []byte(`{`)},
		{Name: "bad.pdf", Data: []byte("%PDF")},
	})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("a.rmdoc", data)
	pr := porttest.NewPages()
	return archives.New(arc, pr), files, arc, pr
}

func TestInspect(t *testing.T) {
	t.Parallel()
	s, _, _, _ := fixture(t)
	r, err := s.Inspect(t.Context(), "a.rmdoc")
	if err != nil || r.Path != "a.rmdoc" || len(r.Documents) != 4 {
		t.Fatalf("%v %+v", err, r)
	}
	d := r.Documents
	if d[0].ID != "n1" || d[0].Name != "Notes" || d[0].Kind != domain.KindNotebook || len(d[0].Entries) != 4 ||
		d[2].Kind != domain.KindFolder || d[3].ID != "bad" || d[3].Name != "" || len(d[3].Entries) != 2 {
		t.Fatalf("%+v", d)
	}
}

func TestPages(t *testing.T) {
	t.Parallel()
	s, _, _, _ := fixture(t)
	docs, err := s.Pages(t.Context(), "a.rmdoc", nil)
	if err != nil || len(docs) != 2 {
		t.Fatalf("%v %+v", err, docs)
	}
	n1 := docs[0]
	if n1.Item.Name != "Notes" || len(n1.Item.Tags) != 1 || len(n1.Pages) != 2 ||
		n1.Pages[0].ID != "b" || n1.Pages[0].Paragraphs[0].Text != "second" || n1.Pages[1].Number != 2 {
		t.Fatalf("%+v", n1)
	}
	if docs[1].Pages[0].Highlights[0].Text != "marked only" {
		t.Fatalf("%+v", docs[1])
	}
	one, err := s.Pages(t.Context(), "a.rmdoc", []string{"1"})
	if err != nil || len(one[0].Pages) != 1 || one[0].Pages[0].ID != "b" {
		t.Fatalf("%v %+v", err, one)
	}
	if _, err := s.Pages(t.Context(), "a.rmdoc", []string{"9"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a page past the end: %v", err)
	}
}

func TestRefusals(t *testing.T) {
	t.Parallel()
	s, files, arc, pr := fixture(t)
	if _, err := s.Inspect(t.Context(), "missing.rmdoc"); err == nil {
		t.Fatal("a missing archive opened")
	}
	if _, err := s.Pages(t.Context(), "missing.rmdoc", nil); err == nil {
		t.Fatal("a missing archive opened")
	}
	data, err := arc.Pack([]domain.ArchiveFile{{Name: "f1.metadata", Data: []byte(`{"visibleName":"F","type":"CollectionType"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("folder.rmdoc", data)
	if _, err := s.Pages(t.Context(), "folder.rmdoc", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an archive with no document: %v", err)
	}
	pr.Fail["ReadPage"] = errBoom
	if _, err := s.Pages(t.Context(), "a.rmdoc", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	arc.Fail["Read"] = errBoom
	if _, err := s.Inspect(t.Context(), "a.rmdoc"); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Pages(t.Context(), "a.rmdoc", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

// reading records the names of the entries an archive hands over.
type reading struct {
	port.ArchiveReader
	mu   sync.Mutex
	read []string
}

func (r *reading) OpenArchive(path string) (port.Archive, error) {
	a, err := r.ArchiveReader.OpenArchive(path)
	if err != nil {
		return nil, err
	}
	return readingArchive{a, r}, nil
}

type readingArchive struct {
	port.Archive
	r *reading
}

func (a readingArchive) Read(keep func(domain.ArchiveEntry) bool, body func(domain.ArchiveEntry, []byte) error) error {
	return a.Archive.Read(keep, func(e domain.ArchiveEntry, data []byte) error {
		a.r.mu.Lock()
		a.r.read = append(a.r.read, e.Name)
		a.r.mu.Unlock()
		return body(e, data)
	})
}

// Offline, an original, thumbnails and other entries are never read: only
// what the answer is made of.
func TestReadsOnlyWhatItNeeds(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	arc := porttest.NewArchiver(files)
	data, err := arc.Pack([]domain.ArchiveFile{
		{Name: "n1.metadata", Data: []byte(`{"visibleName":"Notes","type":"DocumentType"}`)},
		{Name: "n1.content", Data: []byte(`{"formatVersion":1,"pages":["a"]}`)},
		{Name: "n1.pdf", Data: []byte("%PDF large original")},
		{Name: "n1.pagedata", Data: []byte("Blank")},
		{Name: "n1/a.rm", Data: page("first")},
		{Name: "n1/a-metadata.json", Data: []byte("{}")},
		{Name: "n1.thumbnails/a.png", Data: []byte("png")},
	})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("r.rmdoc", data)
	r := &reading{ArchiveReader: arc}
	s := archives.New(r, porttest.NewPages())
	if _, err := s.Pages(t.Context(), "r.rmdoc", nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"n1.metadata", "n1.content", "n1/a.rm"}; !slices.Equal(r.read, want) {
		t.Fatalf("pages read %v", r.read)
	}
	r.read = nil
	if _, err := s.Inspect(t.Context(), "r.rmdoc"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"n1.metadata", "n1.content"}; !slices.Equal(r.read, want) {
		t.Fatalf("inspect read %v", r.read)
	}
}

// A page the .content names but the archive lacks is a page with nothing
// on it, as it is for a cloud document whose index lacks the file.
func TestMissingPageFile(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	arc := porttest.NewArchiver(files)
	data, err := arc.Pack([]domain.ArchiveFile{
		{Name: "n1.metadata", Data: []byte(`{"visibleName":"Notes","type":"DocumentType"}`)},
		{Name: "n1.content", Data: []byte(`{"formatVersion":1,"pages":["a"]}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("m.rmdoc", data)
	docs, err := archives.New(arc, porttest.NewPages()).Pages(t.Context(), "m.rmdoc", nil)
	if err != nil || len(docs[0].Pages) != 1 || len(docs[0].Pages[0].Paragraphs) != 0 {
		t.Fatalf("%v %+v", err, docs)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	for i, f := range []func(){
		func() { archives.New(nil, porttest.NewPages()) },
		func() { archives.New(porttest.NewArchiver(porttest.NewMemFiles()), nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d did not panic", i)
				}
			}()
			f()
		}()
	}
}
