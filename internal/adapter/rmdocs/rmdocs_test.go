//go:build cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdocs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmdoc "github.com/altessa-s/rmdoc-go"
)

var files = []domain.ArchiveFile{
	{Name: "a1b2.metadata", Data: []byte(`{"visibleName":"Notes"}`)},
	{Name: "a1b2.content", Data: []byte(`{}`)},
	{Name: "a1b2/p1.rm", Data: []byte("page one")},
	{Name: "c3d4.metadata", Data: []byte(`{"visibleName":"Other"}`)},
}

// sources writes each file to a directory of its own and names it for
// WriteArchive.
func sources(t testing.TB, list []domain.ArchiveFile) []port.ArchiveSource {
	t.Helper()
	dir := t.TempDir()
	out := make([]port.ArchiveSource, len(list))
	for i, f := range list {
		path := filepath.Join(dir, strconv.Itoa(i))
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		out[i] = port.ArchiveSource{Name: f.Name, Path: path}
	}
	return out
}

// packed writes the archive of files twice and checks both are the bytes
// rmdoc-go packs in memory: the tablet's own, deterministic.
func packed(t *testing.T, a Archives) string {
	t.Helper()
	src := sources(t, files)
	path := filepath.Join(t.TempDir(), "x.rmdoc")
	again := filepath.Join(t.TempDir(), "y.rmdoc")
	if err := a.WriteArchive(path, src); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteArchive(again, src); err != nil {
		t.Fatal(err)
	}
	in := make([]rmdoc.File, len(files))
	for i, f := range files {
		in[i] = rmdoc.File{Name: f.Name, Data: f.Data}
	}
	want, err := rmdoc.Pack(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, again} {
		got, err := os.ReadFile(p) // #nosec G304 -- the test's own archive
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s is not the packed archive: %v", p, err)
		}
	}
	return path
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	a := New()
	arc, err := a.OpenArchive(packed(t, a))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arc.Close() }()
	entries := arc.Entries()
	if len(entries) != len(files) || entries[2].Name != "a1b2/p1.rm" || entries[2].DocumentID != "a1b2" || entries[2].Size != 8 ||
		entries[3].DocumentID != "c3d4" {
		t.Fatalf("%+v", entries)
	}
	got := map[string][]byte{}
	err = arc.Read(func(e domain.ArchiveEntry) bool { return e.DocumentID == "a1b2" }, func(e domain.ArchiveEntry, data []byte) error {
		got[e.Name] = bytes.Clone(data)
		return nil
	})
	if err != nil || len(got) != 3 || string(got["a1b2/p1.rm"]) != "page one" {
		t.Fatalf("%v %v", got, err)
	}
	var all []string
	if err := arc.Read(nil, func(e domain.ArchiveEntry, _ []byte) error { all = append(all, e.Name); return nil }); err != nil || len(all) != 4 {
		t.Fatalf("%v %v", all, err)
	}
}

func TestReadStopsOnTheBodysError(t *testing.T) {
	t.Parallel()
	a := New()
	arc, err := a.OpenArchive(packed(t, a))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arc.Close() }()
	stop := errors.New("stop")
	calls := 0
	err = arc.Read(nil, func(domain.ArchiveEntry, []byte) error { calls++; return stop })
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("%v after %d calls", err, calls)
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()
	a := New()
	path := packed(t, a)
	small := rmdoc.DefaultLimits()
	small.MaximumEntrySize = 4
	arc, err := Archives{limits: small}.OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arc.Close() }()
	called := false
	if err := arc.Read(nil, func(domain.ArchiveEntry, []byte) error { called = true; return nil }); err == nil || called {
		t.Fatalf("an entry past the limit was read: %v %v", err, called)
	}
}

func TestRefusals(t *testing.T) {
	t.Parallel()
	a := New()
	for _, name := range []string{"a1b2.metadata", "a1b2/p1.rm"} {
		if err := a.CheckName(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"", "../x", "a\\b", "/abs", "a\x00b"} {
		if a.CheckName(name) == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	dir := t.TempDir()
	if err := a.WriteArchive(filepath.Join(dir, "empty.rmdoc"), nil); err == nil {
		t.Fatal("an empty archive was written")
	}
	twice := sources(t, []domain.ArchiveFile{{Name: "x.metadata"}, {Name: "x.metadata"}})
	if err := a.WriteArchive(filepath.Join(dir, "twice.rmdoc"), twice); err == nil {
		t.Fatal("a repeated name was written")
	}
	missing := []port.ArchiveSource{{Name: "x.metadata", Path: filepath.Join(dir, "none")}}
	if err := a.WriteArchive(filepath.Join(dir, "missing.rmdoc"), missing); err == nil {
		t.Fatal("a missing source was written")
	}
	if err := a.WriteArchive(filepath.Join(dir, "no", "such", "dir.rmdoc"), sources(t, files)); err == nil {
		t.Fatal("an archive was written where no directory is")
	}
	if _, err := a.OpenArchive(filepath.Join(t.TempDir(), "missing.rmdoc")); err == nil {
		t.Fatal("a missing file opened")
	}
	garbage := filepath.Join(t.TempDir(), "garbage.rmdoc")
	if err := os.WriteFile(garbage, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenArchive(garbage); err == nil {
		t.Fatal("garbage opened as an archive")
	}
}
