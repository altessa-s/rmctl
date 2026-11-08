//go:build integration && norender && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// rmctl-lite, the build without the renderer, through the whole tool: it
// says so in version --full, reads an archive's text as rmctl does, and
// render and open fail as in a build that draws nothing, naming the full
// build.
func TestE2E_Lite(t *testing.T) {
	e := newE2E(t)
	if out := e.ok("version", "--full"); !strings.Contains(out, "render\tno\n") {
		t.Fatalf("version --full: %q", out)
	}
	if out := e.ok("version", "--full", "--json"); !strings.Contains(out, `"render": false`) {
		t.Fatalf("version --full --json: %q", out)
	}
	archive := filepath.Join("..", "..", "internal", "adapter", "renderer", "testdata", "Notes.rmdoc")
	if out := e.ok("text", archive); strings.TrimSpace(out) == "" {
		t.Fatal("text of the archive is empty")
	}
	code, out, errOut := e.rmctl("render", archive, "-o", filepath.Join(t.TempDir(), "notes.pdf"))
	if code != 1 || out != "" || !strings.Contains(errOut, "rendering is not in rmctl-lite: install rmctl, the full build, for it") {
		t.Fatalf("render: exit %d %q %q", code, out, errOut)
	}
	code, out, errOut = e.rmctl("open", archive)
	if code != 1 || out != "" || !strings.Contains(errOut, "rendering is not in rmctl-lite: install rmctl, the full build, for it") ||
		len(e.opener.shown()) != 0 {
		t.Fatalf("open: exit %d %q %q", code, out, errOut)
	}
}
