//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// Completion through the whole tool: nothing before a command read the
// library, then the library as that command left it in the profile's
// cache, without a request to the cloud and without writing the cache.
func TestE2E_Completion(t *testing.T) {
	e := newE2E(t)
	if code, out, errOut := e.rmctl("__complete", "ls", "/"); code != 0 || out != ":4\n" || strings.Contains(errOut, "rmctl:") {
		t.Fatalf("before a read: %d %q %q", code, out, errOut)
	}
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000c1", Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-0000000000c1", Name: "Two words", Parent: "f0000000-0000-4000-8000-0000000000c1",
			Kind: clouddouble.PDF, Body: clouddouble.MakePDF(1)},
		clouddouble.Item{ID: "d0000000-0000-4000-8000-0000000000c2", Name: "Old", Kind: clouddouble.Notebook, Trashed: true},
	)
	e.ok("ls")
	cache := os.Getenv("RMCTL_CACHE_DIR")
	before := snapshot(t, cache)
	requests := hits(e.cloud)
	for args, want := range map[string]string{
		"ls /":              "/Books/\tfolder\n:6\n",
		"get /Books/T":      "/Books/Two words\tpdf\n:4\n",
		"restore /":         "/Old\tnotebook, in the trash\n:4\n",
		"mv /Books/T":       "/Books/Two words\tpdf\n:4\n",
		"--profile home ls": ":4\n",
	} {
		fields := strings.Fields(args)
		word := fields[len(fields)-1]
		if strings.HasSuffix(args, " ls") {
			word = ""
		} else {
			fields = fields[:len(fields)-1]
		}
		code, out, _ := e.rmctl(append(append([]string{"__complete"}, fields...), word)...)
		if code != 0 || out != want {
			t.Errorf("%s: %d %q", args, code, out)
		}
	}
	if n := hits(e.cloud); n != requests {
		t.Fatalf("completion made %d requests", n-requests)
	}
	if after := snapshot(t, cache); after != before {
		t.Fatalf("completion changed the cache:\n%s\n---\n%s", before, after)
	}
}

// snapshot lists every file under dir with its size and modification time.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(path + " " + info.ModTime().String() + " " + strconv.FormatInt(info.Size(), 10) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// hits counts every request the cloud served.
func hits(c *clouddouble.Cloud) int {
	n := 0
	for _, h := range c.AllHits() {
		n += h
	}
	return n
}
