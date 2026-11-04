//go:build integration && cgo && (darwin || linux)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// A notebook the tablet wrote, drawn through the whole tool: from the cloud
// as a PDF and a page as a PNG, then from its exported archive offline.
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
	e.ok("render", "/Notes", "--page", "2", "--png", "--scale", "0.5", "-o", page)
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if config, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || config.Width != 702 {
		t.Fatalf("%+v %v", config, err)
	}
	if code, _, stderr := e.rmctl("render", "/Notes", "--page", "3", "--png", "-o", page); code != 1 || !strings.Contains(stderr, "no such page") {
		t.Fatalf("page 3 of 2: %d %q", code, stderr)
	}

	archive := filepath.Join(dir, "notes.rmdoc")
	e.ok("export", "/Notes", "-o", archive)
	offline := filepath.Join(dir, "offline.pdf")
	if out := e.ok("render", archive, "--offline", "-o", offline, "--json"); !strings.Contains(out, `"pages": 2`) {
		t.Fatalf("render of the archive: %q", out)
	}
}
