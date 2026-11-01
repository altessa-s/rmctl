// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

func manifest() domain.BackupManifest {
	return domain.BackupManifest{
		Format: domain.BackupFormat, Profile: "work", Created: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), Path: "/",
		Documents: []domain.BackupEntry{
			{ID: "bbbb-2", Name: "Novel", Path: "/Books/Novel", Kind: domain.KindEPUB, Hash: "h2", File: "bbbb-2.rmdoc", Size: 20},
			{ID: "aaaa-1", Name: "Paper", Path: "/Paper", Kind: domain.KindPDF, Hash: "h1", File: "aaaa-1.rmdoc", Size: 10, Trashed: true},
			{ID: "aaab-3", Name: "Notes", Path: "/Books/Notes", Kind: domain.KindNotebook, Hash: "h3", File: "aaab-3.rmdoc", Size: 30},
		},
	}
}

func TestBackupManifestRoundTrip(t *testing.T) {
	t.Parallel()
	raw := manifest().Bytes()
	for _, want := range []string{`"format": 1`, `"profile": "work"`, `"created": "2026-11-02T10:00:00Z"`, `"trashed": true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing from %s", want, raw)
		}
	}
	m, err := domain.ParseBackupManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Path order.
	var paths []string
	for _, e := range m.Documents {
		paths = append(paths, e.Path)
	}
	if !slices.Equal(paths, []string{"/Books/Notes", "/Books/Novel", "/Paper"}) || m.Profile != "work" {
		t.Fatalf("%v %+v", paths, m)
	}
	if e, ok := m.Entry("aaaa-1"); !ok || e.Size != 10 || !e.Trashed || e.Kind != domain.KindPDF {
		t.Fatalf("%+v", e)
	}
	if _, ok := m.Entry("none"); ok {
		t.Fatal("found an entry that is not there")
	}
	// An empty manifest lists no documents, not null.
	if empty := (domain.BackupManifest{Format: 1}).Bytes(); !strings.Contains(string(empty), `"documents": []`) {
		t.Fatalf("%s", empty)
	}
	// The entry order of equal paths is the identifier's.
	twin := domain.BackupManifest{Format: 1, Documents: []domain.BackupEntry{
		{ID: "b", Path: "/X", File: "b.rmdoc"}, {ID: "a", Path: "/X", File: "a.rmdoc"},
	}}
	if back, _ := domain.ParseBackupManifest(twin.Bytes()); back.Documents[0].ID != "a" {
		t.Fatalf("%+v", back.Documents)
	}
}

func TestParseBackupManifestRefuses(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"not json":   `{`,
		"no format":  `{"documents": []}`,
		"newer":      `{"format": 2}`,
		"no id":      `{"format": 1, "documents": [{"file": "a.rmdoc"}]}`,
		"escape":     `{"format": 1, "documents": [{"id": "a", "file": "../a.rmdoc"}]}`,
		"hidden":     `{"format": 1, "documents": [{"id": "a", "file": ".a.rmdoc"}]}`,
		"other ext":  `{"format": 1, "documents": [{"id": "a", "file": "a.pdf"}]}`,
		"bare ext":   `{"format": 1, "documents": [{"id": "a", "file": ".rmdoc"}]}`,
		"twice id":   `{"format": 1, "documents": [{"id": "a", "file": "a.rmdoc"}, {"id": "a", "file": "b.rmdoc"}]}`,
		"twice file": `{"format": 1, "documents": [{"id": "a", "file": "a.rmdoc"}, {"id": "b", "file": "a.rmdoc"}]}`,
	} {
		if _, err := domain.ParseBackupManifest([]byte(raw)); !errors.Is(err, domain.ErrBackupManifest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBackupManifestSelect(t *testing.T) {
	t.Parallel()
	m := manifest()
	all, err := m.Select(nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("%v %v", all, err)
	}
	// An identifier, a unique prefix; manifest order, each once.
	got, err := m.Select([]string{"aaab", "bbbb-2", "aaab-3"})
	if err != nil || len(got) != 2 || got[0].ID != "bbbb-2" || got[1].ID != "aaab-3" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err = m.Select([]string{"zz"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err = m.Select([]string{""}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	_, err = m.Select([]string{"aaa"})
	if amb, ok := errors.AsType[*domain.AmbiguousError](err); !ok || len(amb.Candidates) != 2 {
		t.Fatalf("%v", err)
	}
}

func TestBackupEntryFolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ path, name, want string }{
		{"/Books/Novel", "Novel", "/Books"},
		{"/Novel", "Novel", "/"},
		{"/A/B/C", "C", "/A/B"},
		// A path that does not end with the name.
		{"/A/Other", "Novel", "/A"},
		{"", "Novel", "/"},
	} {
		if got := (domain.BackupEntry{Path: tc.path, Name: tc.name}).Folder(); got != tc.want {
			t.Errorf("%q %q: %q, want %q", tc.path, tc.name, got, tc.want)
		}
	}
}

func TestNewBackupEntry(t *testing.T) {
	t.Parallel()
	items := []domain.Item{
		{ID: "f", Name: "Books", Kind: domain.KindFolder},
		{ID: "d", Name: "Novel", Kind: domain.KindEPUB, Parent: "f", Hash: "h"},
	}
	tree := domain.NewTree(1, items)
	e := domain.NewBackupEntry(tree, items[1], "d.rmdoc", 42)
	want := domain.BackupEntry{ID: "d", Name: "Novel", Path: "/Books/Novel", Kind: domain.KindEPUB, Hash: "h", File: "d.rmdoc", Size: 42}
	if e != want {
		t.Fatalf("%+v", e)
	}
}
