// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func streams() (Streams, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return Streams{In: strings.NewReader(""), Out: &out, Err: &errOut}, &out, &errOut
}

func TestHelpListsTheGroups(t *testing.T) {
	t.Parallel()
	s, out, _ := streams()
	cmd := New((&clitest.Fake{}).Services(), s)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Account:", "Reading — these change nothing:", "Writing — these change the live library and ask first:", "Diagnostics:",
		"register", "tree", "mput", "debug", "watch", "completion", "--profile", "--json", "--yes",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRunDrivesAServiceAndCloses(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	closed := false
	factory := func(_ context.Context, profile string, verbose bool) (Services, func() error, error) {
		if profile != "work" || !verbose {
			t.Errorf("profile %q verbose %v", profile, verbose)
		}
		return f.Services(), func() error { closed = true; return nil }, nil
	}
	s, out, _ := streams()
	if err := Run(context.Background(), []string{"ls", "-P", "work", "-v"}, s, factory); err != nil {
		t.Fatal(err)
	}
	if !closed || out.String() != "Books\tfolder\tf-books\tfalse\tfalse\nNotes\tnotebook\td-notes\tfalse\tfalse\n" {
		t.Fatalf("closed %v out %q", closed, out.String())
	}
}

func TestRunReportsACloseFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("close")
	factory := func(context.Context, string, bool) (Services, func() error, error) {
		return (&clitest.Fake{}).Services(), func() error { return boom }, nil
	}
	s, _, _ := streams()
	if err := Run(context.Background(), []string{"ls"}, s, factory); !errors.Is(err, boom) {
		t.Fatal(err)
	}
}

func TestRunExitCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		fake *clitest.Fake
		want int
	}{
		{[]string{"ls"}, &clitest.Fake{}, cliutil.CodeOK},
		{[]string{"bogus"}, &clitest.Fake{}, cliutil.CodeUsage},
		{[]string{"ls", "--bogus"}, &clitest.Fake{}, cliutil.CodeUsage},
		{[]string{"mkdir", "/x"}, &clitest.Fake{}, cliutil.CodeUsage},
		{[]string{"ls"}, &clitest.Fake{Err: port.ErrNotRegistered}, cliutil.CodeNotRegistered},
		{[]string{"mkdir", "/x", "--yes"}, &clitest.Fake{Err: port.ErrRootConflict}, cliutil.CodeConflict},
		{[]string{"version"}, &clitest.Fake{}, cliutil.CodeOK},
	}
	for _, tc := range tests {
		s, _, _ := streams()
		err := Run(context.Background(), tc.args, s, cliutil.Fixed(tc.fake.Services()))
		if got := ExitCode(err); got != tc.want {
			t.Errorf("%v: exit %d, want %d (%v)", tc.args, got, tc.want, err)
		}
	}
}

func TestRunInterrupted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cliutil.ErrInterrupted)
	s, _, _ := streams()
	err := Run(ctx, []string{"watch"}, s, cliutil.Fixed((&clitest.Fake{}).Services()))
	if ExitCode(err) != cliutil.CodeInterrupted {
		t.Fatalf("%v", err)
	}
}

func TestCompletion(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		s, out, _ := streams()
		if err := Run(context.Background(), []string{"completion", shell}, s, cliutil.Fixed(Services{})); err != nil || out.Len() == 0 {
			t.Fatalf("%s: %v", shell, err)
		}
	}
	s, out, _ := streams()
	err := Run(context.Background(), []string{"__complete", "ls", "/B"}, s, cliutil.Fixed((&clitest.Fake{}).Services()))
	if err != nil || !strings.HasPrefix(out.String(), "/Books/\n") {
		t.Fatalf("%v %q", err, out.String())
	}
	s, out, _ = streams()
	err = Run(context.Background(), []string{"__complete", "put", "a.pdf", "/"}, s, cliutil.Fixed((&clitest.Fake{}).Services()))
	if err != nil || !strings.HasPrefix(out.String(), "/Books/\n") || strings.Contains(out.String(), "/Notes") {
		t.Fatalf("%v %q", err, out.String())
	}
	s, out, _ = streams()
	if err := Run(context.Background(), []string{"__complete", "put", ""}, s, cliutil.Fixed(Services{})); err != nil ||
		!strings.Contains(out.String(), "pdf\nepub\n") {
		t.Fatalf("%v %q", err, out.String())
	}
	s, out, _ = streams()
	if err := Run(context.Background(), []string{"__complete", "mput", ""}, s, cliutil.Fixed(Services{})); err != nil ||
		!strings.Contains(out.String(), ":16") {
		t.Fatalf("%v %q", err, out.String())
	}
	s, out, _ = streams()
	if err := Run(context.Background(), []string{"__complete", "mput", "d", "/"}, s, cliutil.Fixed((&clitest.Fake{}).Services())); err != nil ||
		!strings.HasPrefix(out.String(), "/Books/\n") {
		t.Fatalf("%v %q", err, out.String())
	}
}
