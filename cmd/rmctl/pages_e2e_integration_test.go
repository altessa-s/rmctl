//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// End-to-end tests of the page commands and the richer put: real .rm pages
// written on a tablet, read and changed through the whole tool against the
// emulated cloud.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// tabletPage is a page the tablet wrote, from the corpus the wiring tests use.
func tabletPage(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "wiring", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestE2E_PagesRead(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: "n0000000-0000-4000-8000-000000000001", Name: "Notes", Kind: clouddouble.Notebook,
			PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")}},
		clouddouble.Item{ID: "p0000000-0000-4000-8000-000000000001", Name: "Article", Kind: clouddouble.PDF,
			PageBodies: [][]byte{tabletPage(t, "Wikipedia_highlighted_p1.rm")}},
	)

	md := e.ok("text", "/Notes", "--format", "markdown")
	if !strings.Contains(md, "# ") || !strings.Contains(md, "- ") {
		t.Fatalf("text as markdown lacks a heading or a bullet:\n%s", md)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(e.ok("text", "/Notes", "--json")), &doc); err != nil {
		t.Fatal(err)
	}

	hl := e.ok("highlights", "/Article")
	if !strings.Contains(hl, "p.1") {
		t.Fatalf("highlights:\n%s", hl)
	}
	if note := e.ok("highlights", "/Article", "--format", "note"); !strings.Contains(note, "remarkable-path: \"/Article\"\n") ||
		!strings.Contains(note, "\n## Highlights\n\n### Page 1\n") {
		t.Fatalf("highlights --format note:\n%s", note)
	}
	// The whole library as notes: the notebook has no highlights; the
	// next run finds the article's note unchanged.
	vault := filepath.Join(t.TempDir(), "vault")
	if out := e.ok("highlights", "/", "-o", vault, "--incremental"); !strings.Contains(out, "written\t"+filepath.Join(vault, "Article.md")) ||
		!strings.Contains(out, "skipped\t"+filepath.Join(vault, "Notes.md")) {
		t.Fatalf("highlights /:\n%s", out)
	}
	if out := e.ok("highlights", "/", "-o", vault, "--incremental"); !strings.Contains(out, "unchanged\t"+filepath.Join(vault, "Article.md")) {
		t.Fatalf("highlights / again:\n%s", out)
	}
	if page := e.ok("debug", "page", "/Article"); !strings.Contains(page, "highlight") {
		t.Fatalf("debug page:\n%s", page)
	}
	if found := e.ok("find", "--text", strings.Fields(firstParagraph(t, e))[0]); !strings.Contains(found, "Notes") {
		t.Fatalf("find --text:\n%s", found)
	}
}

// firstParagraph is the first non-empty paragraph of /Notes, so the search
// below looks for a word the page really holds.
func firstParagraph(t *testing.T, e *e2e) string {
	t.Helper()
	for line := range strings.SplitSeq(e.ok("text", "/Notes"), "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "Page") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatal("the notebook has no text")
	return ""
}

func TestE2E_Unmark(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t, clouddouble.Item{ID: "p0000000-0000-4000-8000-000000000002", Name: "Article", Kind: clouddouble.PDF,
		PageBodies: [][]byte{tabletPage(t, "Wikipedia_highlighted_p1.rm")}})

	if code, _, _ := e.rmctl("unmark", "/Article"); code != 2 {
		t.Fatalf("unmark without --yes: exit %d", code)
	}
	e.ok("unmark", "/Article", "--yes")
	if hl := e.ok("highlights", "/Article", "--json"); strings.Contains(hl, `"text"`) {
		t.Fatalf("highlights left after unmark:\n%s", hl)
	}
}

func TestE2E_PutNotebookReplaceDryRun(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.ok("mkdir", "/Inbox", "--yes")
	dir := t.TempDir()

	note := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(note, []byte("# Plan\n\n- first\n- second\n\nBody text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("put", note, "/Inbox", "--notebook", "--device", "move", "--yes")
	if text := e.ok("text", "/Inbox/plan"); !strings.Contains(text, "Plan") || !strings.Contains(text, "second") {
		t.Fatalf("notebook text:\n%s", text)
	}
	if meta := e.names()["plan"]; meta == nil || meta["type"] != "DocumentType" {
		t.Fatalf("notebook metadata %v", meta)
	}

	paper := localPDF(t, "paper.pdf", 2)
	before := len(e.cloud.Records(t))
	if out := e.ok("put", paper, "/Inbox", "--dry-run"); out == "" || len(e.cloud.Records(t)) != before {
		t.Fatalf("a dry run uploaded: %q", out)
	}
	e.ok("put", paper, "/Inbox", "--cover", "first", "--yes")
	id := ""
	for _, rec := range e.cloud.Records(t) {
		if e.cloud.Metadata(t, rec.DocumentID)["visibleName"] == "paper" {
			id = rec.DocumentID
		}
	}
	content, ok := e.cloud.File(t, id, id+".content")
	if !ok || !strings.Contains(string(content), `"coverPageNumber":0`) {
		t.Fatalf("--cover first: %s", content)
	}

	_, _, errOut := e.rmctl("put", localPDF(t, "paper.pdf", 3), "/Inbox", "--replace", "--yes")
	if !strings.Contains(errOut, "annotations are dropped") {
		t.Fatalf("no warning on replace: %q", errOut)
	}
	if after := len(e.cloud.Records(t)); after != before+1 {
		t.Fatalf("replace added a document: %d → %d", before, after)
	}

	// Several files: one fails, the others land, and the command says so.
	good := localPDF(t, "good.pdf", 1)
	empty := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := e.rmctl("put", good, empty, "--to", "/Inbox", "--yes"); code != 1 {
		t.Fatalf("a failed file in a batch: exit %d", code)
	}
	if e.names()["good"] == nil {
		t.Fatal("the good file of the batch was not uploaded")
	}
}
