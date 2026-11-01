// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rmdoc

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func build(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{New(env)} }

func inspected() port.ArchiveReport {
	return port.ArchiveReport{Documents: []port.ArchiveDocumentReport{
		{ID: "n1", Name: "Secret plan", Kind: domain.KindNotebook, Entries: []domain.ArchiveEntry{
			{Name: "n1.metadata", Size: 100}, {Name: "n1/p1.rm", Size: 2048},
		}},
		{ID: "bad", Entries: []domain.ArchiveEntry{{Name: "bad.pdf", Size: 4}}},
	}}
}

func TestRmdoc(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Inspected: inspected()}
	r := clitest.Exec(t, f.Services(), clitest.Options{}, build, "rmdoc", "a.rmdoc")
	want := "path\ta.rmdoc\ndocuments\t2\n\n<name:11>\tnotebook\tn1\t2\t2148\n  n1.metadata\t100\n  n1/p1.rm\t2048\n" +
		"(no readable .metadata)\t-\tbad\t1\t4\n  bad.pdf\t4\n"
	if r.Code != cliutil.CodeOK || r.Out != want {
		t.Fatalf("%d %v\n%q", r.Code, r.Error, r.Out)
	}
	tbl := clitest.Exec(t, f.Services(), clitest.Options{OutTerminal: true}, build, "rmdoc", "a.rmdoc", "--color", "never")
	clitest.Golden(t, "rmdoc-table", tbl.Out)
	r = clitest.Exec(t, f.Services(), clitest.Options{}, build, "rmdoc", "a.rmdoc", "--raw", "--json")
	if r.Code != cliutil.CodeOK || !strings.Contains(r.Out, `"name": "Secret plan"`) {
		t.Fatalf("%d %v\n%s", r.Code, r.Error, r.Out)
	}
	clitest.Golden(t, "rmdoc", r.Out)
}

func TestRmdocErrors(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{ArchiveErr: domain.ErrArchivesUnsupported}
	r := clitest.Exec(t, f.Services(), clitest.Options{}, build, "rmdoc", "a.rmdoc")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, cliutil.ErrNoArchives) {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r := clitest.Exec(t, f.Services(), clitest.Options{}, build, "rmdoc"); r.Code != cliutil.CodeUsage {
		t.Fatalf("no file: %d", r.Code)
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, "rmdoc", "a.rmdoc"); r.Code == cliutil.CodeOK {
		t.Fatal("ran without the archives service")
	}
}
