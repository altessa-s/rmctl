// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func imported() []port.ImportedDocument {
	return []port.ImportedDocument{
		{Item: clitest.Item(clitest.Spec{ID: "n1", Name: "Notes", Kind: domain.KindNotebook}), From: "n1"},
		{Item: clitest.Item(clitest.Spec{ID: "new-2", Name: "Copy", Kind: domain.KindNotebook}), From: "d-old"},
	}
}

func TestPutImportsAnArchive(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Imported: imported()}
	r := run(t, f, clitest.Options{}, "put", "backup.rmdoc", "/Archive", "--yes")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Import backup.rmdoc /Archive"}) {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	if r.Out != "imported\tNotes\tnotebook\tn1\t-\tbackup.rmdoc\t/Archive\nimported\tCopy\tnotebook\tnew-2\td-old\tbackup.rmdoc\t/Archive\n" {
		t.Fatalf("%q", r.Out)
	}
	// Mixed with a PDF in one put, and as JSON.
	f = &clitest.Fake{Imported: imported()}
	r = run(t, f, clitest.Options{}, "put", "paper.pdf", "backup.rmdoc", "--to", "/A", "--yes", "--json")
	if r.Code != cliutil.CodeOK || len(f.Calls()) != 2 || !strings.Contains(r.Out, `"renamed_from": "d-old"`) {
		t.Fatalf("%d %v %v\n%s", r.Code, r.Error, f.Calls(), r.Out)
	}
}

// A dry run plans against the library: it imports nothing, and an
// identifier the library holds shows as the new one it would get.
func TestPutArchiveDryRunPlans(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Imported: imported()}
	r := run(t, f, clitest.Options{}, "put", "b.rmdoc", "/A", "--dry-run")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"PlanImport b.rmdoc /A"}) {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	want := "would import\tNotes\tnotebook\tn1\t-\tb.rmdoc\t/A\n" +
		"would import\tCopy\tnotebook\tnew-2\td-old\tb.rmdoc\t/A\n"
	if r.Out != want {
		t.Fatalf("%q", r.Out)
	}
}

func TestPutArchiveRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"put", "b.rmdoc", "--notebook", "--yes"},
		{"put", "b.rmdoc", "--name", "X", "--yes"},
		{"put", "b.rmdoc", "--cover", "first", "--yes"},
		{"put", "b.rmdoc", "--replace", "--yes"},
	} {
		f := &clitest.Fake{}
		if r := run(t, f, clitest.Options{}, args...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Errorf("%v: %d %v", args, r.Code, f.Calls())
		}
	}
	f := &clitest.Fake{ArchiveErr: domain.ErrArchivesUnsupported}
	if r := run(t, f, clitest.Options{}, "put", "b.rmdoc", "--yes"); r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoArchives) {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// One archive of several failing: the others go on, and the command fails.
	f = &clitest.Fake{Imported: imported(), PutErr: map[string]error{"bad.rmdoc": errors.New("broken")}}
	r := run(t, f, clitest.Options{}, "put", "bad.rmdoc", "good.rmdoc", "--to", "/", "--yes")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, ErrSomeFailed) || !strings.Contains(r.Err, "bad.rmdoc") || !strings.Contains(r.Out, "imported\tNotes\t") {
		t.Fatalf("%d %v %q %q", r.Code, r.Error, r.Err, r.Out)
	}
	f = &clitest.Fake{Err: port.ErrNotRegistered}
	if r := run(t, f, clitest.Options{}, "put", "a.rmdoc", "b.rmdoc", "--to", "/", "--yes"); r.Code != cliutil.CodeNotRegistered {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	dry := &clitest.Fake{ArchiveErr: errors.New("not an archive")}
	if r := run(t, dry, clitest.Options{}, "put", "a.rmdoc", "--dry-run"); r.Code != cliutil.CodeError {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	none := cliutil.Services{}
	if r := clitest.Exec(t, none, clitest.Options{}, Commands, "put", "a.rmdoc", "--dry-run"); r.Code == cliutil.CodeOK {
		t.Fatal("a dry run ran without the writer")
	}
}
