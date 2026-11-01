//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// cost runs args and returns what they asked of the cloud, by request.
func (e *e2e) cost(args ...string) (string, map[string]int) {
	e.t.Helper()
	before := e.cloud.AllHits()
	out := e.ok(args...)
	diff := map[string]int{}
	for k, n := range e.cloud.AllHits() {
		if d := n - before[k]; d > 0 {
			diff[k] = d
		}
	}
	return out, diff
}

// total counts the requests to the sync service; a token renewal on the
// auth host is not the library's cost.
func total(hits map[string]int) int {
	n := 0
	for k, v := range hits {
		if strings.Contains(k, " /sync/") {
			n += v
		}
	}
	return n
}

func seedThree(t *testing.T, c *clouddouble.Cloud, prefix string) {
	t.Helper()
	c.Seed(t,
		clouddouble.Item{ID: prefix + "-books", Name: prefix + " Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: prefix + "-paper", Name: prefix + " Paper", Parent: prefix + "-books", Kind: clouddouble.PDF, Body: clouddouble.MakePDF(1)},
		clouddouble.Item{ID: prefix + "-notes", Name: prefix + " Notes", Kind: clouddouble.Notebook, Pages: 1},
	)
}

// A read asks the cloud for the root, then reads only what the cache lacks:
// a second ls is one request, and a changed library costs only its new
// indexes. --no-cache pays the whole library every time.
func TestE2E_CacheMakesASecondReadOneRequest(t *testing.T) {
	e := newE2E(t)
	e.register()
	seedThree(t, e.cloud, "a")

	_, uncached := e.cost("ls", "--no-cache")
	_, again := e.cost("ls", "--no-cache")
	_, first := e.cost("ls")
	out, second := e.cost("ls")
	t.Logf("ls --no-cache: %d requests %v; first cached ls: %d %v; second: %d %v",
		total(uncached), uncached, total(first), first, total(second), second)
	if total(again) != total(uncached) || total(uncached) < 5 {
		t.Fatalf("--no-cache: %v then %v", uncached, again)
	}
	if total(second) != 1 || len(second) != 1 || second["GET /sync/v4/root"] != 1 {
		t.Fatalf("a second ls asked %v", second)
	}
	if !strings.Contains(out, "a Books") {
		t.Fatalf("%q", out)
	}

	// Another device adds a document: the next read fetches the root index
	// and that document's index and files, nothing else of the library.
	seedThree(t, e.cloud, "b")
	_, changed := e.cost("ls")
	if changed["GET /sync/v4/root"] != 1 || total(changed) >= total(first)+total(uncached) {
		t.Fatalf("after a change: %v", changed)
	}
	_, settled := e.cost("tree")
	if total(settled) != 1 {
		t.Fatalf("a read after the change settled: %v", settled)
	}

	// A write always asks for the live root, and keeps the indexes it makes.
	e.ok("mkdir", "/New", "--yes")
	_, afterWrite := e.cost("ls")
	if total(afterWrite) != 1 {
		t.Fatalf("a read after a write: %v", afterWrite)
	}
}

// --offline makes no request at all, answers from the cache, and refuses
// what needs the cloud; the TTL lets reads skip even the root.
func TestE2E_OfflineAndTTL(t *testing.T) {
	e := newE2E(t)
	e.register()
	seedThree(t, e.cloud, "a")

	if code, _, errOut := e.rmctl("ls", "--offline"); code == 0 || !strings.Contains(errOut, "without --offline") {
		t.Fatalf("offline before any read: exit %d %q", code, errOut)
	}
	online := e.ok("tree")
	out, hits := e.cost("tree", "--offline")
	if out != online || len(hits) != 0 {
		t.Fatalf("offline tree: %v\n%s", hits, out)
	}
	if code, _, errOut := e.rmctl("mkdir", "/X", "--yes", "--offline"); code != 2 || !strings.Contains(errOut, "needs the cloud") {
		t.Fatalf("a write offline: exit %d %q", code, errOut)
	}
	if code, _, errOut := e.rmctl("get", "/a Books/a Paper", "--offline"); code != 2 || !strings.Contains(errOut, "needs the cloud") {
		t.Fatalf("get offline: exit %d %q", code, errOut)
	}
	if code, _, _ := e.rmctl("ls", "--offline", "--no-cache"); code != 2 {
		t.Fatalf("--offline --no-cache: exit %d", code)
	}

	t.Setenv("RMCTL_CACHE_TTL", "1h")
	if _, hits := e.cost("ls"); len(hits) != 0 {
		t.Fatalf("a read within the TTL asked %v", hits)
	}
	// A write ignores the TTL: its change lands on the live root.
	e.cloud.Advance()
	e.ok("mkdir", "/Y", "--yes")
	if _, ok := e.names()["Y"]; !ok {
		t.Fatal("the write did not land")
	}
}

// Every profile keeps its own cache: two accounts on two clouds each see
// only their own library, online and offline; logout clears only its
// profile; status and clear take --all.
func TestE2E_CachePerProfile(t *testing.T) {
	work := newE2E(t)
	home := &e2e{t: t, cloud: clouddouble.New(t)}
	work.ok("register", clouddouble.ValidCode, "-P", "work")
	home.ok("register", clouddouble.ValidCode, "-P", "home")
	seedThree(t, work.cloud, "work")
	seedThree(t, home.cloud, "home")

	for _, offline := range []string{"--offline=false", "--offline"} {
		w := work.ok("tree", "-P", "work", offline)
		h := home.ok("tree", "-P", "home", offline)
		if !strings.Contains(w, "work Books") || strings.Contains(w, "home") ||
			!strings.Contains(h, "home Books") || strings.Contains(h, "work") {
			t.Fatalf("%s:\nwork %s\nhome %s", offline, w, h)
		}
	}
	cacheDir := os.Getenv("RMCTL_CACHE_DIR")
	for _, p := range []string{"work", "home"} {
		info, err := os.Stat(filepath.Join(cacheDir, p, "indexes"))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v", p, err)
		}
	}

	var all struct {
		Profiles []struct {
			Profile string         `json:"profile"`
			Entries map[string]int `json:"entries"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(work.ok("debug", "cache", "status", "--all", "--json", "-P", "work")), &all); err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, p := range all.Profiles {
		names[p.Profile] = p.Entries["indexes"]
	}
	if len(names) != 2 || names["work"] == 0 || names["home"] == 0 {
		t.Fatalf("%+v", all)
	}

	// Logging out of home clears home's cache and nothing else.
	home.ok("logout", "-P", "home")
	if _, err := os.Stat(filepath.Join(cacheDir, "home", "indexes")); !os.IsNotExist(err) {
		t.Fatalf("home's cache outlived its logout: %v", err)
	}
	if out := work.ok("ls", "-P", "work", "--offline"); !strings.Contains(out, "work Books") {
		t.Fatalf("logout of home cleared work: %q", out)
	}

	home.ok("register", clouddouble.ValidCode, "-P", "home")
	home.ok("ls", "-P", "home")
	if code, _, errOut := work.rmctl("debug", "cache", "clear", "--all", "-P", "work"); code != 2 || !strings.Contains(errOut, "--yes") {
		t.Fatalf("clear without --yes: exit %d %q", code, errOut)
	}
	out := work.ok("debug", "cache", "clear", "--all", "--yes", "-P", "work")
	if lines := strings.Fields(out); !slices.Equal(lines, []string{"home", "work"}) {
		t.Fatalf("cleared %q", out)
	}
	if code, _, _ := work.rmctl("ls", "-P", "work", "--offline"); code == 0 {
		t.Fatal("an offline read after clear --all succeeded")
	}
}
