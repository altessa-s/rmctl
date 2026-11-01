// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package clitest runs rmctl commands against fake services in tests: a
// library built from short descriptions, a Fake that implements every input
// port and records what it was asked, and Exec, which runs a command line and
// returns its streams and exit code.
package clitest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
)

// The sizes of the files Item invents.
const (
	pdfSize  = 2048
	epubSize = 4096
	pageSize = 100
)

// The identifiers of [Library].
const (
	BooksID = "f-books"
	NovelID = "d-novel"
	PaperID = "d-paper"
	NotesID = "d-notes"
	// NotesName is the name of the notebook the doubles answer with.
	NotesName = "Notes"
	OldID     = "d-old"
)

// Spec describes one item of a test library.
type Spec struct {
	ID, Name, Parent string
	Kind             domain.Kind
	Trashed, Pinned  bool
	Tags             []string
	Modified         time.Time
}

// Item builds the item a spec describes, through the domain's own decoding.
func Item(s Spec) domain.Item {
	meta := map[string]any{"visibleName": s.Name, "type": domain.TypeDocument, "parent": s.Parent, "pinned": s.Pinned}
	if s.Kind.IsFolder() {
		meta["type"] = domain.TypeCollection
	}
	if s.Trashed {
		meta["parent"] = string(domain.TrashID)
	}
	if !s.Modified.IsZero() {
		meta["lastModified"] = fmt.Sprint(s.Modified.UnixMilli())
	}
	data, err := json.Marshal(meta)
	if err != nil {
		panic(err)
	}
	md, err := domain.ParseMetadata(data)
	if err != nil {
		panic(err)
	}
	files := []domain.File{{Name: s.ID + ".metadata", Hash: "h-meta-" + s.ID, Size: int64(len(data))}}
	switch s.Kind {
	case domain.KindFolder:
	case domain.KindPDF:
		files = append(files, domain.File{Name: s.ID + ".pdf", Hash: "h-pdf-" + s.ID, Size: pdfSize})
	case domain.KindAnnotatedPDF:
		files = append(files,
			domain.File{Name: s.ID + ".pdf", Hash: "h-pdf-" + s.ID, Size: pdfSize},
			domain.File{Name: s.ID + "/p1.rm", Hash: "h-rm-" + s.ID, Size: pageSize})
	case domain.KindEPUB:
		files = append(files, domain.File{Name: s.ID + ".epub", Hash: "h-epub-" + s.ID, Size: epubSize})
	case domain.KindNotebook:
		files = append(files, domain.File{Name: s.ID + "/p1.rm", Hash: "h-rm-" + s.ID, Size: pageSize})
	}
	return domain.NewItem(domain.Record{ID: domain.ItemID(s.ID), Hash: "h-" + s.ID}, files, md, s.Tags)
}

// Tree builds a library of specs; a trashed spec keeps its own parent out of
// the trash only through Parent.
func Tree(specs ...Spec) *domain.Tree {
	items := make([]domain.Item, 0, len(specs))
	for _, s := range specs {
		items = append(items, Item(s))
	}
	return domain.NewTree(1, items)
}

// Library is a small library most tests share:
//
//	Books/            f-books
//	  Novel [epub]    d-novel  (pinned, tag fiction)
//	  Paper [pdf]     d-paper
//	Notes [notebook]  d-notes
//	Old [pdf]         d-old    (trashed)
func Library() *domain.Tree {
	mod := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	return Tree(
		Spec{ID: BooksID, Name: "Books", Kind: domain.KindFolder, Modified: mod},
		Spec{ID: NovelID, Name: "Novel", Parent: BooksID, Kind: domain.KindEPUB, Pinned: true, Tags: []string{"fiction"}, Modified: mod},
		Spec{ID: PaperID, Name: "Paper", Parent: BooksID, Kind: domain.KindPDF, Modified: mod},
		Spec{ID: NotesID, Name: NotesName, Kind: domain.KindNotebook},
		Spec{ID: OldID, Name: "Old", Kind: domain.KindPDF, Trashed: true},
	)
}

// Result is what a command line printed and how it ended.
type Result struct {
	Out, Err string
	Code     int
	Error    error
}

// Options shape a run.
type Options struct {
	// Stdin is the input stream's content; Terminal makes the run look like a
	// person at a terminal.
	Stdin    string
	Terminal bool
	// OutTerminal makes the output stream look like a terminal of Width
	// columns that shows color; Env is the whole environment the run sees.
	OutTerminal bool
	Width       int
	Env         map[string]string
	// Factory overrides the fixed services, to test a failing build.
	Factory cliutil.Factory
	// Context is the run's context; nil is a background context.
	Context context.Context //nolint:containedctx // a test option
}

// Exec runs args against a root carrying the persistent flags and the
// commands build returns, with svc behind them.
func Exec(t testing.TB, svc cliutil.Services, opts Options, build func(env *cliutil.Env) []*cobra.Command, args ...string) Result {
	t.Helper()
	var out, errOut bytes.Buffer
	streams := cliutil.Streams{
		In: strings.NewReader(opts.Stdin), Out: &out, Err: &errOut, IsTerminal: opts.Terminal,
		OutTerminal: opts.OutTerminal, OutColor: opts.OutTerminal, Width: opts.Width,
		Getenv: func(key string) string { return opts.Env[key] },
	}
	factory := opts.Factory
	if factory == nil {
		factory = cliutil.Fixed(svc)
	}
	env := cliutil.NewEnv(streams, factory)
	root := &cobra.Command{Use: "rmctl", SilenceUsage: true, SilenceErrors: true}
	root.SetIn(streams.In)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetFlagErrorFunc(cliutil.FlagError)
	cliutil.AddPersistentFlags(root, env)
	for _, c := range build(env) {
		if c.GroupID != "" && !root.ContainsGroup(c.GroupID) {
			root.AddGroup(&cobra.Group{ID: c.GroupID, Title: c.GroupID})
		}
		root.AddCommand(c)
	}
	root.SetArgs(args)
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	err := cliutil.Interrupted(ctx, root.ExecuteContext(ctx))
	if closeErr := env.Close(); err == nil {
		err = closeErr
	}
	return Result{Out: out.String(), Err: errOut.String(), Code: cliutil.ExitCode(err), Error: err}
}
