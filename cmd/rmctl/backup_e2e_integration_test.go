//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// backupReport is backup's --json answer.
type backupReport struct {
	Documents, Written, Skipped, Moved, Pruned int
}

func (e *e2e) backup(args ...string) (backupReport, map[string]int) {
	e.t.Helper()
	out, hits := e.cost(append([]string{"backup"}, append(args, "--json")...)...)
	var r backupReport
	if err := json.Unmarshal([]byte(out), &r, json.MatchCaseInsensitiveNames(true)); err != nil {
		e.t.Fatalf("%v: %q", err, out)
	}
	return r, hits
}

// A backup written, kept up to date and put back: unchanged documents cost
// no download, a renamed one is written again, one gone is set aside, and a
// restore brings the archives back as copies below a new folder, ink and
// all, in one change.
func TestE2E_BackupAndRestore(t *testing.T) {
	e := newE2E(t)
	e.register()
	const (
		folderID = "f0000000-0000-4000-8000-000000000401"
		paperID  = "a0000000-0000-4000-8000-000000000401"
		notesID  = "a0000000-0000-4000-8000-000000000402"
		oldID    = "a0000000-0000-4000-8000-000000000403"
	)
	e.cloud.Seed(t,
		clouddouble.Item{ID: folderID, Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: paperID, Name: "Paper", Parent: folderID, Kind: clouddouble.PDF, Pages: 1},
		clouddouble.Item{ID: notesID, Name: "Notes", Kind: clouddouble.Notebook, PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")}},
		clouddouble.Item{ID: oldID, Name: "Old", Kind: clouddouble.PDF, Trashed: true},
	)
	notesText := e.ok("text", "/Notes")
	dir := filepath.Join(t.TempDir(), "bk")

	first, firstHits := e.backup(dir)
	if first != (backupReport{Documents: 2, Written: 2}) {
		t.Fatalf("%+v", first)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Format    int
		Profile   string
		Path      string
		Documents []struct{ ID, Path, Kind, File string }
	}
	if err = json.Unmarshal(raw, &m, json.MatchCaseInsensitiveNames(true)); err != nil {
		t.Fatal(err)
	}
	if m.Format != 1 || m.Profile != "default" || m.Path != "/" || len(m.Documents) != 2 ||
		m.Documents[0].Path != "/Books/Paper" || m.Documents[1].ID != notesID || m.Documents[1].File != notesID+".rmdoc" {
		t.Fatalf("%+v", m)
	}
	if listing := e.ok("rmdoc", filepath.Join(dir, paperID+".rmdoc"), "--raw"); !strings.Contains(listing, "Paper\tannotated-pdf\t") {
		t.Fatalf("%s", listing)
	}

	again, hits := e.backup(dir, "--jobs", "1")
	if again != (backupReport{Documents: 2, Skipped: 2}) || total(hits) >= total(firstHits) {
		t.Fatalf("%+v %v (first %v)", again, hits, firstHits)
	}

	// The directory is bound to the path of its first backup.
	if code, _, errOut := e.rmctl("backup", dir, "/Books", "--prune"); code != 1 || !strings.Contains(errOut, "--rescope") {
		t.Fatalf("a backup of another path: exit %d %q", code, errOut)
	}
	if _, err = os.Stat(filepath.Join(dir, notesID+".rmdoc")); err != nil {
		t.Fatal("a refused backup touched the directory")
	}

	e.ok("rename", "/Notes", "Notebook", "--yes")
	if r, _ := e.backup(dir); r != (backupReport{Documents: 2, Written: 1, Skipped: 1}) {
		t.Fatalf("after a rename: %+v", r)
	}
	e.ok("rm", "/Books/Paper", "--yes")
	if r, _ := e.backup(dir); r != (backupReport{Documents: 1, Skipped: 1, Moved: 1}) {
		t.Fatalf("after a removal: %+v", r)
	}
	if _, err = os.Stat(filepath.Join(dir, "removed", paperID+".rmdoc")); err != nil {
		t.Fatal(err)
	}

	before := len(e.cloud.Records(t))
	if plan := e.ok("backup", "restore", dir, "--to", "/Restored", "--dry-run"); !strings.Contains(plan, "would create\tRestored\t") ||
		!strings.Contains(plan, "would restore\tNotebook\t") || len(e.cloud.Records(t)) != before {
		t.Fatalf("%q", plan)
	}
	if code, _, _ := e.rmctl("backup", "restore", dir); code != 2 {
		t.Fatalf("a restore without --yes off a terminal: exit %d", code)
	}
	out := e.ok("backup", "restore", dir, "--to", "/Restored", "--yes")
	if !strings.Contains(out, "created\tRestored\t") || !strings.Contains(out, "restored\tNotebook\t") || !strings.Contains(out, "\t"+notesID+"\t") {
		t.Fatalf("%q", out)
	}
	if after := len(e.cloud.Records(t)); after != before+2 {
		t.Fatalf("records %d → %d", before, after)
	}
	if copied := e.ok("text", "/Restored/Notebook"); copied != notesText {
		t.Fatalf("the restored copy's text differs:\n%q\n%q", copied, notesText)
	}
	if e.cloud.Metadata(t, notesID)["parent"] != "" {
		t.Fatal("the original moved")
	}
}
