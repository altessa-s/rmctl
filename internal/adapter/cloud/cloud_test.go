// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/cloud"
	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestRootFilesRead(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	double.Seed(t,
		clouddouble.Item{ID: "folder-1", Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "doc-1", Name: "Paper", Parent: "folder-1", Kind: clouddouble.PDF, Body: []byte("%PDF-1.4 body"), Pages: 2},
	)
	state, err := c.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	hash, gen := double.Root()
	if state.Hash != hash || state.Generation != gen || len(state.Records) != 2 {
		t.Fatalf("%+v", state)
	}
	var doc domain.Record
	for _, r := range state.Records {
		if r.ID == "doc-1" {
			doc = r
		}
	}
	if doc.Subfiles != 6 || doc.Size <= 0 {
		t.Fatalf("record %+v", doc)
	}
	files, err := c.Files(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	if !slices.Contains(names, "doc-1.pdf") || !slices.Contains(names, "doc-1/page-1.rm") {
		t.Fatalf("files %v", names)
	}
	pdf := files[slices.Index(names, "doc-1.pdf")]
	body, err := c.Read(t.Context(), pdf)
	if err != nil || string(body) != "%PDF-1.4 body" {
		t.Fatalf("read %q %v", body, err)
	}
	raw, err := c.ReadRaw(t.Context(), pdf.Hash, pdf.Name)
	if err != nil || !bytes.Equal(raw, body) {
		t.Fatalf("raw %q %v", raw, err)
	}
	var buf bytes.Buffer
	var reported int64
	if err := c.Download(t.Context(), pdf, &buf, func(done, _ int64) { reported = done }); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "%PDF-1.4 body" || reported != int64(len(body)) {
		t.Fatalf("download %q, progress %d", buf.String(), reported)
	}
	if err := c.Download(t.Context(), domain.File{Name: "x.pdf", Hash: strings.Repeat("0", 64)}, &buf, nil); err == nil {
		t.Fatal("downloaded a missing blob")
	}
	if _, err := c.Files(t.Context(), domain.Record{ID: "gone", Hash: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("read the index of a missing record")
	}
}

func TestWriteAFolderAndCommit(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	ctx := t.Context()
	base, err := c.Root(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.Upload(ctx, "f-1.metadata", []byte(`{"type":"CollectionType","visibleName":"New"}`))
	if err != nil {
		t.Fatal(err)
	}
	content, err := c.Upload(ctx, "f-1.content", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.PutIndex(ctx, "f-1", []domain.File{meta, content})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != "f-1" || rec.Subfiles != 2 || rec.Size != meta.Size+content.Size {
		t.Fatalf("record %+v", rec)
	}
	root, err := c.CommitRoot(ctx, base, append(base.Records, rec))
	if err != nil {
		t.Fatal(err)
	}
	if hash, now := double.Root(); root.Generation != now || root.Generation != base.Generation+1 || root.Hash != hash ||
		len(root.Records) != 1 || root.Records[0] != rec {
		t.Fatalf("root %+v, double at %s %d", root, hash, now)
	}
	records := double.Records(t)
	if len(records) != 1 || records[0].DocumentID != "f-1" || records[0].Type != "0" || records[0].Hash != rec.Hash {
		t.Fatalf("root %+v", records)
	}
	if m := double.Metadata(t, "f-1"); m["visibleName"] != "New" {
		t.Fatalf("metadata %v", m)
	}
}

func TestCommitOnAMovedRootIsAConflict(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	base, err := c.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	double.Advance()
	_, err = c.CommitRoot(t.Context(), base, nil)
	if !errors.Is(err, port.ErrRootConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestNoRootYetIsAnEmptyLibrary(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	double.SetFault(func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/root") {
			http.NotFound(w, r)
			return true
		}
		return false
	})
	state, err := c.Root(t.Context())
	if err != nil || state.Generation != 0 || state.Hash != "" || len(state.Records) != 0 {
		t.Fatalf("%+v %v", state, err)
	}
	if hash, gen, err := c.Pointer(t.Context()); err != nil || hash != "" || gen != 0 {
		t.Fatalf("pointer: %q %d %v", hash, gen, err)
	}
}

func TestFailuresPassThrough(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	double.SetFault(func(w http.ResponseWriter, _ *http.Request) bool {
		http.Error(w, "no", http.StatusBadRequest)
		return true
	})
	ctx := t.Context()
	if _, err := c.Root(ctx); err == nil {
		t.Fatal("root")
	}
	if _, _, err := c.Pointer(ctx); err == nil {
		t.Fatal("pointer")
	}
	if _, err := c.Upload(ctx, "a.metadata", []byte("{}")); err == nil {
		t.Fatal("upload")
	}
	if _, err := c.PutIndex(ctx, "a", nil); err == nil {
		t.Fatal("put index")
	}
	if _, err := c.CommitRoot(ctx, port.RootState{}, nil); err == nil {
		t.Fatal("commit")
	}
	if _, err := c.Missing(ctx, []string{strings.Repeat("a", 64)}); err == nil {
		t.Fatal("missing")
	}
	path := filepath.Join(t.TempDir(), "f.pdf")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UploadFile(ctx, path, "a.pdf", nil); err == nil {
		t.Fatal("upload file")
	}
	if _, err := c.Plan(ctx); err != nil {
		t.Fatalf("the plan is read from the stored token: %v", err)
	}
}

func TestUploadFileAndMissing(t *testing.T) {
	t.Parallel()
	_, c := registered(t)
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 local"), 0o600); err != nil {
		t.Fatal(err)
	}
	var done int64
	f, err := c.UploadFile(ctx, path, "d.pdf", func(d, _ int64) { done = d })
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "d.pdf" || f.Size != 14 || done != 14 {
		t.Fatalf("%+v progress %d", f, done)
	}
	if _, err := c.UploadFile(ctx, filepath.Join(t.TempDir(), "none"), "n.pdf", nil); err == nil {
		t.Fatal("uploaded a missing file")
	}
	unknown := strings.Repeat("b", 64)
	missing, err := c.Missing(ctx, []string{f.Hash, unknown})
	if err != nil || !slices.Equal(missing, []string{unknown}) {
		t.Fatalf("%v %v", missing, err)
	}
	if none, err := c.Missing(ctx, nil); err != nil || none != nil {
		t.Fatalf("empty ask: %v %v", none, err)
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	double, c, _ := unregistered(t)
	ctx := t.Context()
	if ok, err := c.IsRegistered(ctx); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := c.Root(ctx); !errors.Is(err, port.ErrNotRegistered) {
		t.Fatalf("unregistered root: %v", err)
	}
	if err := c.Register(ctx, "zzzzzzzz"); err == nil {
		t.Fatal("a refused code registered")
	}
	if err := c.Register(ctx, clouddouble.ValidCode); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.IsRegistered(ctx); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	plan, err := c.Plan(ctx)
	if err != nil || plan.Level != "connect" || plan.SyncScope != "sync:full" {
		t.Fatalf("%+v %v", plan, err)
	}
	if err := c.Revoke(ctx); err != nil {
		t.Fatal(err)
	}
	if double.Revoked() != 1 {
		t.Fatalf("revoked %d times", double.Revoked())
	}
	if ok, _ := c.IsRegistered(ctx); !ok {
		t.Fatal("revoke dropped the stored tokens; only forget does")
	}
	if err := c.Forget(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IsRegistered(ctx); ok {
		t.Fatal("still registered after forget")
	}
	if plan, err := c.Plan(ctx); err == nil && plan.Level != "" {
		t.Fatalf("plan without a token: %+v", plan)
	}
}

func TestListen(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	ctx, cancel := context.WithCancel(t.Context())
	events := make(chan port.Event, 16)
	done := make(chan error, 1)
	go func() { done <- c.Listen(ctx, func(e port.Event) { events <- e }) }()
	double.WaitSocket(t)
	next := func(kind port.EventKind) port.Event {
		t.Helper()
		deadline := time.After(clouddouble.Slack)
		for {
			select {
			case e := <-events:
				if e.Kind == kind {
					return e
				}
			case <-deadline:
				t.Fatalf("no event of kind %d", kind)
			}
		}
	}
	next(port.EventConnected)
	double.Broadcast(clouddouble.SyncComplete("doc-9", "Notes"))
	ev := next(port.EventLibrary)
	if !ev.SyncComplete || ev.DocumentID != "doc-9" || ev.VisibleName != "Notes" || ev.Name != "SyncComplete" ||
		!strings.Contains(string(ev.Frame), `"attributes"`) {
		t.Fatalf("%+v", ev)
	}
	// A frame that does not parse is its own kind, carried whole: not an
	// empty library event.
	double.Broadcast("not json")
	if bad := next(port.EventUnreadable); string(bad.Frame) != "not json" || bad.Name != "" {
		t.Fatalf("%+v", bad)
	}
	double.DropSockets()
	if dropped := next(port.EventDropped); dropped.Err == nil {
		t.Fatalf("%+v", dropped)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(clouddouble.Slack):
		t.Fatal("Listen did not return after its context ended")
	}
}

func TestNewNeedsAClient(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("a nil client did not panic")
		}
	}()
	cloud.New(nil)
}

func TestIndexByFile(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	double.Seed(t,
		clouddouble.Item{ID: "by-file", Name: "File", Kind: clouddouble.PDF},
		clouddouble.Item{ID: "by-node", Name: "Node", Kind: clouddouble.PDF, ByNodeHash: true},
		clouddouble.Item{ID: "v3-file", Name: "Schema 3", Kind: clouddouble.Notebook, Pages: 2, IndexV3: true},
		clouddouble.Item{ID: "v3-node", Name: "Schema 3 node", Kind: clouddouble.Notebook, Pages: 1, IndexV3: true, ByNodeHash: true},
	)
	root, err := c.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.ItemID]bool{"by-file": true, "by-node": false, "v3-file": true, "v3-node": false}
	for _, rec := range root.Records {
		got, err := c.IndexByFile(t.Context(), rec)
		if err != nil || got != want[rec.ID] {
			t.Errorf("%s: %v %v", rec.ID, got, err)
		}
		// Either way the index still reads.
		if files, err := c.Files(t.Context(), rec); err != nil || len(files) == 0 {
			t.Errorf("%s: files %v %v", rec.ID, files, err)
		}
	}
}

func TestIndexByFileFails(t *testing.T) {
	t.Parallel()
	_, c, _ := unregistered(t)
	if _, err := c.IndexByFile(t.Context(), domain.Record{ID: "x", Hash: strings.Repeat("a", 64)}); !errors.Is(err, port.ErrNotRegistered) {
		t.Fatalf("err %v", err)
	}
}
