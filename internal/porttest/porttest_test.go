// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func TestCloudRootAndIndexes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := porttest.NewCloud()
	state, err := c.Root(ctx)
	if err != nil || state.Generation != 0 || len(state.Records) != 0 {
		t.Fatalf("%v %+v", err, state)
	}
	f := c.Put("a.pdf", []byte("x"))
	rec, err := c.PutIndex(ctx, "a", []domain.File{f})
	if err != nil || rec.Subfiles != 1 || rec.Size != 1 {
		t.Fatalf("%v %+v", err, rec)
	}
	if text := porttest.IndexText("a", []domain.File{f}); porttest.Hash([]byte(text)) != rec.Hash {
		t.Fatal("the index is not addressed by its text")
	}
	root, err := c.CommitRoot(ctx, state, []domain.Record{rec})
	if err != nil || root.Generation != 1 || c.Generation() != 1 || len(c.Records()) != 1 || len(root.Records) != 1 {
		t.Fatalf("%v %+v", err, root)
	}
	if now, _ := c.Root(ctx); now.Hash != root.Hash {
		t.Fatalf("the commit answered %s, the root is %s", root.Hash, now.Hash)
	}
	if byFile, err := c.IndexByFile(ctx, rec); err != nil || !byFile {
		t.Fatalf("%v %v", byFile, err)
	}
	node := c.Readdress(rec, porttest.Hash([]byte("node")))
	if byFile, err := c.IndexByFile(ctx, node); err != nil || byFile {
		t.Fatalf("a readdressed index reads as by file: %v %v", byFile, err)
	}
	if files, err := c.Files(ctx, node); err != nil || len(files) != 1 {
		t.Fatalf("the readdressed index: %v %v", files, err)
	}
	if _, err := c.IndexByFile(ctx, domain.Record{Hash: "nope"}); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.CommitRoot(ctx, state, nil); !errors.Is(err, port.ErrRootConflict) {
		t.Fatalf("a stale base committed: %v", err)
	}
	c.Conflicts = 1
	state, _ = c.Root(ctx)
	if _, err := c.CommitRoot(ctx, state, nil); !errors.Is(err, port.ErrRootConflict) {
		t.Fatalf("an injected conflict did not answer: %v", err)
	}
	files, err := c.Files(ctx, rec)
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", err, files)
	}
	if _, err := c.Files(ctx, domain.Record{Hash: "nope"}); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	if c.Count("CommitRoot") != 3 || len(c.Calls()) == 0 {
		t.Fatalf("calls %v", c.Calls())
	}
}

func TestCloudBlobs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := porttest.NewCloud()
	f, err := c.Upload(ctx, "n", []byte("body"))
	if err != nil || f.Size != 4 {
		t.Fatalf("%v %+v", err, f)
	}
	if b, err := c.Read(ctx, f); err != nil || string(b) != "body" {
		t.Fatalf("%v %q", err, b)
	}
	var buf bytes.Buffer
	var done int64
	if err := c.Download(ctx, f, &buf, func(d, _ int64) { done = d }); err != nil || buf.String() != "body" || done != 4 {
		t.Fatalf("%v %q", err, buf.String())
	}
	if err := c.Download(ctx, f, failWriter{}, nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if b, err := c.ReadRaw(ctx, f.Hash, "n"); err != nil || string(b) != "body" {
		t.Fatalf("%v %q", err, b)
	}
	missing, err := c.Missing(ctx, []string{f.Hash, "gone"})
	if err != nil || !slices.Equal(missing, []string{"gone"}) {
		t.Fatalf("%v %v", err, missing)
	}
	if b, ok := c.Blob(f.Hash); !ok || string(b) != "body" {
		t.Fatal("Blob")
	}
	c.DropBlob(f.Hash)
	if _, err := c.Read(ctx, f); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	if err := c.Download(ctx, f, io.Discard, nil); !errors.Is(err, porttest.ErrNoBlob) {
		t.Fatalf("got %v", err)
	}
	c.ReadLocal = func(string) ([]byte, error) { return []byte("local"), nil }
	f, err = c.UploadFile(ctx, "p", "p.pdf", func(d, _ int64) { done = d })
	if err != nil || f.Size != 5 || done != 5 {
		t.Fatalf("%v %+v", err, f)
	}
	c.ReadLocal = func(string) ([]byte, error) { return nil, errBoom }
	if _, err := c.UploadFile(ctx, "p", "p.pdf", nil); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c.ReadLocal = nil
	path := filepath.Join(t.TempDir(), "f")
	if _, err := c.UploadFile(ctx, path, "f", nil); err == nil {
		t.Fatal("a missing local file uploaded")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestCloudFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := porttest.NewCloud()
	for _, m := range []string{
		"Root", "CommitRoot", "Files", "PutIndex", "IndexByFile", "Read", "Download", "Upload", "UploadFile",
		"ReadRaw", "Missing", "IsRegistered", "Register", "Forget", "Revoke", "Plan", "Listen",
	} {
		c.Fail[m] = errBoom
	}
	f := domain.File{}
	errs := []error{
		func() error { _, err := c.Root(ctx); return err }(),
		func() error { _, err := c.CommitRoot(ctx, port.RootState{}, nil); return err }(),
		func() error { _, err := c.Files(ctx, domain.Record{}); return err }(),
		func() error { _, err := c.PutIndex(ctx, "x", nil); return err }(),
		func() error { _, err := c.IndexByFile(ctx, domain.Record{}); return err }(),
		func() error { _, err := c.Read(ctx, f); return err }(),
		c.Download(ctx, f, io.Discard, nil),
		func() error { _, err := c.Upload(ctx, "x", nil); return err }(),
		func() error { _, err := c.UploadFile(ctx, "x", "x", nil); return err }(),
		func() error { _, err := c.ReadRaw(ctx, "x", "x"); return err }(),
		func() error { _, err := c.Missing(ctx, nil); return err }(),
		func() error { _, err := c.IsRegistered(ctx); return err }(),
		c.Register(ctx, "x"),
		c.Forget(ctx),
		c.Revoke(ctx),
		func() error { _, err := c.Plan(ctx); return err }(),
		c.Listen(ctx, nil),
	}
	for i, err := range errs {
		if !errors.Is(err, errBoom) {
			t.Errorf("call %d: %v", i, err)
		}
	}
}

func TestCloudRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := porttest.NewCloud()
	c.ValidCode = "good"
	c.AccountPlan = port.Plan{Level: "connect"}
	if _, err := c.Plan(ctx); !errors.Is(err, port.ErrNotRegistered) {
		t.Fatalf("got %v", err)
	}
	if err := c.Revoke(ctx); !errors.Is(err, port.ErrNotRegistered) {
		t.Fatalf("got %v", err)
	}
	if err := c.Register(ctx, "bad"); !errors.Is(err, porttest.ErrInvalidCode) {
		t.Fatalf("got %v", err)
	}
	if err := c.Register(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IsRegistered(ctx); !ok {
		t.Fatal("not registered")
	}
	if p, err := c.Plan(ctx); err != nil || p.Level != "connect" {
		t.Fatalf("%v %+v", err, p)
	}
	if err := c.Revoke(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Forget(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCloudListen(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.Events = []port.Event{{Kind: port.EventConnected}}
	ctx, cancel := context.WithCancel(t.Context())
	n := 0
	err := c.Listen(ctx, func(port.Event) { n++; cancel() })
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
}

func TestBuilders(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	c := porttest.NewCloud()
	recs := c.Add(
		porttest.Doc{ID: "a", Name: "A", Ext: "epub", Tags: []string{"t"}},
		porttest.Doc{ID: "b", Name: "B", Deleted: true, Pinned: true},
		porttest.Doc{ID: "c", Name: "C", Template: true, Trashed: true},
		porttest.Doc{ID: "d", Name: "D", BadMetadata: true},
	)
	meta := func(rec domain.Record) map[string]any {
		files, _ := c.Files(ctx, rec)
		data, _ := c.Read(ctx, files[0])
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		return m
	}
	if m := meta(recs[1]); m["deleted"] != true || m["pinned"] != true {
		t.Fatalf("%v", m)
	}
	if m := meta(recs[2]); m["type"] != domain.TypeTemplate || m["parent"] != "trash" {
		t.Fatalf("%v", m)
	}
	bad, _ := c.Files(ctx, recs[3])
	data, _ := c.Read(ctx, bad[0])
	if _, err := domain.ParseMetadata(data); err == nil {
		t.Fatal("bad metadata decoded")
	}
	files, _ := c.Files(ctx, recs[0])
	content, _ := c.Read(ctx, files[1])
	if !strings.Contains(string(content), `"fileType":"epub"`) {
		t.Fatalf("%s", content)
	}
}

func TestMemFiles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	m := porttest.NewMemFiles()
	if err := m.MkdirAll(filepath.Join("a", "b")); err != nil {
		t.Fatal(err)
	}
	if _, isDir, ok, _ := m.Stat("a"); !isDir || !ok {
		t.Fatal("a is not a directory")
	}
	if err := m.Produce(ctx, "a", func(io.Writer) error { return nil }); !errors.Is(err, porttest.ErrIsDir) {
		t.Fatalf("got %v", err)
	}
	if err := m.Produce(ctx, "x", func(io.Writer) error { return errBoom }); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	if _, ok := m.File("x"); ok {
		t.Fatal("a failed produce left a file")
	}
	if err := m.Produce(ctx, filepath.Join("a", "f"), func(w io.Writer) error { _, err := w.Write([]byte("hello")); return err }); err != nil {
		t.Fatal(err)
	}
	if size, _, ok, _ := m.Stat(filepath.Join("a", "f")); !ok || size != 5 {
		t.Fatal("stat")
	}
	if r, err := m.Open(filepath.Join("a", "f")); err != nil {
		t.Fatal(err)
	} else if b, _ := io.ReadAll(r); string(b) != "hello" || r.Close() != nil {
		t.Fatalf("open %q", b)
	}
	if _, err := m.Open("none"); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatal(err)
	}
	if b, _ := m.ReadHead(filepath.Join("a", "f"), 2); string(b) != "he" {
		t.Fatalf("head %q", b)
	}
	if b, _ := m.ReadTail(filepath.Join("a", "f"), 3); string(b) != "llo" {
		t.Fatalf("tail %q", b)
	}
	if _, err := m.ReadHead("none", 1); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatal(err)
	}
	if _, err := m.ReadTail("none", 1); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatal(err)
	}
	m.Write("other", nil)
	if rels, _ := m.Walk("a"); !slices.Equal(rels, []string{"f"}) {
		t.Fatalf("walk %v", rels)
	}
	if err := m.Remove(filepath.Join("a", "f")); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("none"); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatal(err)
	}
	dir, remove, err := m.Stage()
	if err != nil {
		t.Fatal(err)
	}
	if _, isDir, ok, _ := m.Stat(dir); !isDir || !ok {
		t.Fatalf("%s is not a directory", dir)
	}
	m.Write(filepath.Join(dir, "0"), []byte("staged"))
	m.Write(dir+"-other", []byte("kept"))
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.File(filepath.Join(dir, "0")); ok {
		t.Fatal("the staging directory kept a file")
	}
	if _, ok := m.File(dir + "-other"); !ok {
		t.Fatal("removing the staging directory removed a neighbour")
	}
	if err := m.Remove(dir + "-other"); err != nil {
		t.Fatal(err)
	}
	m.Fail["Unstage"] = errBoom
	if second, remove, err := m.Stage(); err != nil || second == dir || !errors.Is(remove(), errBoom) {
		t.Fatalf("%s %v", second, err)
	}
	for _, method := range []string{"Produce", "Stat", "Open", "ReadHead", "ReadTail", "MkdirAll", "Walk", "Remove", "Stage"} {
		m.Fail[method] = errBoom
	}
	errs := []error{
		m.Produce(ctx, "y", nil),
		func() error { _, _, _, err := m.Stat("y"); return err }(),
		func() error { _, err := m.ReadHead("y", 1); return err }(),
		func() error { _, err := m.ReadTail("y", 1); return err }(),
		m.MkdirAll("y"),
		func() error { _, err := m.Walk("y"); return err }(),
		m.Remove("y"),
		func() error { _, _, err := m.Stage(); return err }(),
		func() error { _, err := m.Open("y"); return err }(),
	}
	for i, err := range errs {
		if !errors.Is(err, errBoom) {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if len(m.Paths()) != 1 {
		t.Fatalf("paths %v", m.Paths())
	}
}

func TestSmallDoubles(t *testing.T) {
	t.Parallel()
	ids := porttest.NewIDs("ab")
	a, b := ids.NewID(), ids.NewID()
	if a == b || !strings.HasPrefix(string(a), "ab000000-") || strings.Contains(string(a), " ") {
		t.Fatalf("%q %q", a, b)
	}
	clk := porttest.NewClock(porttest.Epoch)
	clk.Advance(time.Second)
	if !clk.Now().Equal(porttest.Epoch.Add(time.Second)) {
		t.Fatal("clock")
	}
	cache := porttest.NewMemCache()
	if _, ok := cache.Get("h"); ok {
		t.Fatal("empty cache hit")
	}
	cache.Put("h", []byte("x"))
	if b, ok := cache.Get("h"); !ok || string(b) != "x" || cache.Hits != 1 {
		t.Fatal("cache")
	}
	pdf := porttest.PDF{Counts: map[string]int{"a.pdf": 2}}
	if n, err := pdf.Pages("a.pdf"); err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	if _, err := pdf.Pages("b.pdf"); !errors.Is(err, porttest.ErrNotAPDF) {
		t.Fatal(err)
	}
	pdf.Err = errBoom
	if _, err := pdf.Pages("a.pdf"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}
