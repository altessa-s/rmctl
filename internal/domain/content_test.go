// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
)

func TestNewPDFContent(t *testing.T) {
	t.Parallel()
	got := string(domain.NewPDFContent([]string{"p1", "p2"}, 1234, []domain.Tag{{Name: "t", Timestamp: 5}}))
	want := `{"coverPageNumber":-1,"documentMetadata":{},"dummyDocument":false,"extraMetadata":{},"fileType":"pdf",` +
		`"fontName":"","formatVersion":1,"lineHeight":-1,"orientation":"portrait","originalPageCount":2,"pageCount":2,` +
		`"pageTags":[],"pages":["p1","p2"],"redirectionPageMap":[0,1],"sizeInBytes":"1234",` +
		`"tags":[{"name":"t","timestamp":5}],"textAlignment":"justify","textScale":1,"zoomMode":"bestFit"}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestNewEPUBContent(t *testing.T) {
	t.Parallel()
	got := string(domain.NewEPUBContent(9, nil))
	for _, part := range []string{`"fileType":"epub"`, `"pageCount":0`, `"pages":[]`, `"redirectionPageMap":[]`, `"originalPageCount":-1`, `"tags":[]`, `"sizeInBytes":"9"`} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %s in %s", part, got)
		}
	}
	if strings.Contains(got, "cPages") {
		t.Fatal("cPages must not be written")
	}
}

func TestContentTags(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		`{"tags":[{"name":"a"},{"name":""},{"name":"a"},{"name":"b"}],"other":1}`: {"a", "b"},
		`{"tags":[]}`:  nil,
		`{}`:           nil,
		`not json`:     nil,
		`{"tags":"x"}`: nil,
	}
	for in, want := range tests {
		if got := domain.ContentTags([]byte(in)); !slices.Equal(got, want) {
			t.Errorf("%s: %v", in, got)
		}
	}
}

func TestPagedata(t *testing.T) {
	t.Parallel()
	if got := string(domain.Pagedata(2)); got != "Blank\nBlank\n" {
		t.Fatalf("%q", got)
	}
	if len(domain.Pagedata(0)) != 0 || string(domain.FolderContent()) != "{}" {
		t.Fatal("empty")
	}
}

func TestItemKinds(t *testing.T) {
	t.Parallel()
	pdf := domain.File{Name: "d.pdf", Size: 3}
	rm := domain.File{Name: "d/p.rm", Size: 4}
	epub := domain.File{Name: "d.epub", Size: 5}
	meta := domain.File{Name: "d.metadata", Size: -100}
	tests := []struct {
		name   string
		folder bool
		files  []domain.File
		kind   domain.Kind
		ext    string
		size   int64
	}{
		{name: "folder", folder: true, kind: domain.KindFolder},
		{name: "pdf", files: []domain.File{pdf}, kind: domain.KindPDF, ext: "pdf", size: 3},
		{name: "annotated", files: []domain.File{pdf, rm}, kind: domain.KindAnnotatedPDF, ext: "pdf", size: 7},
		{name: "epub", files: []domain.File{epub}, kind: domain.KindEPUB, ext: "epub", size: 5},
		{name: "notebook", files: []domain.File{rm}, kind: domain.KindNotebook, size: 4},
		{name: "size clamped", files: []domain.File{meta}, kind: domain.KindNotebook, size: 0},
	}
	for _, tt := range tests {
		it := item(t, "d", "D", tt.folder, "", tt.files...)
		if it.Kind != tt.kind || it.Kind.Extension() != tt.ext || it.Size != tt.size || it.Kind.IsFolder() != tt.folder {
			t.Errorf("%s: %+v", tt.name, it)
		}
		orig, ok := it.Original()
		if (tt.ext != "") != ok || (ok && orig.Extension() != tt.ext) {
			t.Errorf("%s: original %+v %v", tt.name, orig, ok)
		}
	}
}

func TestErrorsRead(t *testing.T) {
	t.Parallel()
	if got := domain.MalformedFile("content", "bad").Error(); got != "malformed content: bad" {
		t.Fatal(got)
	}
	amb := &domain.AmbiguousError{Query: "d", Candidates: []domain.Item{{ID: "d1"}, {ID: "d2"}}}
	if got := amb.Error(); got != `"d" is ambiguous: d1, d2` {
		t.Fatal(got)
	}
}

func TestReplacedContent(t *testing.T) {
	t.Parallel()
	fresh := domain.NewPDFContent([]string{"n1", "n2"}, 10, []domain.Tag{{Name: "new", Timestamp: 1}})
	old := []byte(`{"cPages":{"pages":[]},"formatVersion":2,"fileType":"pdf","pageCount":9,"pages":["o1"],` +
		`"tags":[{"name":"old","timestamp":0}],"margins":180,"zoomMode":"fitToWidth","unknownField":{"keep":true}}`)
	var got map[string]any
	if err := json.Unmarshal(domain.ReplacedContent(old, fresh), &got); err != nil {
		t.Fatal(err)
	}
	// The new original's layout and tags; never the old pages.
	if got["formatVersion"] != float64(1) || got["pageCount"] != float64(2) || got["cPages"] != nil ||
		fmt.Sprint(got["pages"]) != "[n1 n2]" || fmt.Sprint(got["tags"]) != "[map[name:new timestamp:1]]" {
		t.Fatalf("layout: %v", got)
	}
	// The reader's settings of the same kind of file, and every unknown field.
	if got["margins"] != float64(180) || got["zoomMode"] != "fitToWidth" || fmt.Sprint(got["unknownField"]) != "map[keep:true]" {
		t.Fatalf("kept: %v", got)
	}
	// Another kind of file keeps the unknown fields and drops the settings.
	epub := domain.NewEPUBContent(5, nil)
	var other map[string]any
	if err := json.Unmarshal(domain.ReplacedContent(old, epub), &other); err != nil {
		t.Fatal(err)
	}
	if other["margins"] != nil || other["zoomMode"] != "bestFit" || other["unknownField"] == nil || other["fileType"] != "epub" {
		t.Fatalf("other kind: %v", other)
	}
	// What does not decode leaves the fresh content as it is.
	for _, bad := range [][]byte{nil, []byte("not json"), []byte("[]"), []byte("null")} {
		if !bytes.Equal(domain.ReplacedContent(bad, fresh), fresh) {
			t.Errorf("%q changed the fresh content", bad)
		}
	}
}

func TestContentWithTags(t *testing.T) {
	t.Parallel()
	old := []byte(`{"fileType":"pdf","tags":[{"name":"a","timestamp":1}],"zoomMode":"fitToWidth","future":{"x":[1,2]}}`)
	out, err := domain.ContentWithTags(old, []domain.Tag{{Name: "b", Timestamp: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"fileType":"pdf","future":{"x":[1,2]},"tags":[{"name":"b","timestamp":2}],"zoomMode":"fitToWidth"}` {
		t.Fatalf("%s", out)
	}
	if out, _ := domain.ContentWithTags([]byte(`{}`), nil); string(out) != `{"tags":[]}` {
		t.Fatalf("%s", out)
	}
	if _, err := domain.ContentWithTags([]byte(`[]`), nil); err == nil {
		t.Fatal("a list was given tags")
	}
	tags := []domain.Tag{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	if got := domain.TagNames(domain.WithoutTags(tags, []string{"b", "x"})); fmt.Sprint(got) != "[a c]" {
		t.Fatal(got)
	}
}
