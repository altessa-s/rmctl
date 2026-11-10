// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
)

var errBoom = errors.New("boom")

// A mirror goes in as a [[mirror]] table of its own, every other line
// stays, and it comes out the same way.
func TestMirrorTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	w := config.Writer{Path: path}
	if mirrors, err := config.Mirrors(path, nil); err != nil || mirrors != nil {
		t.Fatalf("%v %v", mirrors, err)
	}
	papers := config.Mirror{Name: "papers", Dir: `/Users/me/My "Papers"\x`, Folder: "/Papers", Profile: "work", PullNew: true, NoDelete: true}
	if err := w.AddMirror(papers); err != nil {
		t.Fatal(err)
	}
	write(t, path, readFile(t, path)+"\n# the books\n[[mirror]]\nname = \"books\"\ndir = \"/b\"\nfolder = \"/Books\" # by hand\n\n[cache]\nttl = \"5m\"\n")
	if err := w.AddMirror(config.Mirror{Name: "tabs\tand\u007f", Dir: "/t", Folder: "/T"}); err == nil {
		t.Fatal("a name no mirror takes was added")
	}
	if err := w.AddMirror(config.Mirror{Name: "notes", Dir: "/n", Folder: "/Notes"}); err != nil {
		t.Fatal(err)
	}
	mirrors, err := config.Mirrors(path, nil)
	if err != nil || len(mirrors) != 3 || mirrors[0] != papers || mirrors[1].Name != "books" || mirrors[2].Name != "notes" {
		t.Fatalf("%+v %v", mirrors, err)
	}
	if err = w.AddMirror(config.Mirror{Name: "books", Dir: "/x", Folder: "/X"}); !errors.Is(err, config.ErrConfigEdit) {
		t.Fatal(err)
	}
	removed, err := w.RemoveMirror("books")
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	got := readFile(t, path)
	if strings.Contains(got, `name = "books"`) || !strings.Contains(got, "# the books") || !strings.Contains(got, "ttl = \"5m\"") {
		t.Fatalf("%q", got)
	}
	if removed, err = w.RemoveMirror("books"); err != nil || removed {
		t.Fatal(removed, err)
	}
	for _, name := range []string{"papers", "notes"} {
		if removed, err = w.RemoveMirror(name); err != nil || !removed {
			t.Fatal(name, removed, err)
		}
	}
	if got = readFile(t, path); strings.Contains(got, "mirror") || !strings.Contains(got, "[cache]") {
		t.Fatalf("%q", got)
	}
}

// A file that held nothing but the mirror is left empty.
func TestRemoveTheOnlyMirror(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	w := config.Writer{Path: path}
	if err := w.AddMirror(config.Mirror{Name: "a", Dir: "/a", Folder: "/A"}); err != nil {
		t.Fatal(err)
	}
	if removed, err := w.RemoveMirror("a"); err != nil || !removed {
		t.Fatal(removed, err)
	}
	if got := readFile(t, path); got != "" {
		t.Fatalf("%q", got)
	}
}

// A mirror rmctl could not run, or could not take out, is refused.
func TestMirrorTablesRefused(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"name":     "[[mirror]]\nname = \"Bad\"\ndir = \"/a\"\nfolder = \"/A\"\n",
		"twice":    "[[mirror]]\nname = \"a\"\ndir = \"/a\"\nfolder = \"/A\"\n[[mirror]]\nname = \"a\"\ndir = \"/b\"\nfolder = \"/B\"\n",
		"dir":      "[[mirror]]\nname = \"a\"\ndir = \"a\"\nfolder = \"/A\"\n",
		"folder":   "[[mirror]]\nname = \"a\"\ndir = \"/a\"\nfolder = \"A\"\n",
		"profile":  "[[mirror]]\nname = \"a\"\ndir = \"/a\"\nfolder = \"/A\"\nprofile = \"Bad Name\"\n",
		"key":      "[[mirror]]\nname = \"a\"\ndir = \"/a\"\nfolder = \"/A\"\ncolour = \"red\"\n",
		"no table": "mirror = [{name = \"a\", dir = \"/a\", folder = \"/A\"}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.toml")
			write(t, path, body)
			_, err := config.Mirrors(path, nil)
			if name == "no table" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = (config.Writer{Path: path}).RemoveMirror("a"); !errors.Is(err, config.ErrMirrorTables) {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, config.ErrConfig) {
				t.Fatal(err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "not toml [")
	if err := (config.Writer{Path: path}).AddMirror(config.Mirror{Name: "a", Dir: "/a", Folder: "/A"}); !errors.Is(err, config.ErrConfig) {
		t.Fatal(err)
	}
	read := func(string) ([]byte, bool, error) { return nil, false, errBoom }
	if _, err := config.Mirrors(path, read); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if err := (config.Writer{Path: path, Read: read}).AddMirror(config.Mirror{Name: "a", Dir: "/a", Folder: "/A"}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
