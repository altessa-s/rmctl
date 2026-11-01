// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
)

func TestEditCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args       []string
		call, want string
	}{
		{[]string{"pin", "d-paper", "/Books"}, "Pin true d-paper /Books", "pinned\tPaper\tpdf\t-\td-paper\tfalse\npinned\tBooks\tfolder\t-\tf-books\tfalse\n"},
		{[]string{"unpin", "d-novel"}, "Pin false d-novel", "unpinned\tNovel\tepub\tfiction\td-novel\ttrue\n"},
		{[]string{"tag", "add", "d-paper", "--tag", "work", "--tag", "2026"}, "Tag +work,2026 - d-paper", "tagged\tPaper\tpdf\t-\td-paper\tfalse\n"},
		{[]string{"tag", "rm", "d-novel", "--tag", "fiction"}, "Tag + -fiction d-novel", "untagged\tNovel\tepub\tfiction\td-novel\ttrue\n"},
		{[]string{"rename", "d-paper", "Paper 2026"}, "Rename d-paper Paper 2026", "renamed\tPaper 2026\tpdf\t-\td-paper\tfalse\n"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Parallel()
			f := &clitest.Fake{}
			r := run(t, f, clitest.Options{}, append(tc.args, "--yes")...)
			if calls := f.Calls(); r.Code != 0 || r.Out != tc.want || len(calls) == 0 || calls[len(calls)-1] != tc.call {
				t.Fatalf("code %d err %v out %q calls %v", r.Code, r.Error, r.Out, calls)
			}
		})
	}
}

func TestEditCommandsAskAndReportNoChange(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, clitest.Options{Terminal: true, Stdin: "n\n"}, "tag", "add", "d-paper", "d-novel", "--tag", "work")
	if !errors.Is(r.Error, cliutil.ErrDeclined) || !strings.Contains(r.Err, "Tag 2 items with work? [y/N]") || len(f.Calls()) != 0 {
		t.Fatalf("%v %q %v", r.Error, r.Err, f.Calls())
	}
	for args, question := range map[string]string{
		"pin d-paper":            "Pin d-paper?",
		"rename d-paper X":       `Rename d-paper to "X"?`,
		"tag rm d-paper --tag x": "Take x off d-paper?",
	} {
		r := run(t, &clitest.Fake{}, clitest.Options{Terminal: true, Stdin: "n\n"}, strings.Fields(args)...)
		if !strings.Contains(r.Err, question) {
			t.Errorf("%s: %q", args, r.Err)
		}
	}
	if r := run(t, &clitest.Fake{}, clitest.Options{}, "pin", "d-paper"); r.Code != cliutil.CodeUsage {
		t.Fatalf("no --yes off a terminal: %d", r.Code)
	}
	same := &clitest.Fake{Unchanged: true}
	r = run(t, same, clitest.Options{}, "pin", "d-paper", "--yes", "--json")
	if r.Code != 0 || !strings.Contains(r.Out, `"action": "unchanged"`) || !strings.Contains(r.Out, `"changed": false`) ||
		!strings.Contains(r.Err, "Paper is already as asked") {
		t.Fatalf("%d %q %q", r.Code, r.Out, r.Err)
	}
}

func TestEditCommandRefusals(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"tag", "add", "d-paper"},
		{"tag", "add", "d-paper", "--tag", " "},
		{"tag", "rm", "--tag", "x"},
		{"pin"},
		{"rename", "d-paper"},
		{"rename", "d-paper", "a", "b"},
	} {
		f := &clitest.Fake{}
		if r := run(t, f, clitest.Options{}, append(args, "--yes")...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Errorf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	if r := run(t, &clitest.Fake{}, clitest.Options{}, "tag"); r.Code != 0 || !strings.Contains(r.Out, "add") {
		t.Fatalf("tag alone: %q", r.Out)
	}
	boom := &clitest.Fake{Err: errors.New("boom")}
	for _, args := range [][]string{{"pin", "d-paper"}, {"tag", "add", "d-paper", "--tag", "x"}, {"rename", "d-paper", "X"}} {
		if r := run(t, boom, clitest.Options{}, append(args, "--yes")...); r.Code != cliutil.CodeError {
			t.Errorf("%v: %d", args, r.Code)
		}
	}
	if r := run(t, &clitest.Fake{}, clitest.Options{}, "rename", "/Nothing", "X", "--yes"); r.Code != cliutil.CodeError {
		t.Fatalf("%d", r.Code)
	}
}
