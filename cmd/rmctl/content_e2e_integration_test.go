//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

const annotatedID = "a0000000-0000-4000-8000-000000000101"

// files are a document's files in the cloud, by name.
func (e *e2e) files(id string) map[string]string {
	e.t.Helper()
	out := map[string]string{}
	for _, f := range e.cloud.Files(e.t, id) {
		out[f.DocumentID] = f.Hash
	}
	return out
}

func (e *e2e) jsonBlob(hash string) map[string]any {
	e.t.Helper()
	data, ok := e.cloud.Blob(hash)
	if !ok {
		e.t.Fatalf("no blob %s", hash)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// contentOnly runs put --content-only --yes --json with args and returns
// the id_map of its plan.
func (e *e2e) contentOnly(file string, args ...string) map[string]string {
	e.t.Helper()
	out := e.ok(append([]string{"put", file, "--content-only", "--yes", "--json"}, args...)...)
	var results []struct {
		Plan struct {
			IDMap map[string]string `json:"id_map"`
		} `json:"content_plan"`
	}
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) != 1 {
		e.t.Fatalf("%v %s", err, out)
	}
	return results[0].Plan.IDMap
}

// put --content-only swaps the PDF and keeps every page's ink: the .rm
// blobs are the very same, and only the PDF, the .content, the .metadata
// and the .pagedata change.
func TestE2E_PutContentOnly(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t, clouddouble.Item{ID: annotatedID, Name: "Paper", Kind: clouddouble.PDF, Pages: 3, Tags: []string{"keep"}, Pinned: true})
	before := e.files(annotatedID)
	id := annotatedID

	// More pages, opening on the fourth.
	four := localPDF(t, "Paper.pdf", 4)
	if code, _, errOut := e.rmctl("put", four, "--content-only", "--current-page", "9", "--yes"); code != 1 || !strings.Contains(errOut, "no such page") {
		t.Fatalf("a page beyond: exit %d %q", code, errOut)
	}
	idMap := e.contentOnly(four, "--current-page", "4")
	after := e.files(id)
	old := []string{"page-0", "page-1", "page-2"}
	if len(idMap) != 3 {
		t.Fatalf("id_map %v", idMap)
	}
	// Every page has a new identifier; nothing is filed under an old one;
	// the ink is the very same blobs under the new names.
	for _, p := range old {
		for name := range after {
			if strings.Contains(name, p) {
				t.Fatalf("%s is filed under the old page %s", name, p)
			}
		}
		if got := after[id+"/"+idMap[p]+".rm"]; got == "" || got != before[id+"/"+p+".rm"] {
			t.Fatalf("%s → %s: %q, was %q", p, idMap[p], got, before[id+"/"+p+".rm"])
		}
	}
	if pdf, _ := e.cloud.Blob(after[id+".pdf"]); string(pdf) != string(clouddouble.MakePDF(4)) {
		t.Fatal("the PDF is not the new one")
	}
	content := e.jsonBlob(after[id+".content"])
	pages, _ := content["pages"].([]any)
	if len(pages) != 4 || pages[0] != idMap["page-0"] || pages[1] != idMap["page-1"] || pages[2] != idMap["page-2"] ||
		slices.Contains(old, pages[3].(string)) || content["originalPageCount"] != float64(4) ||
		fmt.Sprint(content["tags"]) != "[map[name:keep timestamp:1.7356896e+12]]" {
		t.Fatalf("%v", content)
	}
	for _, p := range old {
		if strings.Contains(fmt.Sprint(content), p) {
			t.Fatalf("the .content still names %s: %v", p, content)
		}
	}
	meta := e.jsonBlob(after[id+".metadata"])
	if meta["lastOpenedPage"] != float64(3) || meta["pinned"] != true || meta["visibleName"] != "Paper" {
		t.Fatalf("%v", meta)
	}

	// Fewer pages: page 3 carries ink, so it is refused, names it, and changes
	// nothing; a dry run says the same; --drop-annotations drops it.
	two := localPDF(t, "Paper.pdf", 2)
	code, _, errOut := e.rmctl("put", two, "--content-only", "--yes")
	if code != 1 || !strings.Contains(errOut, "page 3 ("+idMap["page-2"]+", 1 annotation files)") || !strings.Contains(errOut, "--drop-annotations") {
		t.Fatalf("exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("put", two, "--content-only", "--dry-run"); code != 1 {
		t.Fatalf("a dry run of a refused change: exit %d", code)
	}
	if !maps.Equal(e.files(id), after) {
		t.Fatal("a refused change changed the document")
	}
	plan := e.ok("put", two, "--content-only", "--drop-annotations", "--dry-run", "--json")
	if !strings.Contains(plan, `"dropped": 2`) || !strings.Contains(plan, `"annotated_dropped": [`) || !maps.Equal(e.files(id), after) {
		t.Fatalf("%s", plan)
	}
	second := e.contentOnly(two, "--drop-annotations")
	final := e.files(id)
	if len(final) != 6 || final[id+"/"+second[idMap["page-0"]]+".rm"] != before[id+"/page-0.rm"] ||
		final[id+"/"+second[idMap["page-1"]]+".rm"] != before[id+"/page-1.rm"] {
		t.Fatalf("%v %v", second, final)
	}

	// No document of the name, an EPUB, flags that exclude each other.
	if code, _, errOut := e.rmctl("put", localPDF(t, "Other.pdf", 1), "--content-only", "--yes"); code != 1 || !strings.Contains(errOut, "without --content-only") {
		t.Fatalf("no target: exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("put", two, "--content-only", "--replace", "--yes"); code != 2 {
		t.Fatalf("with --replace: exit %d", code)
	}
}
