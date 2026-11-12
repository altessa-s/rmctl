//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// End-to-end tests: the whole tool, from the command line through the use
// cases, the adapters and the real rmcloud-go transport, against an emulated
// cloud on loopback TLS. Nothing calls remarkable.com.

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands"
	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// e2e is one emulated cloud and one isolated profile. keychain, when set,
// stands in for the login Keychain; opener stands in for the system's
// viewer, so no test starts one; streams, when set, says what the terminal
// is.
type e2e struct {
	t        *testing.T
	cloud    *clouddouble.Cloud
	keychain wiring.KeychainItems
	opener   *viewer
	streams  func(*commands.Streams)
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	isolate(t)
	return &e2e{t: t, cloud: clouddouble.New(t), opener: &viewer{}}
}

// viewer records the files open showed.
type viewer struct {
	mu     sync.Mutex
	opened []string
}

func (v *viewer) Open(_ context.Context, path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.opened = append(v.opened, path)
	return nil
}

func (v *viewer) shown() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.opened)
}

// rmctl runs the tool and returns its exit code, standard output and
// standard error.
func (e *e2e) rmctl(args ...string) (int, string, string) {
	e.t.Helper()
	var out, errOut bytes.Buffer
	code := runWith(args, nil, &out, &errOut, func(c int) { e.t.Fatalf("exit(%d) from %v", c, args) }, seams{keychain: e.keychain, opener: e.opener, streams: e.streams}, e.cloud.Options()...)
	return code, out.String(), errOut.String()
}

// ok runs the tool and fails the test unless it succeeds.
func (e *e2e) ok(args ...string) string {
	e.t.Helper()
	code, out, errOut := e.rmctl(args...)
	if code != 0 {
		e.t.Fatalf("rmctl %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

func (e *e2e) register() {
	e.t.Helper()
	e.ok("register", clouddouble.ValidCode)
}

// names are the visible names of the root records, read from the cloud's
// own copy of each .metadata.
func (e *e2e) names() map[string]map[string]any {
	e.t.Helper()
	out := map[string]map[string]any{}
	for _, rec := range e.cloud.Records(e.t) {
		meta := e.cloud.Metadata(e.t, rec.DocumentID)
		out[meta["visibleName"].(string)] = meta
	}
	return out
}

func localPDF(t *testing.T, name string, pages int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, clouddouble.MakePDF(pages), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestE2E_RegisterAndAccount(t *testing.T) {
	e := newE2E(t)

	if code, _, errOut := e.rmctl("ls"); code != 3 || !strings.Contains(errOut, "not registered") {
		t.Fatalf("before register: exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("register", "wrongcod"); code == 0 {
		t.Fatal("a refused code registered")
	}
	e.register()
	if code, _, errOut := e.rmctl("register", clouddouble.ValidCode); code == 0 || !strings.Contains(errOut, "already") {
		t.Fatalf("a second register: exit %d %q", code, errOut)
	}
	e.ok("register", clouddouble.ValidCode, "--replace")

	var info map[string]any
	if err := json.Unmarshal([]byte(e.ok("account", "--json")), &info); err != nil || info["registered"] != true {
		t.Fatalf("account: %v %v", info, err)
	}
	tokenDir := os.Getenv("RMCTL_CONFIG_DIR")
	if err := filepath.WalkDir(tokenDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		fi, err := d.Info()
		if err == nil && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v", path, fi.Mode().Perm())
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	e.ok("logout", "--revoke", "--yes")
	if e.cloud.Revoked() != 1 {
		t.Fatalf("revoked %d times", e.cloud.Revoked())
	}
	if code, _, _ := e.rmctl("ls"); code != 3 {
		t.Fatalf("after logout: exit %d", code)
	}
}

func TestE2E_Reading(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-000000000001", Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-000000000001", Name: "Go", Parent: "f0000000-0000-4000-8000-000000000001",
			Kind: clouddouble.PDF, Body: clouddouble.MakePDF(2), Tags: []string{"reading"}, Pinned: true},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-000000000002", Name: "Rust", Parent: "f0000000-0000-4000-8000-000000000001",
			Kind: clouddouble.EPUB},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-000000000003", Name: "Sketches", Kind: clouddouble.Notebook, Pages: 2},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-000000000004", Name: "Old", Kind: clouddouble.PDF, Trashed: true},
	)

	tree := e.ok("tree")
	for _, want := range []string{"Books\tfolder\t", "  Go\tpdf\t", "  Rust\tepub\t", "Sketches\tnotebook\t"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree lacks %q:\n%s", want, tree)
		}
	}
	if strings.Contains(tree, "Old") {
		t.Fatalf("the trash shows without --all:\n%s", tree)
	}
	if !strings.Contains(e.ok("ls", "--all"), "Old") {
		t.Fatal("--all hides the trash")
	}
	if ls := e.ok("ls", "/Books"); !strings.Contains(ls, "Go") || !strings.Contains(ls, "Rust") || strings.Contains(ls, "Sketches") {
		t.Fatalf("ls /Books:\n%s", ls)
	}
	if found := e.ok("find", "--tag", "reading"); !strings.Contains(found, "Go") || strings.Contains(found, "Rust") {
		t.Fatalf("find --tag:\n%s", found)
	}
	if found := e.ok("find", "--starred"); !strings.Contains(found, "Go") {
		t.Fatalf("find --starred:\n%s", found)
	}
	var stat map[string]any
	if err := json.Unmarshal([]byte(e.ok("stat", "d0000000-0000-4000-8000-000000000001", "--json")), &stat); err != nil {
		t.Fatal(err)
	}
	if stat["name"] != "Go" || stat["kind"] != "pdf" {
		t.Fatalf("stat: %v", stat)
	}
	if code, _, errOut := e.rmctl("stat", "d0000000"); code != 1 || !strings.Contains(errOut, "ambiguous") {
		t.Fatalf("an ambiguous prefix: exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("stat", "/Nope"); code != 1 {
		t.Fatalf("a missing path: exit %d", code)
	}

	dir := t.TempDir()
	got := filepath.Join(dir, "go.pdf")
	e.ok("get", "/Books/Go", "-o", got)
	if data, err := os.ReadFile(got); err != nil || !bytes.Equal(data, clouddouble.MakePDF(2)) {
		t.Fatalf("get wrote %d bytes, %v", len(data), err)
	}
	if code, _, _ := e.rmctl("get", "/Sketches", "-o", filepath.Join(dir, "x")); code != 1 {
		t.Fatalf("a notebook has no original: exit %d", code)
	}

	archive := filepath.Join(dir, "sketches.rmdoc")
	e.ok("export", "/Sketches", "-o", archive)
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var entries []string
	for _, f := range zr.File {
		entries = append(entries, f.Name)
	}
	if !slices.ContainsFunc(entries, func(n string) bool { return strings.HasSuffix(n, ".rm") }) ||
		!slices.ContainsFunc(entries, func(n string) bool { return strings.HasSuffix(n, ".metadata") }) ||
		slices.ContainsFunc(entries, func(n string) bool { return strings.HasSuffix(n, ".docSchema") }) {
		t.Fatalf("rmdoc entries %v", entries)
	}

	mirror := filepath.Join(dir, "mirror")
	e.ok("mget", "/Books", "-o", mirror)
	files, err := filepath.Glob(filepath.Join(mirror, "*"))
	if err != nil || len(files) != 2 {
		t.Fatalf("mget wrote %v, %v", files, err)
	}
	// A second incremental run finds everything in place and writes nothing.
	var report map[string]any
	if err := json.Unmarshal([]byte(e.ok("mget", "/Books", "-o", mirror, "--incremental", "--json")), &report); err != nil {
		t.Fatal(err)
	}
	if report["written"] != float64(0) || report["skipped"] != float64(2) {
		t.Fatalf("incremental mget: %v", report)
	}
}

func TestE2E_Writing(t *testing.T) {
	e := newE2E(t)
	e.register()

	// A writing command off a terminal needs --yes, and changes nothing without it.
	if code, _, _ := e.rmctl("mkdir", "/Inbox"); code != 2 {
		t.Fatalf("mkdir without --yes: exit %d", code)
	}
	if len(e.cloud.Records(t)) != 0 {
		t.Fatal("a refused write changed the library")
	}

	e.ok("mkdir", "-p", "/Archive/2026", "--yes")
	e.ok("mkdir", "/Inbox", "--yes")
	if code, _, _ := e.rmctl("mkdir", "/Inbox", "--yes"); code == 0 {
		t.Fatal("mkdir of an existing folder succeeded")
	}
	e.ok("put", localPDF(t, "paper.pdf", 3), "/Inbox", "--tag", "work", "--yes")

	names := e.names()
	paper, ok := names["paper"]
	if !ok || names["Inbox"] == nil || names["2026"] == nil {
		t.Fatalf("library after writes: %v", names)
	}
	if paper["type"] != "DocumentType" || paper["parent"] == "" {
		t.Fatalf("paper metadata %v", paper)
	}

	e.ok("mv", "/Inbox/paper", "/Archive/2026", "--yes")
	e.ok("mv", "/Archive/2026/paper", "/Archive/2026/Final paper", "--yes")
	if ls := e.ok("ls", "/Archive/2026"); !strings.Contains(ls, "Final paper") {
		t.Fatalf("after mv:\n%s", ls)
	}
	if code, _, errOut := e.rmctl("mv", "/Archive", "/Archive/2026", "--yes"); code == 0 || !strings.Contains(errOut, "itself") {
		t.Fatalf("a folder moved into itself: exit %d %q", code, errOut)
	}

	e.ok("trash", "/Archive/2026/Final paper", "--yes")
	if strings.Contains(e.ok("tree"), "Final paper") || !strings.Contains(e.ok("tree", "--all"), "Final paper") {
		t.Fatal("trash")
	}
	if e.names()["Final paper"]["parent"] != "trash" {
		t.Fatal("trash did not set the parent to trash")
	}
	e.ok("restore", "Final paper", "--to", "/Inbox", "--yes")
	if ls := e.ok("ls", "/Inbox"); !strings.Contains(ls, "Final paper") {
		t.Fatalf("after restore:\n%s", ls)
	}

	before := len(e.cloud.Records(t))
	e.ok("rm", "/Archive", "--yes")
	if after := len(e.cloud.Records(t)); after != before-2 {
		t.Fatalf("rm of a folder with one subfolder: %d → %d records", before, after)
	}

	upload := t.TempDir()
	if err := os.MkdirAll(filepath.Join(upload, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.pdf", "sub/b.pdf"} {
		if err := os.WriteFile(filepath.Join(upload, p), clouddouble.MakePDF(1), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(upload, "notes.txt"), []byte("skip me"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("mput", upload, "/Inbox", "--yes")
	if tree := e.ok("tree", "/Inbox"); !strings.Contains(tree, "a\tpdf") || !strings.Contains(tree, "sub\tfolder") || !strings.Contains(tree, "  b\tpdf") {
		t.Fatalf("after mput:\n%s", tree)
	}
	if _, ok := e.names()["notes.txt"]; ok {
		t.Fatal("mput uploaded a text file")
	}
}

// A root that moves under a write is replayed: the write lands on the new
// root and keeps what the other writer did.
func TestE2E_WriteSurvivesAConcurrentChange(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.ok("mkdir", "/First", "--yes")
	e.cloud.Advance()
	e.ok("mkdir", "/Second", "--yes")
	names := e.names()
	if names["First"] == nil || names["Second"] == nil {
		t.Fatalf("library %v", names)
	}
}
