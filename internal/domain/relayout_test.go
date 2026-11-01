// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

func newIDs(prefix string) func(domain.PageSlot) string {
	n := 0
	return func(domain.PageSlot) string {
		n++
		return fmt.Sprintf("%s%d", prefix, n)
	}
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const v1Content = `{"coverPageNumber":0,"fileType":"pdf","formatVersion":1,"margins":125,"zoomMode":"fitToWidth",` +
	`"tags":[{"name":"keep","timestamp":7}],"futureField":{"x":1},` +
	`"pages":["a","b","ins","c"],"redirectionPageMap":[0,1,-1,2],"pageCount":4,"originalPageCount":3,"sizeInBytes":"100"}`

const v2Content = `{"coverPageNumber":-1,"fileType":"pdf","formatVersion":2,"pageCount":4,"originalPageCount":3,"sizeInBytes":"100",` +
	`"cPages":{"lastOpened":{"timestamp":"1:9","value":"c"},"original":{"timestamp":"1:3","value":3},"uuids":[{"first":"u","second":1}],` +
	`"pages":[{"id":"c","idx":{"timestamp":"1:2","value":"bd"},"redir":{"timestamp":"1:2","value":2}},` +
	`{"id":"a","idx":{"timestamp":"1:2","value":"bb"},"redir":{"timestamp":"1:2","value":0}},` +
	`{"id":"gone","idx":{"timestamp":"1:2","value":"ba"},"deleted":{"timestamp":"1:5","value":1}},` +
	`{"id":"ins","idx":{"timestamp":"1:4","value":"bcm"}},` +
	`{"id":"b","idx":{"timestamp":"1:2","value":"bc"},"redir":{"timestamp":"1:2","value":1}}]}}`

func TestContentPages(t *testing.T) {
	t.Parallel()
	want := "[{a 0} {b 1} {ins -1} {c 2}]"
	for name, content := range map[string]string{"pages": v1Content, "cPages": v2Content} {
		pages, err := domain.ContentPages([]byte(content))
		if err != nil || fmt.Sprint(pages) != want {
			t.Errorf("%s: %v %v", name, pages, err)
		}
		if n := domain.ContentOriginalPages([]byte(content)); n != 3 {
			t.Errorf("%s: %d original pages", name, n)
		}
	}
	// Without a redirection map each page shows the original page of its
	// index; a short map leaves the rest with none.
	for content, want := range map[string]string{
		`{"pages":["a","","b"]}`:                       "[{a 0} {b 2}]",
		`{"pages":["a","b"],"redirectionPageMap":[3]}`: "[{a 3} {b -1}]",
		`{"fileType":"epub"}`:                          "[]",
	} {
		if pages, err := domain.ContentPages([]byte(content)); err != nil || fmt.Sprint(pages) != want {
			t.Errorf("%s: %v %v", content, pages, err)
		}
	}
	if _, err := domain.ContentPages([]byte("not json")); err == nil {
		t.Fatal("garbage decoded")
	}
	if n := domain.ContentOriginalPages([]byte(`{"pages":["a","b"],"redirectionPageMap":[4,-1]}`)); n != 5 {
		t.Fatalf("counted %d", n)
	}
}

func TestPlanRelayout(t *testing.T) {
	t.Parallel()
	pages := []domain.PageSlot{{ID: "a", Original: 0}, {ID: "b", Original: 1}, {ID: "ins", Original: -1}, {ID: "c", Original: 2}}
	tests := []struct {
		count                   int
		kept, appended, dropped string
	}{
		{3, "[{n1 0 a} {n2 1 b} {n3 -1 ins} {n4 2 c}]", "[]", "[]"},
		{5, "[{n1 0 a} {n2 1 b} {n3 -1 ins} {n4 2 c}]", "[{n5 3} {n6 4}]", "[]"},
		{2, "[{n1 0 a} {n2 1 b} {n3 -1 ins}]", "[]", "[{c 2}]"},
	}
	for _, tc := range tests {
		r := domain.PlanRelayout(pages, tc.count, newIDs("n"))
		if fmt.Sprint(r.Kept) != tc.kept || fmt.Sprint(nonNil(r.Appended)) != tc.appended || fmt.Sprint(nonNil(r.Dropped)) != tc.dropped {
			t.Errorf("%d pages: %+v", tc.count, r)
		}
		if len(r.Pages()) != len(r.Kept)+len(r.Appended) || r.Count != tc.count {
			t.Errorf("%d pages: %v", tc.count, r.Pages())
		}
	}
	// An original page no page shows yet gets a page too.
	r := domain.PlanRelayout([]domain.PageSlot{{ID: "a", Original: 1}}, 3, newIDs("n"))
	if fmt.Sprint(r.Appended) != "[{n2 0} {n3 2}]" || fmt.Sprint(r.Renamed()) != "map[a:n1]" {
		t.Fatalf("%v", r.Appended)
	}
}

func nonNil(p []domain.PageSlot) []domain.PageSlot {
	if p == nil {
		return []domain.PageSlot{}
	}
	return p
}

func TestRelaidContentPages(t *testing.T) {
	t.Parallel()
	pages, _ := domain.ContentPages([]byte(v1Content))
	more := domain.PlanRelayout(pages, 5, newIDs("n"))
	out, err := domain.RelaidContent([]byte(v1Content), more, 4096, "")
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if fmt.Sprint(m["pages"]) != "[n1 n2 n3 n4 n5 n6]" || fmt.Sprint(m["redirectionPageMap"]) != "[0 1 -1 2 3 4]" ||
		m["pageCount"] != float64(6) || m["originalPageCount"] != float64(5) || m["sizeInBytes"] != "4096" {
		t.Fatalf("the layout: %v", m)
	}
	// Everything the new PDF says nothing of stays.
	if m["coverPageNumber"] != float64(0) || m["margins"] != float64(125) || m["zoomMode"] != "fitToWidth" ||
		fmt.Sprint(m["tags"]) != "[map[name:keep timestamp:7]]" || fmt.Sprint(m["futureField"]) != "map[x:1]" || m["formatVersion"] != float64(1) {
		t.Fatalf("the settings: %v", m)
	}
	fewer := domain.PlanRelayout(pages, 2, newIDs("n"))
	out, _ = domain.RelaidContent([]byte(v1Content), fewer, 1, "")
	if m := decode(t, out); fmt.Sprint(m["pages"]) != "[n1 n2 n3]" || fmt.Sprint(m["redirectionPageMap"]) != "[0 1 -1]" || m["pageCount"] != float64(3) {
		t.Fatalf("fewer: %v", m)
	}
	if _, err := domain.RelaidContent([]byte("[]"), more, 1, ""); err == nil {
		t.Fatal("a list was rewritten")
	}
}

func TestRelaidContentCPages(t *testing.T) {
	t.Parallel()
	pages, _ := domain.ContentPages([]byte(v2Content))
	more := domain.PlanRelayout(pages, 5, newIDs("n"))
	out, err := domain.RelaidContent([]byte(v2Content), more, 9, "")
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if _, ok := m["pages"]; ok {
		t.Fatal("a pages list was added beside cPages")
	}
	got, err := domain.ContentPages(out)
	if err != nil || fmt.Sprint(got) != "[{n1 0} {n2 1} {n3 -1} {n4 2} {n5 3} {n6 4}]" {
		t.Fatalf("%v %v", got, err)
	}
	cp := m["cPages"].(map[string]any)
	if fmt.Sprint(cp["original"]) != "map[timestamp:1:3 value:5]" || fmt.Sprint(cp["lastOpened"]) != "map[timestamp:1:9 value:n4]" ||
		fmt.Sprint(cp["uuids"]) != "[map[first:u second:1]]" || m["pageCount"] != float64(6) || m["originalPageCount"] != float64(5) {
		t.Fatalf("%v", m)
	}
	// The deleted entry stays as it was; the new entries come after every key.
	if !strings.Contains(string(out), `"id":"gone"`) || !strings.Contains(string(out), `"value":"bdbb"`) {
		t.Fatalf("%s", out)
	}
	// Fewer pages: the dropped page goes, and a last-opened page among them
	// becomes the first page; --current-page names its own.
	fewer := domain.PlanRelayout(pages, 2, newIDs("n"))
	out, _ = domain.RelaidContent([]byte(v2Content), fewer, 9, "")
	if got, _ := domain.ContentPages(out); fmt.Sprint(got) != "[{n1 0} {n2 1} {n3 -1}]" ||
		fmt.Sprint(decode(t, out)["cPages"].(map[string]any)["lastOpened"]) != "map[timestamp:1:9 value:n1]" {
		t.Fatalf("%s", out)
	}
	out, _ = domain.RelaidContent([]byte(v2Content), fewer, 9, "n2")
	if fmt.Sprint(decode(t, out)["cPages"].(map[string]any)["lastOpened"]) != "map[timestamp:1:9 value:n2]" {
		t.Fatalf("%s", out)
	}
	for _, bad := range []string{`{"cPages":[]}`, `{"cPages":{"pages":{}}}`} {
		if _, err := domain.RelaidContent([]byte(bad), more, 1, ""); err == nil {
			t.Errorf("%s was rewritten", bad)
		}
	}
	// A cPages with no lastOpened gets one only when a page is named.
	bare := `{"cPages":{"pages":[]}}`
	out, _ = domain.RelaidContent([]byte(bare), domain.PlanRelayout(nil, 1, newIDs("n")), 1, "n1")
	if fmt.Sprint(decode(t, out)["cPages"].(map[string]any)["lastOpened"]) != "map[timestamp:1:1 value:n1]" {
		t.Fatalf("%s", out)
	}
}

func TestRelaidPagedata(t *testing.T) {
	t.Parallel()
	before := []domain.PageSlot{{ID: "a", Original: 0}, {ID: "b", Original: 1}, {ID: "c", Original: 2}}
	r := domain.PlanRelayout(before, 2, newIDs("n"))
	if got := string(domain.RelaidPagedata([]byte("Blank\nLS Grid\nP Lines\n"), before, r)); got != "Blank\nLS Grid\n" {
		t.Fatalf("%q", got)
	}
	r = domain.PlanRelayout(before, 4, newIDs("n"))
	if got := string(domain.RelaidPagedata([]byte("Blank\n\nP Lines\n"), before, r)); got != "Blank\nBlank\nP Lines\nBlank\n" {
		t.Fatalf("%q", got)
	}
}

func TestPageFilesAndThumbnails(t *testing.T) {
	t.Parallel()
	files := []domain.File{
		{Name: "d/p1.rm"}, {Name: "d/p1-metadata.json"}, {Name: "d.highlights/p1.json"}, {Name: "d/p2.rm"},
		{Name: "d.thumbnails/p1.png"}, {Name: "d.pdf"}, {Name: "e/p1.rm"},
	}
	all, notes := domain.PageFiles("d", "p1", files)
	if len(all) != 3 || len(notes) != 2 {
		t.Fatalf("%v %v", all, notes)
	}
	if !domain.IsThumbnail("d", files[4]) || domain.IsThumbnail("d", files[5]) || domain.IsThumbnail("e", files[4]) {
		t.Fatal("thumbnails")
	}
}

func TestOpenedAt(t *testing.T) {
	t.Parallel()
	m := domain.NewDocumentMetadata("A", "", time.Unix(0, 0)).OpenedAt(4)
	if decode(t, m.Bytes())["lastOpenedPage"] != float64(4) {
		t.Fatalf("%s", m.Bytes())
	}
}

func TestRelayoutErrorsAreMalformed(t *testing.T) {
	t.Parallel()
	_, err := domain.ContentPages([]byte("{"))
	if _, ok := errors.AsType[*domain.MalformedFileError](err); !ok {
		t.Fatal(err)
	}
}

// Every reference to a kept page follows it to its new identifier: the page
// tags, a field rmctl has no name for, an object keyed by a page; the tags
// of a dropped page go; nothing else changes.
func TestRelaidContentRenamesEveryReference(t *testing.T) {
	t.Parallel()
	const a, b, c = "aaaaaaaa-0000-4000-8000-000000000001", "bbbbbbbb-0000-4000-8000-000000000002", "cccccccc-0000-4000-8000-000000000003"
	content := `{"pages":["` + a + `","` + b + `","` + c + `"],"redirectionPageMap":[0,1,2],"fileType":"pdf",` +
		`"pageTags":[{"name":"star","pageId":"` + a + `","timestamp":1},{"name":"gone","pageId":"` + c + `","timestamp":2}],` +
		`"futureNotes":{"` + b + `":{"pinnedTo":"` + a + `"}},"title":"` + a + ` is not a title"}`
	pages, err := domain.ContentPages([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	r := domain.PlanRelayout(pages, 2, newIDs("new-"))
	out, err := domain.RelaidContent([]byte(content), r, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{`"` + a + `"`, `"` + b + `"`, `"` + c + `"`} {
		if strings.Contains(string(out), old) {
			t.Fatalf("%s is still referenced: %s", old, out)
		}
	}
	m := decode(t, out)
	if fmt.Sprint(m["pages"]) != "[new-1 new-2]" || fmt.Sprint(m["pageTags"]) != "[map[name:star pageId:new-1 timestamp:1]]" ||
		fmt.Sprint(m["futureNotes"]) != "map[new-2:map[pinnedTo:new-1]]" || m["title"] != a+" is not a title" {
		t.Fatalf("%v", m)
	}
	if fmt.Sprint(r.Renamed()) != "map["+a+":new-1 "+b+":new-2]" {
		t.Fatalf("%v", r.Renamed())
	}
}

func TestRenamedPageFile(t *testing.T) {
	t.Parallel()
	renamed := map[string]string{"old": "new"}
	for name, want := range map[string]string{
		"d/old.rm":                  "d/new.rm",
		"d/old-metadata.json":       "d/new-metadata.json",
		"d.highlights/old.json":     "d.highlights/new.json",
		"d.textconversion/old.json": "d.textconversion/new.json",
	} {
		f, ok := domain.RenamedPageFile("d", domain.File{Name: name, Hash: "h", Size: 3}, renamed)
		if !ok || f.Name != want || f.Hash != "h" || f.Size != 3 {
			t.Errorf("%s: %+v %v", name, f, ok)
		}
	}
	for _, name := range []string{"d/other.rm", "e/old.rm", "d.pdf", "d/old.rm.bak"} {
		if f, ok := domain.RenamedPageFile("d", domain.File{Name: name}, renamed); ok || f.Name != name {
			t.Errorf("%s was renamed to %s", name, f.Name)
		}
	}
}
