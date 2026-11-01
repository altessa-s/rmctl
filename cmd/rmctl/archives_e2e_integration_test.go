//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// The whole round of an archive through the command line: a notebook the
// tablet wrote goes out as .rmdoc, is read offline, and comes back in under
// a fresh identifier, its text unchanged.
func TestE2E_ArchiveRoundTrip(t *testing.T) {
	e := newE2E(t)
	e.register()
	const id = "a0000000-0000-4000-8000-0000000000e2"
	e.cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000e2", Name: "Restored", Kind: clouddouble.Folder},
		clouddouble.Item{ID: id, Name: "Notes", Kind: clouddouble.Notebook, PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")}},
	)
	cloudText := e.ok("text", "/Notes")

	archive := filepath.Join(t.TempDir(), "notes.rmdoc")
	if out := e.ok("export", "/Notes", "-o", archive); !strings.Contains(out, "1 documents") {
		t.Fatalf("export: %q", out)
	}

	listing := e.ok("rmdoc", archive)
	if !strings.Contains(listing, id) || strings.Contains(listing, "Notes") || !strings.Contains(listing, ".rm") {
		t.Fatalf("rmdoc (names masked):\n%s", listing)
	}
	if raw := e.ok("rmdoc", archive, "--raw"); !strings.Contains(raw, "Notes [notebook]") {
		t.Fatalf("rmdoc --raw:\n%s", raw)
	}
	if offline := e.ok("text", archive); offline != cloudText {
		t.Fatalf("offline text differs:\n%q\n%q", offline, cloudText)
	}

	before := len(e.cloud.Records(t))
	// The plan names the new identifier the taken one would get, and
	// changes nothing.
	planned := len(e.cloud.Records(t))
	if plan := e.ok("put", archive, "/Restored", "--dry-run"); !strings.Contains(plan, "taken") || len(e.cloud.Records(t)) != planned {
		t.Fatalf("dry run: %q, %d → %d records", plan, planned, len(e.cloud.Records(t)))
	}
	out := e.ok("put", archive, "/Restored", "--yes")
	if !strings.Contains(out, "new id") || !strings.Contains(out, "was "+id) {
		t.Fatalf("import of a held identifier: %q", out)
	}
	if after := len(e.cloud.Records(t)); after != before+1 {
		t.Fatalf("records %d → %d", before, after)
	}
	var listed []map[string]any
	if err := json.Unmarshal([]byte(e.ok("ls", "/Restored", "--json")), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("ls /Restored: %v %v", listed, err)
	}
	copyID := listed[0]["id"].(string)
	if copyID == id {
		t.Fatal("the copy kept the original's identifier")
	}
	if again := e.ok("text", copyID); again != cloudText {
		t.Fatalf("the imported copy's text differs:\n%q\n%q", again, cloudText)
	}
	if e.cloud.Metadata(t, id)["parent"] != "" {
		t.Fatal("the original moved")
	}
}
