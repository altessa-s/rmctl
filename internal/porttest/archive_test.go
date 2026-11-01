// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestArchiverRoundTrip(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	a := porttest.NewArchiver(files)
	data, err := a.Pack([]domain.ArchiveFile{
		{Name: "a1.metadata", Data: []byte("{}")},
		{Name: "a1/p.rm", Data: []byte("page")},
		{Name: "b2.content", Data: []byte("c")},
	})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("x.rmdoc", data)
	arc, err := a.OpenArchive("x.rmdoc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arc.Close() }()
	entries := arc.Entries()
	if len(entries) != 3 || entries[1].DocumentID != "a1" || entries[1].Size != 4 || entries[2].DocumentID != "b2" {
		t.Fatalf("%+v", entries)
	}
	var got []string
	err = arc.Read(func(e domain.ArchiveEntry) bool { return e.DocumentID == "a1" }, func(e domain.ArchiveEntry, _ []byte) error {
		got = append(got, e.Name)
		return nil
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	stop := errors.New("stop")
	if err := arc.Read(nil, func(domain.ArchiveEntry, []byte) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("got %v", err)
	}
}

func TestArchiverRefusals(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	a := porttest.NewArchiver(files)
	for _, name := range []string{"", "a/b/c", "a\\b", ".x", "a/.x", "a//b"} {
		if err := a.CheckName(name); !errors.Is(err, porttest.ErrBadArchiveName) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if _, err := a.Pack(nil); err == nil {
		t.Fatal("an empty archive was packed")
	}
	if _, err := a.Pack([]domain.ArchiveFile{{Name: "../x"}}); !errors.Is(err, porttest.ErrBadArchiveName) {
		t.Fatalf("got %v", err)
	}
	if _, err := a.Pack([]domain.ArchiveFile{{Name: "x.a"}, {Name: "x.a"}}); !errors.Is(err, porttest.ErrBadArchiveName) {
		t.Fatalf("got %v", err)
	}
	files.Write("src", []byte("body"))
	if err := a.WriteArchive("w.rmdoc", []port.ArchiveSource{{Name: "x.a", Path: "src"}}); err != nil {
		t.Fatal(err)
	}
	if w, err := a.OpenArchive("w.rmdoc"); err != nil || len(w.Entries()) != 1 || w.Entries()[0].Size != 4 {
		t.Fatalf("%v", err)
	}
	if err := a.WriteArchive("w.rmdoc", []port.ArchiveSource{{Name: "x.a", Path: "none"}}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
	if err := a.WriteArchive("w.rmdoc", []port.ArchiveSource{{Name: "../x", Path: "src"}}); !errors.Is(err, porttest.ErrBadArchiveName) {
		t.Fatalf("got %v", err)
	}
	if _, err := a.OpenArchive("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
	files.Write("junk", []byte("not json"))
	if _, err := a.OpenArchive("junk"); err == nil {
		t.Fatal("junk opened")
	}
	boom := errors.New("boom")
	for _, method := range []string{"CheckName", "Pack", "WriteArchive", "OpenArchive", "Read"} {
		a.Fail[method] = boom
	}
	if a.CheckName("x.a") == nil {
		t.Fatal("CheckName")
	}
	if _, err := a.Pack([]domain.ArchiveFile{{Name: "x.a"}}); !errors.Is(err, boom) {
		t.Fatal("Pack")
	}
	if _, err := a.OpenArchive("junk"); !errors.Is(err, boom) {
		t.Fatal("OpenArchive")
	}
	if err := a.WriteArchive("w.rmdoc", nil); !errors.Is(err, boom) {
		t.Fatal("WriteArchive")
	}
	delete(a.Fail, "OpenArchive")
	delete(a.Fail, "Pack")
	delete(a.Fail, "CheckName")
	data, err := a.Pack([]domain.ArchiveFile{{Name: "x.a"}})
	if err != nil {
		t.Fatal(err)
	}
	files.Write("ok", data)
	arc, err := a.OpenArchive("ok")
	if err != nil {
		t.Fatal(err)
	}
	if err := arc.Read(nil, func(domain.ArchiveEntry, []byte) error { return nil }); !errors.Is(err, boom) {
		t.Fatal("Read")
	}
}
