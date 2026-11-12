//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/flock"
	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/wiring"

	jsonv1 "encoding/json"
)

// The identifiers of the mirrored library.
const (
	mirrorFolderID = "f0000000-0000-4000-8000-0000000000a0"
	keptID         = "d0000000-0000-4000-8000-0000000000a1"
	tabletNoteID   = "d0000000-0000-4000-8000-0000000000a2"
	movedFolderID  = "f0000000-0000-4000-8000-0000000000a3"
)

// mirrorDir is a local directory with files written a minute ago, settled.
type mirrorDir struct {
	t    *testing.T
	path string
}

func (d mirrorDir) write(rel string, data []byte) {
	d.t.Helper()
	path := filepath.Join(d.path, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		d.t.Fatal(err)
	}
	d.settle(rel)
}

// settle dates a file a minute back: the mirror takes only files left
// alone for a while.
func (d mirrorDir) settle(rel string) {
	d.t.Helper()
	back := time.Now().Add(-time.Minute) //nolint:forbidigo // the file system's own clock
	if err := os.Chtimes(filepath.Join(d.path, filepath.FromSlash(rel)), back, back); err != nil {
		d.t.Fatal(err)
	}
}

func (d mirrorDir) has(rel string) bool {
	_, err := os.Stat(filepath.Join(d.path, filepath.FromSlash(rel)))
	return err == nil
}

func (d mirrorDir) read(rel string) []byte {
	d.t.Helper()
	b, err := os.ReadFile(filepath.Join(d.path, filepath.FromSlash(rel)))
	if err != nil {
		d.t.Fatal(err)
	}
	return b
}

// idOf is the identifier of the document named name in the cloud.
func (e *e2e) idOf(name string) string {
	e.t.Helper()
	for _, rec := range e.cloud.Records(e.t) {
		if meta := e.cloud.Metadata(e.t, rec.DocumentID); meta["visibleName"] == name {
			return rec.DocumentID
		}
	}
	e.t.Fatalf("no document %s", name)
	return ""
}

// trashed reports whether the item id is in the trash.
func (e *e2e) trashed(id string) bool {
	return e.cloud.Metadata(e.t, id)["parent"] == "trash"
}

// pass runs mirror run and returns its JSON lines' actions as "action path".
func (e *e2e) pass(args ...string) []string {
	e.t.Helper()
	out := e.ok(append([]string{"mirror", "run", "--json"}, args...)...)
	type row struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		To     string `json:"to"`
		Error  string `json:"error"`
	}
	// A stream is one JSON value per row.
	var rows []row
	dec := jsonv1.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var r row
		if err := dec.Decode(&r); err != nil {
			e.t.Fatalf("%v\n%s", err, out)
		}
		rows = append(rows, r)
	}
	var got []string
	for _, r := range rows {
		if r.Error != "" {
			e.t.Fatalf("%s %s failed: %s", r.Action, r.Path, r.Error)
		}
		if r.Action != "pass" {
			got = append(got, strings.Join(strings.Fields(r.Action+" "+r.Path+" "+r.To), " "))
		}
	}
	return got
}

func wantActions(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A mirror through the whole tool against the emulated cloud: uploads,
// adoption, drawings (or .rmdoc in rmctl-lite), renames and moves both ways,
// content updates and conflicts, deletions both ways, the mass-deletion
// guard, documents created on the tablet, and a dry run.
func TestE2E_Mirror(t *testing.T) {
	e := newE2E(t)
	e.register()
	renders := wiring.Renders()
	drawn, ext := "draw", "pdf"
	if !renders {
		drawn, ext = "export", "rmdoc"
	}
	page := tabletPage(t, "Bold_Heading_Bullet_Normal.rm")
	e.cloud.Seed(t,
		clouddouble.Item{ID: mirrorFolderID, Name: "Papers", Kind: clouddouble.Folder},
		clouddouble.Item{ID: keptID, Name: "Kept", Parent: mirrorFolderID, Kind: clouddouble.PDF, Body: clouddouble.MakePDF(2), PageBodies: [][]byte{page}},
	)
	d := mirrorDir{t: t, path: t.TempDir()}
	d.write("Kept.pdf", clouddouble.MakePDF(2))
	d.write("Book.pdf", clouddouble.MakePDF(1))
	d.write("Sub/Other.pdf", clouddouble.MakePDF(1))
	d.write("Sub/Third.pdf", clouddouble.MakePDF(3))
	d.write("Ignored.txt", []byte("not a document"))

	e.ok("mirror", "add", "papers", d.path, "/Papers")
	if out := e.ok("mirror", "ls"); !strings.Contains(out, "papers") || !strings.Contains(out, "never run") {
		t.Fatalf("ls: %q", out)
	}

	// A dry run changes nothing.
	records := len(e.cloud.Records(t))
	wantActions(t, e.pass("papers", "--dry-run"),
		"upload Book.pdf", "upload Sub/Other.pdf", "upload Sub/Third.pdf", "adopt Kept.pdf", drawn+" Kept.pdf _annotated/Kept."+ext)
	if len(e.cloud.Records(t)) != records || d.has(domain.MirrorJournalName) {
		t.Fatal("a dry run changed something")
	}

	// The first pass: up, paired, drawn.
	wantActions(t, e.pass("papers"),
		"upload Book.pdf", "upload Sub/Other.pdf", "upload Sub/Third.pdf", "adopt Kept.pdf", drawn+" Kept.pdf _annotated/Kept."+ext)
	drawing := d.read("_annotated/Kept." + ext)
	if renders && !bytes.HasPrefix(drawing, []byte("%PDF-")) || !renders && !bytes.HasPrefix(drawing, []byte("PK")) {
		t.Fatalf("the drawing starts %q", drawing[:min(len(drawing), 8)])
	}
	if e.cloud.Metadata(t, e.idOf("Other"))["parent"] == mirrorFolderID {
		t.Fatal("Other did not go into Sub")
	}
	wantActions(t, e.pass("papers"))

	// Annotated on the tablet: drawn.
	book := e.idOf("Book")
	e.cloud.Replace(t, clouddouble.Item{ID: book, Name: "Book", Parent: mirrorFolderID, Kind: clouddouble.PDF, Body: clouddouble.MakePDF(1), PageBodies: [][]byte{page}})
	wantActions(t, e.pass("papers"), drawn+" Book.pdf _annotated/Book."+ext)

	// Renamed and moved on the tablet: the file and its drawing follow.
	e.cloud.Replace(t,
		clouddouble.Item{ID: movedFolderID, Name: "Read", Parent: mirrorFolderID, Kind: clouddouble.Folder},
		clouddouble.Item{ID: book, Name: "Book (read)", Parent: movedFolderID, Kind: clouddouble.PDF, Body: clouddouble.MakePDF(1), PageBodies: [][]byte{page}},
	)
	wantActions(t, e.pass("papers"), "move-local Book.pdf Read/Book (read).pdf")
	if !d.has("Read/Book (read).pdf") || !d.has("_annotated/Read/Book (read)."+ext) || d.has("Book.pdf") {
		t.Fatal("the file or its drawing did not follow")
	}

	// Renamed locally: the document follows.
	if err := os.Rename(filepath.Join(d.path, "Sub", "Other.pdf"), filepath.Join(d.path, "Sub", "Renamed.pdf")); err != nil {
		t.Fatal(err)
	}
	wantActions(t, e.pass("papers"), "move-cloud Sub/Other.pdf Sub/Renamed.pdf")
	if e.cloud.Metadata(t, e.idOf("Renamed"))["visibleName"] != "Renamed" {
		t.Fatal("the document was not renamed")
	}

	// Changed locally, and on the tablet too: the PDF goes up, the ink stays.
	e.cloud.Replace(t, clouddouble.Item{ID: keptID, Name: "Kept", Parent: mirrorFolderID, Kind: clouddouble.PDF, Body: clouddouble.MakePDF(2),
		PageBodies: [][]byte{page, page}})
	d.write("Kept.pdf", clouddouble.MakePDF(3))
	wantActions(t, e.pass("papers"), "conflict Kept.pdf", drawn+" Kept.pdf _annotated/Kept."+ext)
	if pdf, _ := e.cloud.File(t, keptID, keptID+".pdf"); !bytes.Equal(pdf, clouddouble.MakePDF(3)) {
		t.Fatal("the PDF was not replaced")
	}
	if rm, ok := e.cloud.File(t, keptID, keptID+"/page-1.rm"); ok && len(rm) == 0 {
		t.Fatal("the ink was lost")
	}
	inked := 0
	for _, f := range e.cloud.Files(t, keptID) {
		if strings.HasSuffix(f.DocumentID, ".rm") {
			inked++
		}
	}
	if inked != 2 {
		t.Fatalf("%d pages of ink, want 2", inked)
	}

	// Deleted on either side: the trash, and .rmctl-removed.
	third := e.idOf("Third")
	if err := os.Remove(filepath.Join(d.path, "Sub", "Third.pdf")); err != nil {
		t.Fatal(err)
	}
	e.cloud.Remove(t, book)
	wantActions(t, e.pass("papers"), "remove Read/Book (read).pdf .rmctl-removed/Read/Book (read).pdf", "trash Sub/Third.pdf")
	if !e.trashed(third) || !d.has(".rmctl-removed/Read/Book (read).pdf") || !d.has(".rmctl-removed/_annotated/Read/Book (read)."+ext) {
		t.Fatal("a deletion was not carried")
	}

	// Most of the directory gone: refused, then forced.
	for _, rel := range []string{"Kept.pdf", "Sub/Renamed.pdf"} {
		if err := os.Remove(filepath.Join(d.path, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	wantActions(t, e.pass("papers"), "refuse Kept.pdf", "refuse Sub/Renamed.pdf")
	if e.trashed(keptID) {
		t.Fatal("a refused deletion was carried out")
	}
	wantActions(t, e.pass("papers", "--force-delete"), "trash Kept.pdf", "trash Sub/Renamed.pdf")

	// Created on the tablet: drawn only with --pull-new.
	e.cloud.Replace(t, clouddouble.Item{ID: tabletNoteID, Name: "Retro", Parent: mirrorFolderID, Kind: clouddouble.Notebook, PageBodies: [][]byte{page}})
	wantActions(t, e.pass("papers"))
	wantActions(t, e.pass("--dir", d.path, "--folder", "/Papers", "--pull-new", "--yes"), drawn+" _annotated/Retro."+ext)
	if !d.has("_annotated/Retro." + ext) {
		t.Fatal("the notebook was not drawn")
	}

	var states []map[string]any
	if err := json.Unmarshal([]byte(e.ok("mirror", "ls", "--json")), &states); err != nil || len(states) != 1 || states[0]["last_pass"] == "" {
		t.Fatalf("%v %v", states, err)
	}
	e.ok("mirror", "rm", "papers")
	if !d.has(domain.MirrorJournalName) {
		t.Fatal("mirror rm touched the directory")
	}
}

// A second process on the same directory is refused while one holds it.
func TestE2E_MirrorLock(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t, clouddouble.Item{ID: mirrorFolderID, Name: "Papers", Kind: clouddouble.Folder})
	dir := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(dir, domain.MirrorLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if !lockFile(t, lock) {
		t.Skip("no advisory lock on this system")
	}
	code, _, errOut := e.rmctl("mirror", "run", "--dir", dir, "--folder", "/Papers", "--yes")
	if code != 1 || !strings.Contains(errOut, "another rmctl is working on this directory") {
		t.Fatalf("exit %d %q", code, errOut)
	}
	code, _, errOut = e.rmctl("mirror", "run", "--dir", filepath.Join(dir, "missing"), "--folder", "/Papers", "--yes")
	if code != 1 || !strings.Contains(errOut, "mounted") {
		t.Fatalf("exit %d %q", code, errOut)
	}
}

// lockFile takes the advisory lock on f as another process would; false is
// a system without one.
func lockFile(t *testing.T, f *os.File) bool {
	t.Helper()
	if runtime.GOOS == "plan9" || runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		return false
	}
	busy, err := flock.TryLock(f)
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	return true
}

// serviceTools stand for launchctl, systemctl and schtasks.
type serviceTools struct {
	mu    sync.Mutex
	calls []string
}

func (s *serviceTools) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name+" "+strings.Join(args, " "))
	if name == "launchctl" && len(args) > 0 && args[0] == "print" {
		return []byte("state = running"), nil
	}
	if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
		return []byte("active\n"), nil
	}
	return nil, nil
}

// The service is defined for this system and the user alone, runs this
// rmctl as the service, and its log is read back.
func TestE2E_MirrorService(t *testing.T) {
	isolate(t)
	tools := &serviceTools{}
	home := t.TempDir()
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runWith(args, nil, &out, &errOut, func(int) { t.Fatal("exit") }, seams{service: tools, home: home})
		return code, out.String(), errOut.String()
	}
	code, out, errOut := run("mirror", "service", "install", "--json")
	if code != 0 {
		t.Fatalf("exit %d %q", code, errOut)
	}
	var info struct {
		File      string `json:"file"`
		Installed bool   `json:"installed"`
		Log       string `json:"log"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || !info.Installed {
		t.Fatalf("%v %q", err, out)
	}
	definition, err := os.ReadFile(info.File)
	if err != nil {
		t.Fatal(err)
	}
	text := string(definition)
	if runtime.GOOS == "windows" {
		text = strings.ReplaceAll(text, "\x00", "")
	}
	if !strings.Contains(text, "mirror") || !strings.Contains(text, "--service") {
		t.Fatalf("%q", text)
	}
	if err = os.MkdirAll(filepath.Dir(info.Log), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(info.Log, []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ = run("mirror", "service", "logs", "-n", "1"); code != 0 || out != "second\n" {
		t.Fatalf("exit %d %q", code, out)
	}
	if code, out, _ = run("mirror", "service", "status"); code != 0 || !strings.Contains(out, "installed") {
		t.Fatalf("exit %d %q", code, out)
	}
	if code, _, _ = run("mirror", "service", "uninstall"); code != 0 {
		t.Fatal(code)
	}
	if _, err = os.Stat(info.File); err == nil {
		t.Fatal("the definition stayed")
	}
}
