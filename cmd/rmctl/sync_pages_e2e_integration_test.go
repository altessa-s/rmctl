//go:build integration && cgo && (darwin || linux || windows)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// After sync --files, every read of a cloud document runs under --offline
// without a request: the page commands, export, backup and render. Before
// it, they fail naming what the cache lacks.
func TestE2E_SyncServesEveryOfflineRead(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: "s-folder", Name: "Work", Kind: clouddouble.Folder},
		clouddouble.Item{
			ID: "s-notes", Name: "Notes", Parent: "s-folder", Kind: clouddouble.Notebook,
			PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")},
		},
		clouddouble.Item{ID: "s-paper", Name: "Paper", Parent: "s-folder", Kind: clouddouble.PDF, Body: clouddouble.MakePDF(2)},
	)
	dir := t.TempDir()

	// Cold: only the tree is cached.
	e.ok("ls")
	for _, args := range [][]string{
		{"text", "/Work/Notes"},
		{"export", "/Work/Notes", "-o", filepath.Join(dir, "cold.rmdoc")},
		{"render", "/Work/Notes", "-o", filepath.Join(dir, "cold.pdf")},
	} {
		code, _, errOut := e.rmctl(append(args, "--offline")...)
		if code != 1 || !strings.Contains(errOut, "is not in the cache") || !strings.Contains(errOut, "s-notes") {
			t.Fatalf("cold %v: exit %d %q", args, code, errOut)
		}
	}

	online := e.ok("text", "/Work/Notes")
	e.ok("debug", "cache", "clear", "--yes")
	if a, _ := e.sync("/Work", "--files"); a.Documents != 2 || a.Fetched == 0 {
		t.Fatalf("sync: %+v", a)
	}

	for _, args := range [][]string{
		{"text", "/Work/Notes"},
		{"highlights", "/Work/Notes"},
		{"find", "--text", "Heading"},
		{"debug", "page", "/Work/Notes"},
		{"get", "/Work/Paper", "-o", filepath.Join(dir, "paper.pdf")},
		{"export", "/Work", "-o", filepath.Join(dir, "work.rmdoc")},
		{"mget", "/Work", "-o", filepath.Join(dir, "mirror")},
		{"backup", filepath.Join(dir, "backup")},
		{"render", "/Work/Notes", "-o", filepath.Join(dir, "notes.pdf")},
	} {
		out, hits := e.cost(append(args, "--offline")...)
		if len(hits) != 0 {
			t.Fatalf("%v asked the cloud: %v", args, hits)
		}
		if args[0] == "text" && out != online {
			t.Fatalf("offline text differs:\n%s\n%s", out, online)
		}
	}
	for _, name := range []string{"paper.pdf", "work.rmdoc", "notes.pdf"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.HasSuffix(name, ".pdf") && !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatalf("%s is not a PDF", name)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "backup")); err != nil || len(entries) < 3 {
		t.Fatalf("backup: %v %v", entries, err)
	}
}
