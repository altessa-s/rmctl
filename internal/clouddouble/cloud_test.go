// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble_test

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

func TestSeedLaysOutTheTabletFormat(t *testing.T) {
	t.Parallel()
	c := clouddouble.New(t)
	_, before := c.Root()
	c.Seed(t,
		clouddouble.Item{ID: "f", Name: "Folder", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "p", Name: "Paper", Parent: "f", Kind: clouddouble.PDF, Pages: 2, Tags: []string{"work"}, Pinned: true},
		clouddouble.Item{ID: "e", Name: "Book", Kind: clouddouble.EPUB, Body: []byte("PK\x03\x04 book")},
		clouddouble.Item{ID: "n", Name: "Notes", Kind: clouddouble.Notebook, Pages: 1, Minimal: true, RecordType: "0"},
		clouddouble.Item{ID: "t", Name: "Old", Kind: clouddouble.PDF, Trashed: true},
		clouddouble.Item{ID: "d", Name: "Flagged", Kind: clouddouble.Notebook, Deleted: true},
		clouddouble.Item{ID: "x", Name: "Template", Kind: clouddouble.Template},
		clouddouble.Item{ID: "r", Raw: []byte(`{"visibleName":"Raw","type":"DocumentType"}`), Kind: clouddouble.Notebook},
		clouddouble.Item{ID: "f", Name: "Again", Kind: clouddouble.Folder},
	)
	if _, after := c.Root(); after != before+1 {
		t.Fatalf("one seed is one root write: %d → %d", before, after)
	}
	records := c.Records(t)
	if len(records) != 9 || records[0].Type != "80000000" {
		t.Fatalf("%+v", records)
	}
	names := map[string]bool{}
	for _, f := range c.Files(t, "p") {
		names[f.DocumentID] = true
	}
	for _, want := range []string{"p.metadata", "p.content", "p.pdf", "p.pagedata", "p/page-0.rm", "p/page-1.rm"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	if m := c.Metadata(t, "p"); m["parent"] != "f" || m["pinned"] != true || m["type"] != "DocumentType" {
		t.Fatalf("pdf metadata %v", m)
	}
	if m := c.Metadata(t, "t"); m["parent"] != "trash" {
		t.Fatalf("trashed metadata %v", m)
	}
	if m := c.Metadata(t, "n"); m["version"] != nil || m["synced"] != nil {
		t.Fatalf("minimal metadata %v", m)
	}
	if m := c.Metadata(t, "x"); m["type"] != "TemplateType" {
		t.Fatalf("template metadata %v", m)
	}
	if body, ok := c.File(t, "e", "e.epub"); !ok || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("epub %q", body)
	}
	if content, ok := c.File(t, "f", "f.content"); !ok || string(content) != "{}" {
		t.Fatalf("folder content %q", content)
	}
	if c.Files(t, "missing") != nil || c.Metadata(t, "missing") != nil {
		t.Fatal("a missing item has files")
	}
	if c.BlobCount() == 0 {
		t.Fatal("no blobs")
	}
}

func TestProtocolEdges(t *testing.T) {
	t.Parallel()
	c := clouddouble.New(t)
	u := c.URL()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // a loopback test server
	t.Cleanup(client.CloseIdleConnections)
	do := func(method, path, body string, header map[string]string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, u.String()+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	token := "Bearer " + clouddouble.UserToken(t, "unknown")
	for name, tc := range map[string]struct {
		method, path, body string
		header             map[string]string
		want               int
	}{
		"unknown path":           {http.MethodGet, "/nowhere", "", nil, http.StatusNotFound},
		"sync without a token":   {http.MethodGet, "/sync/v4/root", "", nil, http.StatusUnauthorized},
		"socket without a token": {http.MethodGet, "/notifications/ws/json/1", "", map[string]string{"Authorization": token}, http.StatusUnauthorized},
		"user token, no device":  {http.MethodPost, "/token/json/2/user/new", "{}", nil, http.StatusUnauthorized},
		"bad register body":      {http.MethodPost, "/token/json/2/device/new", "{", nil, http.StatusBadRequest},
	} {
		if got := do(tc.method, tc.path, tc.body, tc.header); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	if c.Hits("GET /sync/v4/root") != 1 {
		t.Fatalf("hits %d", c.Hits("GET /sync/v4/root"))
	}
}

func TestSyncEdgesThroughTheClient(t *testing.T) {
	t.Parallel()
	c := clouddouble.New(t)
	cl := c.Client(t, c.Registered(t))
	ctx := t.Context()
	if _, err := cl.Upload(ctx, []byte("x"), "bad/name.docSchema"); err == nil {
		t.Fatal("an index name with a slash was stored")
	}
	if _, err := cl.PutRoot(ctx, strings.Repeat("0", 64), 1); err == nil {
		t.Fatal("a root pointing at nothing was accepted")
	}
	if _, err := cl.MissingFiles(ctx, []string{strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
	c.SetFault(func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusTeapot)
		return true
	})
	if _, err := cl.Blob(ctx, strings.Repeat("d", 64), "x"); err == nil {
		t.Fatal("the fault did not answer")
	}
	c.SetFault(nil)
	if _, err := cl.Blob(ctx, strings.Repeat("d", 64), "x"); err == nil || !rmcloud.IsKind(err, rmcloud.KindHTTP) {
		t.Fatalf("a missing blob: %v", err)
	}
	if n := c.Broadcast(clouddouble.SyncComplete("a", "b")); n != 0 {
		t.Fatalf("broadcast to %d sockets with none open", n)
	}
	if n := c.DropSockets(); n != 0 {
		t.Fatalf("dropped %d sockets with none open", n)
	}
	store := clouddouble.NewStore()
	if store.Has(rmcloud.SecretDeviceToken) {
		t.Fatal("a new store holds a token")
	}
}
