//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

const (
	editDocID    = "a0000000-0000-4000-8000-000000000201"
	editFolderID = "f0000000-0000-4000-8000-000000000201"
)

// changedFiles are the names whose hash differs between two file lists, or
// that only one has.
func changedFiles(before, after map[string]string) []string {
	var out []string
	for name, h := range after {
		if before[name] != h {
			out = append(out, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// tag, pin and rename touch the .metadata, and tag the .content's tags,
// nothing else; asking for what already is commits nothing.
func TestE2E_MetadataEdits(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: editFolderID, Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: editDocID, Name: "Paper", Parent: editFolderID, Kind: clouddouble.PDF, Pages: 2, Tags: []string{"old"}},
	)
	before := e.files(editDocID)

	out := e.ok("tag", "add", "/Books/Paper", "--tag", "work", "--tag", "old", "--yes")
	if !strings.HasPrefix(out, "tagged\tPaper\tannotated-pdf\told,work\t"+editDocID) {
		t.Fatalf("%q", out)
	}
	after := e.files(editDocID)
	if got := changedFiles(before, after); !slices.Equal(got, []string{editDocID + ".content", editDocID + ".metadata"}) {
		t.Fatalf("changed %v", got)
	}
	content := e.jsonBlob(after[editDocID+".content"])
	if tags, _ := content["tags"].([]any); len(tags) != 2 || content["fileType"] != "pdf" || content["pageCount"] != float64(2) {
		t.Fatalf("%v", content)
	}

	e.ok("pin", "/Books/Paper", "/Books", "--yes")
	e.ok("rename", "/Books/Paper", "Paper 2026", "--yes")
	final := e.files(editDocID)
	if got := changedFiles(after, final); !slices.Equal(got, []string{editDocID + ".metadata"}) {
		t.Fatalf("changed %v", got)
	}
	meta := e.cloud.Metadata(t, editDocID)
	if meta["visibleName"] != "Paper 2026" || meta["pinned"] != true || meta["parent"] != editFolderID {
		t.Fatalf("%v", meta)
	}
	if e.cloud.Metadata(t, editFolderID)["pinned"] != true {
		t.Fatal("the folder is not pinned")
	}

	// What already is costs no commit.
	gen := func() int64 { _, g := e.cloud.Root(); return g }
	was := gen()
	if out := e.ok("pin", "/Books/Paper 2026", "--yes"); !strings.HasPrefix(out, "unchanged\t") || gen() != was {
		t.Fatalf("%q: generation %d → %d", out, was, gen())
	}
	if out := e.ok("tag", "rm", "/Books/Paper 2026", "--tag", "absent", "--yes"); !strings.HasPrefix(out, "unchanged\t") || gen() != was {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := e.rmctl("tag", "add", "/Books", "--tag", "x", "--yes"); code != 1 || !strings.Contains(errOut, "a folder takes no tags") {
		t.Fatalf("a folder: exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("pin", "/Books", "--yes", "--offline"); code != 2 {
		t.Fatalf("offline: exit %d", code)
	}
}
