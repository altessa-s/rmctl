// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want int
	}{
		{nil, CodeOK},
		{errors.New("x"), CodeError},
		{Usagef("bad %s", "args"), CodeUsage},
		{&ExitError{Code: 7}, 7},
		{fmt.Errorf("ls: %w", port.ErrNotRegistered), CodeNotRegistered},
		{fmt.Errorf("mkdir: %w", port.ErrRootConflict), CodeConflict},
		{ErrInterrupted, CodeInterrupted},
		{errors.New(`unknown command "x" for "rmctl"`), CodeUsage},
		{errors.New("unknown flag: --x"), CodeUsage},
		{errors.New("unknown shorthand flag: 'q' in -q"), CodeUsage},
		{errors.New("flag needs an argument: -o"), CodeUsage},
		{errors.New(`invalid argument "n" for "--jobs" flag`), CodeUsage},
		{errors.New(`required flag(s) "output" not set`), CodeUsage},
		{errors.New(`the required flag(s) note`), CodeError},
	}
	for _, tc := range tests {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
	if Usage(nil) != nil {
		t.Fatal("Usage(nil) is not nil")
	}
	wrapped := &ExitError{Code: 2, Err: port.ErrRootConflict}
	if !errors.Is(wrapped, port.ErrRootConflict) || wrapped.Error() != port.ErrRootConflict.Error() {
		t.Fatal("ExitError does not wrap")
	}
	if (&ExitError{Code: 5}).Error() != "exit status 5" {
		t.Fatal((&ExitError{Code: 5}).Error())
	}
}

func TestInterrupted(t *testing.T) {
	t.Parallel()
	if Interrupted(context.Background(), nil) != nil {
		t.Fatal("nil error changed")
	}
	plain := errors.New("x")
	if !errors.Is(Interrupted(context.Background(), plain), plain) {
		t.Fatal("unrelated error changed")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrInterrupted)
	if !errors.Is(Interrupted(ctx, context.Canceled), ErrInterrupted) {
		t.Fatal("interruption not named")
	}
}

func TestInterrupt(t *testing.T) {
	t.Parallel()
	sigs := make(chan os.Signal)
	var errOut syncBuffer
	var exited atomic.Int32
	exitCalled := make(chan struct{})
	ctx, stop := Interrupt(context.Background(), sigs, &errOut, func(code int) {
		exited.Store(int32(code)) //nolint:gosec // an exit code fits
		close(exitCalled)
	})
	sigs <- os.Interrupt
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), ErrInterrupted) || errOut.String() != InterruptNotice+"\n" {
		t.Fatalf("first signal: cause %v notice %q", context.Cause(ctx), errOut.String())
	}
	if exited.Load() != 0 {
		t.Fatal("exited on the first signal")
	}
	sigs <- os.Interrupt
	<-exitCalled
	if exited.Load() != CodeInterrupted {
		t.Fatalf("exit %d", exited.Load())
	}
	stop()
}

func TestInterruptStopWithoutSignal(t *testing.T) {
	t.Parallel()
	ctx, stop := Interrupt(context.Background(), make(chan os.Signal), &bytes.Buffer{}, func(int) { t.Error("exited") })
	stop()
	if ctx.Err() == nil || errors.Is(context.Cause(ctx), ErrInterrupted) {
		t.Fatalf("stop: %v", context.Cause(ctx))
	}
}

// syncBuffer is a buffer the interrupt goroutine and the test share.
type syncBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func (b *syncBuffer) lock() {
	if b.mu == nil {
		b.mu = make(chan struct{}, 1)
	}
	b.mu <- struct{}{}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.lock()
	defer func() { <-b.mu }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.lock()
	defer func() { <-b.mu }()
	return b.buf.String()
}

func TestSize(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB", 1 << 62: "4096.0 PiB"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestStampAndLines(t *testing.T) {
	t.Parallel()
	if Stamp(time.Time{}) != "" || Stamp(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) != "2026-01-02T03:04:05Z" {
		t.Fatal("stamp")
	}
	it := domain.NewItem(domain.Record{ID: "f"}, nil, mustMeta(t, `{"visibleName":"F","type":"CollectionType","pinned":true}`), nil)
	row := ItemRow(it, true, false)
	if name := row[0]; name.Text != "F" || !name.Has(output.Folder) || !name.Has(output.Pinned) || !name.Has(output.Trashed) {
		t.Fatalf("%+v", name)
	}
	if output.PlainValue(row[2]) != "f" || !row[3].Bool || !row[4].Bool {
		t.Fatalf("%+v", row)
	}
	v := View(nil, it)
	if v.Path != "" || v.Tags == nil || v.Kind != "folder" {
		t.Fatalf("%+v", v)
	}
}

func mustMeta(t *testing.T, s string) domain.Metadata {
	t.Helper()
	m, err := domain.ParseMetadata([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWriteJSONFailingWriter(t *testing.T) {
	t.Parallel()
	if WriteJSON(failWriter{}, 1) == nil {
		t.Fatal("unnoticed")
	}
	if WriteJSON(&failAfterOne{}, 1) == nil {
		t.Fatal("unnoticed newline")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

type failAfterOne struct{ done bool }

func (f *failAfterOne) Write(p []byte) (int, error) {
	if f.done {
		return 0, errors.New("closed")
	}
	f.done = true
	return len(p), nil
}

func TestProgress(t *testing.T) {
	t.Parallel()
	p, done := Progress(Streams{Err: &bytes.Buffer{}}, "x")
	if p != nil {
		t.Fatal("progress off a terminal")
	}
	done()
	var buf bytes.Buffer
	p, done = Progress(Streams{Err: &buf, IsTerminal: true}, "up")
	p(0, 200)
	p(1, 200) // same percent: not rewritten
	p(100, 200)
	p(300, 200) // past the total: capped
	p(2048, -1)
	done()
	if buf.String() != "\rup   0%\rup  50%\rup 100%\rup 2.0 KiB\n" {
		t.Fatalf("%q", buf.String())
	}
	var quiet bytes.Buffer
	_, done = Progress(Streams{Err: &quiet, IsTerminal: true}, "up")
	done()
	if quiet.Len() != 0 {
		t.Fatal("an unused progress line printed")
	}
}

func TestMaskName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                "",
		"Diary":           "<name:5>",
		"Plan.pdf":        "<name:4>.pdf",
		"тетрадь.epub":    "<name:7>.epub",
		"a.verylongext1":  "<name:14>",
		"v1.2 notes":      "<name:10>",
		".hidden":         "<name:7>",
		"report.tar-gz":   "<name:13>",
		"x.PDF":           "<name:1>.PDF",
		"name.":           "<name:5>",
		"two.dots.notes1": "<name:8>.notes1",
	} {
		if got := MaskName(in); got != want {
			t.Errorf("MaskName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskJSON(t *testing.T) {
	t.Parallel()
	in := `{"visibleName":"Secret.pdf","source":"https://x","tags":[{"name":"work","timestamp":1730000000000123}],` +
		`"nested":{"visibleName":"deep"},"parent":"p","size":12345678901234567890,"empty":""}`
	got := string(MaskJSON([]byte(in)))
	for _, want := range []string{
		`"visibleName": "<name:6>.pdf"`, `"source": "<name:9>"`, `"name": "<name:4>"`, `"timestamp": 1730000000000123`,
		`"visibleName": "<name:4>"`, `"parent": "p"`, `"size": 12345678901234567890`, `"empty": ""`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if string(MaskJSON([]byte("Blank\n"))) != "Blank\n" {
		t.Fatal("non-JSON changed")
	}
}

func FuzzMaskJSON(f *testing.F) {
	f.Add([]byte(`{"visibleName":"Secret.pdf","tags":[{"name":"a"}]}`))
	f.Add([]byte(`[1,"x",{"source":["s"]}]`))
	f.Add([]byte("not json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		out := MaskJSON(data)
		if bytes.Equal(out, data) {
			return
		}
		// What masking changed is JSON again, and stays JSON masked once more.
		if !json.Valid(out) || !json.Valid(MaskJSON(out)) {
			t.Fatalf("masked output is not JSON:\n%s", out)
		}
	})
}

func TestConfirm(t *testing.T) {
	t.Parallel()
	newCmd := func(yes bool) *cobra.Command {
		cmd := &cobra.Command{Use: "x"}
		AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
		args := []string{}
		if yes {
			args = append(args, "--yes")
		}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	env := NewEnv(Streams{In: strings.NewReader(""), Err: &bytes.Buffer{}}, nil)
	if Confirm(newCmd(true), env, "?") != nil {
		t.Fatal("--yes refused")
	}
	if ExitCode(Confirm(newCmd(false), env, "?")) != CodeUsage {
		t.Fatal("off a terminal without --yes")
	}
	tty := NewEnv(Streams{In: strings.NewReader(strings.Repeat("y", 300) + "\n"), Err: &bytes.Buffer{}, IsTerminal: true}, nil)
	if !errors.Is(Confirm(newCmd(false), tty, "?"), ErrDeclined) {
		t.Fatal("an overlong answer passed")
	}
}

func TestEnv(t *testing.T) {
	builds := 0
	closed := 0
	env := NewEnv(Streams{}, func(_ context.Context, req Request) (Services, func() error, error) {
		builds++
		if req.Profile != "work" || !req.Verbose {
			t.Errorf("%+v", req)
		}
		return Services{}, func() error { closed++; return nil }, nil
	})
	cmd := &cobra.Command{Use: "x"}
	AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
	t.Setenv(ProfileEnv, "work")
	if err := cmd.ParseFlags([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := env.Services(cmd); err != nil {
			t.Fatal(err)
		}
	}
	if builds != 1 {
		t.Fatalf("built %d times", builds)
	}
	if env.Close() != nil || env.Close() != nil || closed != 1 {
		t.Fatalf("closed %d times", closed)
	}
	if err := cmd.ParseFlags([]string{"--profile", "home"}); err != nil {
		t.Fatal(err)
	}
	if Profile(cmd) != "home" || ProfileName(cmd) != "home" {
		t.Fatal("--profile does not win over the environment")
	}
	t.Setenv(ProfileEnv, "")
	if bare := (&cobra.Command{}); ProfileName(bare) != DefaultProfile {
		t.Fatal("no profile is not the default one")
	}
	if NewEnv(Streams{}, nil).Close() != nil {
		t.Fatal("closing unbuilt services failed")
	}
}

func TestCandidates(t *testing.T) {
	t.Parallel()
	tree := testTree(t)
	tests := []struct {
		word        string
		foldersOnly bool
		want        []string
	}{
		{"d-", false, []string{"d-paper\tPaper"}},
		{"", true, []string{"f-books\tBooks"}},
		{"/", false, []string{"/Books/", "/Notes"}},
		{"/B", false, []string{"/Books/"}},
		{"/Books/", false, []string{"/Books/Paper"}},
		{"/Books/", true, nil},
		{"/Nowhere/", false, nil},
		{"/Notes/", false, nil},
	}
	for _, tc := range tests {
		got, _ := Candidates(tree, tc.word, tc.foldersOnly)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("Candidates(%q, %v) = %q, want %q", tc.word, tc.foldersOnly, got, tc.want)
		}
	}
}

func testTree(t *testing.T) *domain.Tree {
	t.Helper()
	item := func(id, name, typ, parent string, files ...domain.File) domain.Item {
		return domain.NewItem(domain.Record{ID: domain.ItemID(id)}, files,
			mustMeta(t, fmt.Sprintf(`{"visibleName":%q,"type":%q,"parent":%q}`, name, typ, parent)), nil)
	}
	return domain.NewTree(1, []domain.Item{
		item("f-books", "Books", domain.TypeCollection, ""),
		item("d-paper", "Paper", domain.TypeDocument, "f-books", domain.File{Name: "d-paper.pdf"}),
		item("n-notes", "Notes", domain.TypeDocument, ""),
		item("d-old", "Old", domain.TypeDocument, "trash"),
	})
}

func TestCompleteRemote(t *testing.T) {
	t.Parallel()
	tree := testTree(t)
	svc := Services{Browser: treeBrowser{tree: tree}}
	cmd := &cobra.Command{Use: "x"}
	AddPersistentFlags(cmd, NewEnv(Streams{}, Fixed(Services{})))
	got, dir := CompleteRemote(NewEnv(Streams{}, Fixed(svc)), false)(cmd, nil, "/B")
	if strings.Join(got, ",") != "/Books/" || dir&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Fatalf("%q %v", got, dir)
	}
	failing := Services{Browser: treeBrowser{err: errors.New("offline")}}
	if got, _ := CompleteRemote(NewEnv(Streams{}, Fixed(failing)), false)(cmd, nil, "/"); got != nil {
		t.Fatal(got)
	}
	if got, _ := CompleteRemote(NewEnv(Streams{}, Fixed(Services{})), false)(cmd, nil, "/"); got != nil {
		t.Fatal(got)
	}
}

type treeBrowser struct {
	port.Browser
	tree *domain.Tree
	err  error
}

func (b treeBrowser) Tree(context.Context) (*domain.Tree, error) { return b.tree, b.err }

func TestRenderTreeBelowAFolder(t *testing.T) {
	t.Parallel()
	tree := testTree(t)
	lines := BuildTree(tree, "f-books", false)
	if got := plainTree(t, lines); got != "Paper\tpdf\td-paper\n" || lines.Stranded != 0 {
		t.Fatalf("%q %+v", got, lines)
	}
	if nodes := TreeJSON(tree, "d-paper", false); len(nodes) != 0 || nodes == nil {
		t.Fatalf("%v", nodes)
	}
	all := BuildTree(tree, domain.RootID, true)
	if got := plainTree(t, all); !strings.Contains(got, "Old\tnotebook\td-old\n") || !all.Nodes[len(all.Nodes)-1].Cells[0].Has(output.Trashed) {
		t.Fatalf("%q", got)
	}
}

// plainTree is nodes as the plain renderer writes them.
func plainTree(t *testing.T, nodes TreeNodes) string {
	t.Helper()
	var b strings.Builder
	if err := (output.PlainRenderer{}).Render(&b, output.Result{Doc: output.Doc{output.Tree{Nodes: nodes.Nodes}}}, output.Style{}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func BenchmarkMaskJSON(b *testing.B) {
	data := []byte(`{"visibleName":"Quarterly report.pdf","type":"DocumentType","parent":"","lastModified":"1730000000000",` +
		`"tags":[{"name":"work","timestamp":1730000000000}],"source":"","version":3,"pinned":false}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = MaskJSON(data)
	}
}
