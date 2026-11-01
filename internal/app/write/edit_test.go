// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// fileHashes are an item's files by name.
func fileHashes(it domain.Item) map[string]string {
	out := map[string]string{}
	for _, f := range it.Files {
		out[f.Name] = f.Hash
	}
	return out
}

// onlyChanged reports the names whose hash differs between two items.
func onlyChanged(before, after domain.Item) []string {
	b, a := fileHashes(before), fileHashes(after)
	var out []string
	for name, h := range a {
		if b[name] != h {
			out = append(out, name)
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func TestPin(t *testing.T) {
	t.Parallel()
	f := setup(t)
	before, _ := tree(t, f).Get("d1aaaaaa")
	records := f.c.Records()
	edited, err := f.s.Pin(t.Context(), []string{"/Books/Paper", "/Books", "d1aaaaaa"}, true)
	if err != nil || len(edited) != 2 || !edited[0].Changed || !edited[1].Changed || !edited[0].Item.Pinned {
		t.Fatalf("%+v %v", edited, err)
	}
	after, _ := tree(t, f).Get("d1aaaaaa")
	if got := onlyChanged(before, after); !slices.Equal(got, []string{"d1aaaaaa.metadata"}) {
		t.Fatalf("changed %v", got)
	}
	m := metadata(t, f, "d1aaaaaa")
	if m["pinned"] != true || m["visibleName"] != "Paper" || m["parent"] != "f1aaaaaa" {
		t.Fatalf("%v", m)
	}
	// Pinning again changes nothing and commits nothing.
	records = f.c.Records()
	edited, err = f.s.Pin(t.Context(), []string{"/Books/Paper"}, true)
	if err != nil || edited[0].Changed || !slices.Equal(f.c.Records(), records) {
		t.Fatalf("%+v %v", edited, err)
	}
	if edited, err = f.s.Pin(t.Context(), []string{"/Books/Paper"}, false); err != nil || !edited[0].Changed || metadata(t, f, "d1aaaaaa")["pinned"] != false {
		t.Fatalf("%+v %v", edited, err)
	}
	if _, err := f.s.Pin(t.Context(), []string{"/Nothing"}, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestTag(t *testing.T) {
	t.Parallel()
	f := setup(t)
	before, _ := tree(t, f).Get("d1aaaaaa")
	edited, err := f.s.Tag(t.Context(), []string{"/Books/Paper", "/Books/Fiction/Novel"}, []string{"work", "2026"}, nil)
	if err != nil || len(edited) != 2 || !slices.Equal(edited[0].Item.Tags, []string{"work", "2026"}) {
		t.Fatalf("%+v %v", edited, err)
	}
	after, _ := tree(t, f).Get("d1aaaaaa")
	if got := onlyChanged(before, after); !slices.Equal(got, []string{"d1aaaaaa.content", "d1aaaaaa.metadata"}) {
		t.Fatalf("changed %v", got)
	}
	// Every other field of the .content stays.
	c := contentOf(t, f, after)
	if fmt.Sprint(c["tags"]) != fmt.Sprintf("[map[name:work timestamp:%v] map[name:2026 timestamp:%v]]",
		float64(porttest.Epoch.UnixMilli()), float64(porttest.Epoch.UnixMilli())) ||
		c["fileType"] != "pdf" || c["zoomMode"] != contentOf(t, f, before)["zoomMode"] {
		t.Fatalf("%v", c)
	}
	if metadata(t, f, "d1aaaaaa")["visibleName"] != "Paper" {
		t.Fatal("the name changed")
	}
	// A tag already there is no change.
	records := f.c.Records()
	if edited, err = f.s.Tag(t.Context(), []string{"/Books/Paper"}, []string{"work"}, nil); err != nil || edited[0].Changed || !slices.Equal(f.c.Records(), records) {
		t.Fatalf("%+v %v", edited, err)
	}
	if edited, err = f.s.Tag(t.Context(), []string{"/Books/Paper"}, nil, []string{"work", "absent"}); err != nil || !edited[0].Changed ||
		!slices.Equal(edited[0].Item.Tags, []string{"2026"}) {
		t.Fatalf("%+v %v", edited, err)
	}
	if edited, err = f.s.Tag(t.Context(), []string{"/Books/Paper"}, nil, []string{"absent"}); err != nil || edited[0].Changed {
		t.Fatalf("%+v %v", edited, err)
	}
	// A folder takes no tags, and nothing is written for the others.
	records = f.c.Records()
	if _, err := f.s.Tag(t.Context(), []string{"/Books/Paper", "/Books"}, []string{"x"}, nil); !errors.Is(err, domain.ErrFolderTags) || !slices.Equal(f.c.Records(), records) {
		t.Fatal(err)
	}
}

func TestRename(t *testing.T) {
	t.Parallel()
	f := setup(t)
	before, _ := tree(t, f).Get("d1aaaaaa")
	e, err := f.s.Rename(t.Context(), "/Books/Paper", "Paper 2026")
	if err != nil || !e.Changed || e.Item.Name != "Paper 2026" || e.Item.Parent != "f1aaaaaa" {
		t.Fatalf("%+v %v", e, err)
	}
	after, _ := tree(t, f).Get("d1aaaaaa")
	if got := onlyChanged(before, after); !slices.Equal(got, []string{"d1aaaaaa.metadata"}) {
		t.Fatalf("changed %v", got)
	}
	if e, err = f.s.Rename(t.Context(), "/Books/Paper 2026", "Paper 2026"); err != nil || e.Changed {
		t.Fatalf("%+v %v", e, err)
	}
	if e, err = f.s.Rename(t.Context(), "/Books/Fiction", "Novels"); err != nil || !e.Changed || metadata(t, f, "f2aaaaaa")["visibleName"] != "Novels" {
		t.Fatalf("a folder: %+v %v", e, err)
	}
	for name, want := range map[string]error{"": domain.ErrEmptyName, "  ": domain.ErrEmptyName, "a/b": domain.ErrSlashInName} {
		if _, err := f.s.Rename(t.Context(), "d1aaaaaa", name); !errors.Is(err, want) {
			t.Errorf("%q: %v", name, err)
		}
	}
}

// An edit replays on a conflict against the current root.
func TestEditReplays(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.c.Conflicts = 1
	edited, err := f.s.Pin(t.Context(), []string{"/Books/Paper"}, true)
	if err != nil || !edited[0].Changed || metadata(t, f, "d1aaaaaa")["pinned"] != true {
		t.Fatalf("%+v %v", edited, err)
	}
	f.c.Fail = map[string]error{"Upload": errBoom}
	if _, err := f.s.Tag(t.Context(), []string{"/Books/Paper"}, []string{"x"}, nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	f.c.Fail = map[string]error{"PutIndex": errBoom}
	if _, err := f.s.Rename(t.Context(), "/Books/Paper", "y"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}
