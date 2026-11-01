// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func parsed(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "x"}
	AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestFlagsAndNeed(t *testing.T) {
	t.Parallel()
	env := NewEnv(Streams{Getenv: func(string) string { return "" }}, Fixed(Services{}))
	if !env.JSON(parsed(t, "--json")) || env.JSON(parsed(t)) || Yes(parsed(t)) {
		t.Fatal("flag readers")
	}
	cmd := parsed(t)
	browser := treeBrowser{}
	got, err := Need(cmd, NewEnv(Streams{}, Fixed(Services{Browser: browser})), func(s Services) port.Browser { return s.Browser })
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if _, err := Need(cmd, NewEnv(Streams{}, Fixed(Services{})), func(s Services) port.Browser { return s.Browser }); !errors.Is(err, ErrNoService) {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	failing := func(context.Context, string, bool) (Services, func() error, error) { return Services{}, nil, boom }
	if _, err := Need(cmd, NewEnv(Streams{}, failing), func(s Services) port.Browser { return s.Browser }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	_, closer, _ := Fixed(Services{})(context.Background(), "", false)
	if closer() != nil {
		t.Fatal("fixed closer failed")
	}
}

func TestArgsAndFlagError(t *testing.T) {
	t.Parallel()
	v := Args(cobra.ExactArgs(1))
	if v(nil, []string{"a"}) != nil || ExitCode(v(nil, nil)) != CodeUsage {
		t.Fatal("Args")
	}
	if ExitCode(FlagError(nil, errors.New("bad flag"))) != CodeUsage {
		t.Fatal("FlagError")
	}
}

func TestConfirmAnswers(t *testing.T) {
	t.Parallel()
	for answer, ok := range map[string]bool{"yes\n": true, " Y \n": true, "no\n": false, "": false} {
		var prompt bytes.Buffer
		env := NewEnv(Streams{In: strings.NewReader(answer), Err: &prompt, IsTerminal: true}, nil)
		err := Confirm(parsed(t), env, "Go?")
		if ok != (err == nil) || prompt.String() != "Go? [y/N] " {
			t.Fatalf("%q: %v %q", answer, err, prompt.String())
		}
	}
}

func TestViewsAndItemRows(t *testing.T) {
	t.Parallel()
	tree := testTree(t)
	views := Views(tree, tree.Items())
	if len(views) != 4 || views[1].Path != "/Books/Paper" || !views[3].Trashed {
		t.Fatalf("%+v", views)
	}
	it, _ := tree.Get("d-paper")
	if row := ItemRow(it, false, true); !row[2].Empty() || row[3].Int != 0 || row[4].Text != "d-paper" {
		t.Fatalf("%+v", row)
	}
	meta := mustMeta(t, `{"visibleName":"N","type":"DocumentType","lastModified":"1730000000000","createdTime":"1730000000000"}`)
	dated := domain.NewItem(domain.Record{ID: "n"}, []domain.File{{Name: "n.pdf", Size: 2048}}, meta, nil)
	if row := ItemRow(dated, false, true); !row[2].Time.Equal(time.UnixMilli(1730000000000)) || row[3].Int != 2048 {
		t.Fatalf("%+v", row)
	}
	folder, _ := tree.Get("f-books")
	if row := ItemRow(folder, false, true); !row[3].Empty() || !row[0].Has(output.Folder) {
		t.Fatalf("a folder has a size: %+v", row)
	}
	if v := View(nil, dated); v.Created == "" || v.Modified == "" {
		t.Fatalf("%+v", v)
	}
	var buf bytes.Buffer
	Notef(&buf, "%d left", 3)
	if buf.String() != "3 left\n" {
		t.Fatal(buf.String())
	}
}

func TestTreeLoopsAndStrays(t *testing.T) {
	t.Parallel()
	item := func(id, typ, parent string) domain.Item {
		return domain.NewItem(domain.Record{ID: domain.ItemID(id)}, nil,
			mustMeta(t, fmt.Sprintf(`{"visibleName":%q,"type":%q,"parent":%q}`, strings.ToUpper(id), typ, parent)), nil)
	}
	tree := domain.NewTree(1, []domain.Item{
		item("s", domain.TypeDocument, "gone"),
		item("x", domain.TypeCollection, "y"),
		item("y", domain.TypeCollection, "x"),
		item("t", domain.TypeCollection, "trash"),
		item("u", domain.TypeDocument, "t"),
	})
	lines := BuildTree(tree, domain.RootID, false)
	joined := plainTree(t, lines)
	if lines.Stranded != 2 || strings.Contains(joined, "\tt\n") || !strings.Contains(joined, "Y\tfolder\ty\n") {
		t.Fatalf("%q %+v", joined, lines)
	}
	nodes := TreeJSON(tree, domain.RootID, false)
	if len(nodes) != 2 || len(nodes[1].Children) != 1 {
		t.Fatalf("%+v", nodes)
	}
	if all := TreeJSON(tree, domain.RootID, true); len(all) != 3 {
		t.Fatalf("%+v", all)
	}
	if all := plainTree(t, BuildTree(tree, domain.RootID, true)); strings.Count(all, "\n") != 5 {
		t.Fatalf("%q", all)
	}
}

func TestMaskJSONKeepsWhatItCannotRewrite(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`"just a string"`, `123`, `null`, `[]`} {
		if out := MaskJSON([]byte(in)); !strings.Contains(string(out), strings.Trim(in, `"`)) {
			t.Fatalf("%s → %s", in, out)
		}
	}
}
