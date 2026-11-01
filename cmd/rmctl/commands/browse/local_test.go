// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// localArchive is a file on disk named like an archive: what LocalArchive
// asks of an argument before it is read offline.
func localArchive(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b.rmdoc")
	if err := os.WriteFile(path, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func archived() []port.DocumentText {
	page := func(text string) domain.Page {
		return domain.Page{Number: 1, ID: "p1", Paragraphs: []domain.Paragraph{{Text: text, Style: domain.StylePlain}},
			Highlights: []domain.Highlight{{ID: "1:1", Text: "marked " + text, HasText: true, Color: "yellow"}}}
	}
	return []port.DocumentText{
		{Item: domain.Item{ID: "n1", Name: "One"}, Pages: []domain.Page{page("first")}},
		{Item: domain.Item{ID: "n2", Name: "Two"}, Pages: []domain.Page{page("second")}},
	}
}

func TestTextOfALocalArchive(t *testing.T) {
	t.Parallel()
	path := localArchive(t)
	f := &clitest.Fake{Local: archived()}
	r := run(t, f, "text", path, "--page", "1")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"LocalPages " + path + " 1"}) ||
		!strings.Contains(r.Out, "== One ==") || !strings.Contains(r.Out, "\n== Two ==") || !strings.Contains(r.Out, "second") {
		t.Fatalf("%d %v %v\n%s", r.Code, r.Error, f.Calls(), r.Out)
	}
	r = run(t, f, "text", path, "--format", "markdown")
	if !strings.Contains(r.Out, "# One\n\n") {
		t.Fatalf("%q", r.Out)
	}
	// An archive is always an array in JSON, whatever its document count.
	one := &clitest.Fake{Local: archived()[:1]}
	r = run(t, one, "text", path, "--json")
	if r.Code != cliutil.CodeOK || !strings.HasPrefix(strings.TrimSpace(r.Out), "[") || strings.Contains(r.Out, "== One") {
		t.Fatalf("%q", r.Out)
	}
}

func TestHighlightsOfALocalArchive(t *testing.T) {
	t.Parallel()
	path := localArchive(t)
	f := &clitest.Fake{Local: archived()}
	r := run(t, f, "highlights", path)
	if r.Code != cliutil.CodeOK || !strings.Contains(r.Out, "marked first") || !strings.Contains(r.Out, "== Two ==") {
		t.Fatalf("%d %v\n%s", r.Code, r.Error, r.Out)
	}
	r = run(t, f, "highlights", path, "--format", "markdown")
	if !strings.Contains(r.Out, "# Two") {
		t.Fatalf("%q", r.Out)
	}
	r = run(t, f, "highlights", path, "--json")
	if !strings.HasPrefix(strings.TrimSpace(r.Out), "[") {
		t.Fatalf("%q", r.Out)
	}
}

func TestLocalArchiveRule(t *testing.T) {
	t.Parallel()
	path := localArchive(t)
	// A name that only looks like an archive, and is no local file, is a
	// document in the cloud.
	f := &clitest.Fake{Document: port.DocumentText{Item: domain.Item{ID: "d", Name: "Cloud"}}}
	if r := run(t, f, "text", "/Backups/old.rmdoc"); r.Code != cliutil.CodeOK || f.Calls()[0] != "Pages /Backups/old.rmdoc" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	if cliutil.LocalArchive(filepath.Dir(path)) || !cliutil.LocalArchive(path) || cliutil.LocalArchive(path+".x") {
		t.Fatal("LocalArchive")
	}
	bad := &clitest.Fake{ArchiveErr: domain.ErrArchivesUnsupported}
	if r := run(t, bad, "text", path); !errors.Is(r.Error, cliutil.ErrNoArchives) {
		t.Fatalf("%v", r.Error)
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "text", path); r.Code == cliutil.CodeOK {
		t.Fatal("ran without the archives service")
	}
}
