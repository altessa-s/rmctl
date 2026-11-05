//go:build integration && cgo && (darwin || linux || windows) && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/xml"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// A notebook the tablet wrote, drawn through the whole tool: from the cloud
// as a PDF and a page as a PNG, a JPEG and an SVG, then from its exported
// archive offline.
func TestE2E_Render(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t, clouddouble.Item{
		ID: "r0000000-0000-4000-8000-0000000000e2", Name: "Notes", Kind: clouddouble.Notebook,
		PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm"), tabletPage(t, "Wikipedia_highlighted_p1.rm")},
	})
	dir := t.TempDir()

	pdf := filepath.Join(dir, "notes.pdf")
	if out := e.ok("render", "/Notes", "-o", pdf, "--paper", "letter"); !strings.Contains(out, "pages\t2\n") {
		t.Fatalf("render: %q", out)
	}
	if data, err := os.ReadFile(pdf); err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("%s: %v", pdf, err)
	}

	page := filepath.Join(dir, "page.png")
	e.ok("render", "/Notes", "--page", "2", "--scale", "0.5", "-o", page)
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if config, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || config.Width != 702 {
		t.Fatalf("%+v %v", config, err)
	}
	if code, _, stderr := e.rmctl("render", "/Notes", "--page", "3", "-o", page); code != 1 || !strings.Contains(stderr, "no such page") {
		t.Fatalf("page 3 of 2: %d %q", code, stderr)
	}

	photo := filepath.Join(dir, "page.jpeg")
	e.ok("render", "/Notes", "--page", "1", "--scale", "0.5", "--quality", "60", "--no-template", "-o", photo)
	if data, err = os.ReadFile(photo); err != nil {
		t.Fatal(err)
	}
	if config, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil || config.Width != 702 {
		t.Fatalf("%+v %v", config, err)
	}
	vector := filepath.Join(dir, "page.out")
	e.ok("render", "/Notes", "--page", "2", "--format", "svg", "-o", vector)
	if data, err = os.ReadFile(vector); err != nil {
		t.Fatal(err)
	}
	var svg struct {
		XMLName xml.Name
	}
	if err = xml.Unmarshal(data, &svg); err != nil || svg.XMLName.Local != "svg" {
		t.Fatalf("%s: %v", svg.XMLName.Local, err)
	}

	archive := filepath.Join(dir, "notes.rmdoc")
	e.ok("export", "/Notes", "-o", archive)
	offline := filepath.Join(dir, "offline.pdf")
	if out := e.ok("render", archive, "--offline", "-o", offline, "--json"); !strings.Contains(out, `"pages": 2`) {
		t.Fatalf("render of the archive: %q", out)
	}
}

// A folder drawn through the whole tool: its tree mirrored as PDFs that
// take the documents' modification times, then drawn again incrementally,
// and the whole library with the trash.
func TestE2E_RenderFolder(t *testing.T) {
	e := newE2E(t)
	e.register()
	modified := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	page := tabletPage(t, "Bold_Heading_Bullet_Normal.rm")
	e.cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000f1", Name: "Clients", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000f2", Name: "Harbor", Kind: clouddouble.Folder, Parent: "f0000000-0000-4000-8000-0000000000f1"},
		clouddouble.Item{
			ID: "r0000000-0000-4000-8000-0000000000a1", Name: "Kickoff", Kind: clouddouble.Notebook,
			Parent: "f0000000-0000-4000-8000-0000000000f1", PageBodies: [][]byte{page}, Modified: modified,
		},
		clouddouble.Item{
			ID: "r0000000-0000-4000-8000-0000000000a2", Name: "Contract/Draft", Kind: clouddouble.Notebook,
			Parent: "f0000000-0000-4000-8000-0000000000f2", PageBodies: [][]byte{page, page}, Modified: modified,
		},
		clouddouble.Item{ID: "r0000000-0000-4000-8000-0000000000a3", Name: "Old", Kind: clouddouble.Notebook, PageBodies: [][]byte{page}, Trashed: true},
	)
	dir := filepath.Join(t.TempDir(), "out")
	out := e.ok("render", "/Clients", "-o", dir, "--jobs", "2", "--json")
	if !strings.Contains(out, `"rendered": 2`) || !strings.Contains(out, `"pages": 2`) {
		t.Fatalf("render: %q", out)
	}
	for _, name := range []string{"Kickoff.pdf", "Harbor/Contract-Draft.pdf"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatalf("%s: %v", path, err)
		}
		if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(modified) {
			t.Fatalf("%s: %v %v", path, info.ModTime(), err)
		}
	}
	if out = e.ok("render", "/Clients", "-o", dir, "--incremental", "--json"); !strings.Contains(out, `"unchanged": 2`) {
		t.Fatalf("incremental: %q", out)
	}
	// The whole library, the trash too, into its own directory.
	all := t.TempDir()
	t.Chdir(all)
	if out = e.ok("render", "/", "--all", "--json"); !strings.Contains(out, `"rendered": 3`) {
		t.Fatalf("render /: %q", out)
	}
	if _, err := os.Stat(filepath.Join(all, "remarkable", "Old.pdf")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := e.rmctl("render", "/Clients", "--page", "1"); code != 2 || !strings.Contains(stderr, "leave --page out") {
		t.Fatalf("--page with a folder: %d %q", code, stderr)
	}
}
