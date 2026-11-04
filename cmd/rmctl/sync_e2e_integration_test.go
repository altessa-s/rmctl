//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// syncAnswer is the JSON answer of sync.
type syncAnswer struct {
	Current    bool   `json:"current"`
	Path       string `json:"path"`
	Documents  int    `json:"documents"`
	Fetched    int    `json:"fetched"`
	Cached     int    `json:"cached"`
	Bytes      int64  `json:"bytes_fetched"`
	Needed     int64  `json:"bytes_needed"`
	CacheBytes int64  `json:"cache_bytes"`
	Budget     int64  `json:"budget"`
	Source     string `json:"budget_source"`
}

func (e *e2e) sync(args ...string) (syncAnswer, map[string]int) {
	e.t.Helper()
	out, hits := e.cost(append([]string{"sync", "--json"}, args...)...)
	var a syncAnswer
	if err := json.Unmarshal([]byte(out), &a); err != nil {
		e.t.Fatalf("%v: %q", err, out)
	}
	return a, hits
}

// sync fills the cache; the next sync at the same root is one request; the
// originals it kept serve get and mget under --offline, and online without a
// download; a change costs only what changed.
func TestE2E_SyncThenOffline(t *testing.T) {
	e := newE2E(t)
	e.register()
	seedThree(t, e.cloud, "a")
	dir := t.TempDir()

	// Cold: an offline get names the file the cache lacks.
	e.ok("ls")
	code, _, errOut := e.rmctl("get", "/a Books/a Paper", "-o", dir+"/", "--offline")
	if code != 1 || !strings.Contains(errOut, "a-paper.pdf is not in the cache") {
		t.Fatalf("cold offline get: exit %d %q", code, errOut)
	}

	pages, _ := e.sync()
	if pages.Current || pages.Documents != 2 || pages.Fetched == 0 || pages.Budget != 512<<20 || pages.Source != "default" || pages.Path != "/" {
		t.Fatalf("sync: %+v", pages)
	}
	// Without --files the originals stay in the cloud.
	if code, _, errOut = e.rmctl("get", "/a Books/a Paper", "-o", dir+"/", "--offline"); code != 1 || !strings.Contains(errOut, "a-paper.pdf") {
		t.Fatalf("offline get after sync: exit %d %q", code, errOut)
	}
	if again, hits := e.sync(); !again.Current || total(hits) != 1 || hits["GET /sync/v4/root"] != 1 {
		t.Fatalf("a second sync: %+v %v", again, hits)
	}

	files, _ := e.sync("--files", "--jobs", "2")
	if files.Current || files.Fetched != 1 || files.Bytes != int64(len(clouddouble.MakePDF(1))) {
		t.Fatalf("sync --files: %+v", files)
	}
	if again, hits := e.sync("--files"); !again.Current || total(hits) != 1 {
		t.Fatalf("a second sync --files: %+v %v", again, hits)
	}
	// A narrower sync is covered by the whole library's.
	if narrow, hits := e.sync("/a Books"); !narrow.Current || total(hits) != 1 {
		t.Fatalf("a narrower sync: %+v %v", narrow, hits)
	}

	// Offline from the warm cache: no request at all.
	out, hits := e.cost("get", "/a Books/a Paper", "-o", dir+"/", "--offline")
	if len(hits) != 0 || strings.TrimSpace(out) != filepath.Join(dir, "a Paper.pdf") {
		t.Fatalf("offline get: %v %q", hits, out)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "a Paper.pdf")); err != nil || !bytes.Equal(data, clouddouble.MakePDF(1)) {
		t.Fatalf("the file got offline differs: %v", err)
	}
	mirror := filepath.Join(dir, "mirror")
	if _, hits = e.cost("mget", "/", "-o", mirror, "--offline"); len(hits) != 0 {
		t.Fatalf("offline mget: %v", hits)
	}
	if _, err := os.Stat(filepath.Join(mirror, "a Books", "a Paper.pdf")); err != nil {
		t.Fatal(err)
	}
	// Online, a cached original is not downloaded again: one request, the
	// root.
	if _, hits = e.cost("get", "/a Books/a Paper", "-o", filepath.Join(dir, "online.pdf")); total(hits) != 1 {
		t.Fatalf("an online get of a cached file: %v", hits)
	}

	// The cache report counts what sync kept.
	var status struct {
		Profiles []struct {
			Bytes   int64          `json:"bytes"`
			Entries map[string]int `json:"entries"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(e.ok("debug", "cache", "status", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Profiles) != 1 || status.Profiles[0].Bytes != files.CacheBytes || status.Profiles[0].Entries["blobs"] == 0 {
		t.Fatalf("status %+v, sync %+v", status, files)
	}

	// Another device adds a document: the next sync walks and fetches only
	// its original (its .metadata and .content come with the tree).
	added := clouddouble.MakePDF(3)
	e.cloud.Seed(t, clouddouble.Item{ID: "b-new", Name: "b New", Parent: "a-books", Kind: clouddouble.PDF, Body: added})
	changed, _ := e.sync("--files")
	if changed.Current || changed.Documents != 3 || changed.Fetched != 1 || changed.Bytes != int64(len(added)) {
		t.Fatalf("after a change: %+v (before %+v)", changed, files)
	}
	if again, hits := e.sync("--files"); !again.Current || total(hits) != 1 {
		t.Fatalf("after the change settled: %+v %v", again, hits)
	}
	// --force walks anyway, and fetches nothing.
	if forced, hits := e.sync("--files", "--force"); !forced.Current || forced.Fetched != 0 || total(hits) != 1 {
		t.Fatalf("--force: %+v %v", forced, hits)
	}
}

func TestE2E_SyncRefusals(t *testing.T) {
	e := newE2E(t)
	e.register()
	big := append(clouddouble.MakePDF(1), bytes.Repeat([]byte(" "), 2<<20)...)
	e.cloud.Seed(t, clouddouble.Item{ID: "big-0000", Name: "Big", Kind: clouddouble.PDF, Body: big})

	for _, args := range [][]string{{"sync", "--offline"}, {"sync", "--no-cache"}, {"sync", "/a", "/b"}} {
		if code, _, errOut := e.rmctl(args...); code != 2 {
			t.Fatalf("%v: exit %d %q", args, code, errOut)
		}
	}

	t.Setenv("RMCTL_CACHE_BUDGET", "1MiB")
	code, _, errOut := e.rmctl("sync", "--files")
	if code != 1 || !strings.Contains(errOut, "and the budget is 1.0 MiB; nothing was downloaded") || !strings.Contains(errOut, "RMCTL_CACHE_BUDGET") {
		t.Fatalf("over budget: exit %d %q", code, errOut)
	}
	// What fits still syncs: the refused sync was not recorded, and kept
	// no original.
	if a, _ := e.sync(); a.Documents != 1 || a.Budget != 1<<20 {
		t.Fatalf("%+v", a)
	}
	if code, _, errOut = e.rmctl("ls", "--offline"); code != 0 {
		t.Fatalf("offline after a sync: %d %q", code, errOut)
	}
	if code, _, errOut = e.rmctl("get", "/Big", "-o", t.TempDir()+"/", "--offline"); code != 1 || !strings.Contains(errOut, "big-0000.pdf is not in the cache") {
		t.Fatalf("a refused sync kept the original: exit %d %q", code, errOut)
	}
	t.Setenv("RMCTL_CACHE_BUDGET", "512KiB")
	if code, _, errOut = e.rmctl("ls"); code == 0 || !strings.Contains(errOut, "at least 1MiB") {
		t.Fatalf("a budget below the least: exit %d %q", code, errOut)
	}
	// A budget of 0 is no limit: what the small budget refused now syncs,
	// and every original stays.
	t.Setenv("RMCTL_CACHE_BUDGET", "0")
	a, _ := e.sync("--files")
	if a.Budget != 0 || a.Source != "env" || a.Fetched == 0 || a.Needed < 2<<20 {
		t.Fatalf("no limit: %+v", a)
	}
	if code, _, errOut = e.rmctl("get", "/Big", "-o", t.TempDir()+"/", "--offline"); code != 0 {
		t.Fatalf("no limit kept no original: exit %d %q", code, errOut)
	}
}
