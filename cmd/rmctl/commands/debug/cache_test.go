// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func cacheFake() *clitest.Fake {
	confirmed := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	entries := func(blobs, indexes int) map[string]int {
		return map[string]int{"blobs": blobs, "indexes": indexes, "state": 1}
	}
	return &clitest.Fake{CacheAt: port.CacheReport{
		Dir: "/c/rmctl", Profile: "work", TTL: 10 * time.Minute,
		Profiles: []port.ProfileCache{
			{Profile: "home", Dir: "/c/rmctl/home", Bytes: 2048, Budget: 512 << 20, Entries: entries(4, 3)},
			{
				Profile: "work", Dir: "/c/rmctl/work", Bytes: 10 << 20, Budget: 512 << 20, Entries: entries(120, 61),
				RootHash: strings.Repeat("ab", 32), RootGeneration: 42, Confirmed: confirmed,
			},
		},
	}}
}

func TestCacheStatusGoldens(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"cache-status-table":     {"--format", "table", "--color", "never"},
		"cache-status-all-table": {"--all", "--format", "table", "--color", "never"},
		"cache-status-json":      {"--json"},
		"cache-status-all-json":  {"--all", "--json"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := run(t, cacheFake(), append([]string{"cache", "status"}, args...)...)
			if r.Code != 0 {
				t.Fatalf("%d %v", r.Code, r.Error)
			}
			clitest.Golden(t, name, r.Out)
		})
	}
	plain := run(t, cacheFake(), "cache", "status")
	if !strings.Contains(plain.Out, "profile\twork\n") || !strings.Contains(plain.Out, "root_generation\t42\n") || !strings.Contains(plain.Out, "ttl\t10m0s\n") {
		t.Fatalf("%q", plain.Out)
	}
}

func TestCacheClear(t *testing.T) {
	t.Parallel()
	f := cacheFake()
	r := clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "n\n"}, build, "debug", "cache", "clear", "--all")
	if !errors.Is(r.Error, cliutil.ErrDeclined) || !strings.Contains(r.Err, "Clear the caches of profiles home, work?") {
		t.Fatalf("%v %q", r.Error, r.Err)
	}
	if r := run(t, f, "cache", "clear"); r.Code != cliutil.CodeUsage {
		t.Fatalf("clear without --yes off a terminal: %d", r.Code)
	}
	r = clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "y\n"}, build, "debug", "cache", "clear")
	if r.Code != 0 || r.Out != "work\n" || !strings.Contains(r.Err, "Clear the cache of profile work?") {
		t.Fatalf("%d %v %q %q", r.Code, r.Error, r.Out, r.Err)
	}
	r = run(t, f, "cache", "clear", "--all", "--yes", "--json")
	if r.Code != 0 || r.Out != "{\n  \"cleared\": [\n    \"home\",\n    \"work\"\n  ]\n}\n" {
		t.Fatalf("%q", r.Out)
	}
	if calls := f.Calls(); !slices.Contains(calls, "CacheClear all") || !slices.Contains(calls, "CacheClear") {
		t.Fatalf("%v", calls)
	}
	bad := cacheFake()
	bad.CacheErr = errors.New("unreadable")
	for _, args := range [][]string{{"cache", "status"}, {"cache", "clear", "--yes"}} {
		if r := run(t, bad, args...); r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "unreadable") {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	if r := run(t, f, "cache"); r.Code != 0 || !strings.Contains(r.Out, "status") {
		t.Fatalf("cache alone: %q", r.Out)
	}
}
