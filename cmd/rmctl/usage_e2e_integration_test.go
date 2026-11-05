//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// du and dupes read the indexes only: from the cloud once, then from the
// cache with --offline and no request at all.
func TestE2E_DuAndDupes(t *testing.T) {
	e := newE2E(t)
	e.register()
	same := clouddouble.MakePDF(3)
	e.cloud.Seed(t,
		clouddouble.Item{ID: editFolderID, Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: editDocID, Name: "Paper", Parent: editFolderID, Kind: clouddouble.PDF, Body: same},
		clouddouble.Item{ID: annotatedID, Name: "Paper again", Kind: clouddouble.PDF, Body: same, Pages: 1},
		clouddouble.Item{ID: "a0000000-0000-4000-8000-000000000301", Name: "Notes", Kind: clouddouble.Notebook, Pages: 2},
	)
	var du struct {
		Bytes      int64 `json:"bytes"`
		Documents  int   `json:"documents"`
		Folders    int   `json:"folders"`
		Subfolders []struct {
			Path      string `json:"path"`
			Documents int    `json:"documents"`
		} `json:"subfolders"`
		ByKind []struct {
			Kind string `json:"kind"`
		} `json:"by_kind"`
	}
	if err := json.Unmarshal([]byte(e.ok("du", "--json")), &du); err != nil {
		t.Fatal(err)
	}
	if du.Documents != 3 || du.Folders != 1 || du.Bytes <= int64(2*len(same)) || len(du.Subfolders) != 1 || du.Subfolders[0].Path != "/Books" ||
		du.Subfolders[0].Documents != 1 || len(du.ByKind) != 3 {
		t.Fatalf("%+v", du)
	}
	out, hits := e.cost("dupes", "--offline")
	if len(hits) != 0 || !strings.Contains(out, "\t/Books/Paper\t") || !strings.Contains(out, "\t/Paper again\t") ||
		strings.Contains(out, "Notes") || !strings.Contains(out, "groups\t1\n") {
		t.Fatalf("%v %q", hits, out)
	}
	if out, hits := e.cost("du", "/Books", "--offline", "--depth", "0"); len(hits) != 0 || !strings.HasPrefix(out, "path\t/Books\n") {
		t.Fatalf("%v %q", hits, out)
	}
	if code, _, _ := e.rmctl("du", "/Books/Paper"); code != 1 {
		t.Fatalf("du of a document: exit %d", code)
	}
}
