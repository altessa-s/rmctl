//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/port"
)

func paragraphs(docs ...port.DocumentText) []string {
	var out []string
	for _, d := range docs {
		for _, p := range d.Pages {
			for _, para := range p.Paragraphs {
				out = append(out, para.Text)
			}
		}
	}
	return out
}

// A notebook with pages the tablet wrote goes out as an .rmdoc through
// rmdoc-go, reads offline, and comes back in under a fresh identifier with
// the same text.
func TestIntegration_ExportInspectImport(t *testing.T) {
	svc, cloud := connected(t)
	const id = "a0000000-0000-4000-8000-0000000000aa"
	cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000aa", Name: "Archive", Kind: clouddouble.Folder},
		clouddouble.Item{ID: id, Name: "Notes", Kind: clouddouble.Notebook, PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")}},
	)
	path := filepath.Join(t.TempDir(), "notes.rmdoc")
	report, err := svc.Fetcher.Export(t.Context(), []string{"/Notes"}, path)
	if err != nil || report.Documents != 1 || report.Path != path {
		t.Fatalf("%+v %v", report, err)
	}

	inspected, err := svc.Archives.Inspect(t.Context(), path)
	if err != nil || len(inspected.Documents) != 1 || inspected.Documents[0].Name != "Notes" || inspected.Documents[0].ID != id {
		t.Fatalf("%+v %v", inspected, err)
	}
	offline, err := svc.Archives.Pages(t.Context(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cloudText, err := svc.Pages.Pages(t.Context(), "/Notes", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := paragraphs(cloudText)
	if len(want) == 0 || !slices.Equal(paragraphs(offline...), want) {
		t.Fatalf("offline %q, cloud %q", paragraphs(offline...), want)
	}

	imported, err := svc.Writer.Import(t.Context(), path, "/Archive")
	if err != nil || len(imported) != 1 {
		t.Fatalf("%+v %v", imported, err)
	}
	copyOf := imported[0]
	if copyOf.From != id || copyOf.Item.ID == id || copyOf.Item.Parent != "f0000000-0000-4000-8000-0000000000aa" {
		t.Fatalf("%+v", copyOf)
	}
	again, err := svc.Pages.Pages(t.Context(), string(copyOf.Item.ID), nil)
	if err != nil || !slices.Equal(paragraphs(again), want) {
		t.Fatalf("imported %q, want %q (%v)", paragraphs(again), want, err)
	}
	if meta := cloud.Metadata(t, string(copyOf.Item.ID)); meta["parent"] != "f0000000-0000-4000-8000-0000000000aa" {
		t.Fatalf("metadata %v", meta)
	}
}
