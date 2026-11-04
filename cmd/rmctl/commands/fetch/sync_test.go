// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

// synced is a sync that fetched three files of five.
var synced = port.SyncReport{ //nolint:gochecknoglobals // a test answer
	Path: "/Books", Root: "abc123", Generation: 42, Documents: 2, Fetched: 3, Cached: 2,
	Bytes: 3 << 20, Needed: 4 << 20, CacheBytes: 5 << 20, Budget: 512 << 20, BudgetSource: port.BudgetFromDefault,
}

func TestSync(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Synced: synced}
	r := run(t, f, clitest.Options{}, "sync")
	if r.Code != 0 || f.Calls()[0] != "Sync / jobs=0" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "sync", r.Out)
	f = &clitest.Fake{Synced: synced}
	r = run(t, f, clitest.Options{}, "sync", "/Books", "--files", "--all", "--force", "-j", "8", "--json")
	if r.Code != 0 || f.Calls()[0] != "Sync /Books files all force jobs=8" {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
	clitest.Golden(t, "sync-json", r.Out)
	current := synced
	current.Current, current.Fetched, current.Bytes = true, 0, 0
	r = run(t, &clitest.Fake{Synced: current}, clitest.Options{}, "sync")
	if !strings.HasPrefix(r.Out, "current\tyes\n") && !strings.HasPrefix(r.Out, "current\ttrue\n") {
		t.Fatalf("%q", r.Out)
	}
	// A budget of 0 is no limit: a person reads "unlimited", --json 0.
	unlimited := synced
	unlimited.Budget, unlimited.BudgetSource = 0, port.BudgetFromProfile
	r = run(t, &clitest.Fake{Synced: unlimited}, clitest.Options{}, "sync")
	if r.Code != 0 || !strings.Contains(r.Out, "budget\tunlimited\nbudget_source\tprofile\n") {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	r = run(t, &clitest.Fake{Synced: unlimited}, clitest.Options{}, "sync", "--json")
	if !strings.Contains(r.Out, `"budget": 0,`) || !strings.Contains(r.Out, `"budget_source": "profile"`) {
		t.Fatalf("%q", r.Out)
	}
}

func TestSyncUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"sync", "/a", "/b"},
		{"sync", "--jobs", "-1"},
		{"sync", "--jobs", "many"},
		{"sync", "--no-cache"},
	} {
		f := &clitest.Fake{}
		if r := run(t, f, clitest.Options{}, args...); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
			t.Errorf("%v: %d %v %v", args, r.Code, r.Error, f.Calls())
		}
	}
	// sync asks the cloud whether the cache is current: never offline.
	f := &clitest.Fake{Offline: true}
	r := run(t, f, clitest.Options{}, "sync", "--offline")
	if r.Code != cliutil.CodeUsage || !strings.Contains(r.Error.Error(), "needs the cloud") || len(f.Calls()) != 0 {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestSyncErrors(t *testing.T) {
	t.Parallel()
	// The refusal names the setting that chose the profile's budget.
	const head = "sync /Books: it needs 700.0 MiB in the cache of profile work, and the budget is 512.0 MiB; nothing was downloaded. "
	for source, advice := range map[string]string{
		port.BudgetFromEnv:     "Raise RMCTL_CACHE_BUDGET, which bounds every profile, to at least 701MiB (0 is no limit)",
		port.BudgetFromProfile: "Raise the budget to at least 701MiB with budget under [profiles.work.cache] in config.toml (0 is no limit)",
		port.BudgetFromConfig: "Raise budget under [cache] in config.toml to at least 701MiB for every profile, " +
			"or set it under [profiles.work.cache] for this one alone (0 is no limit)",
		port.BudgetFromDefault: "Raise the budget to at least 701MiB with budget under [profiles.work.cache] in config.toml (0 is no limit)",
	} {
		over := &port.OverBudgetError{Needed: 700<<20 + 1, Budget: 512 << 20, Source: source}
		r := run(t, &clitest.Fake{Err: over}, clitest.Options{}, "sync", "/Books", "-P", "work")
		want := head + advice + ", or sync a smaller path"
		if r.Code != cliutil.CodeError || r.Error.Error() != want {
			t.Errorf("%s: %d\n%v\n%s", source, r.Code, r.Error, want)
		}
	}
	// A sync that stops says what it kept.
	r := run(t, &clitest.Fake{Err: context.Canceled, Synced: port.SyncReport{Fetched: 4, Bytes: 2048}}, clitest.Options{}, "sync")
	if r.Code != cliutil.CodeError || !errors.Is(r.Error, context.Canceled) ||
		r.Err != "4 files (2.0 KiB) fetched before the sync stopped stay in the cache\n" {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
}

func TestSyncProgressOnlyOnATerminal(t *testing.T) {
	t.Parallel()
	steps := []port.SyncProgress{
		{FilesTotal: 2, BytesTotal: 2048},
		{Files: 1, FilesTotal: 2, Bytes: 1024, BytesTotal: 2048},
		{Files: 2, FilesTotal: 2, Bytes: 2048, BytesTotal: 2048},
	}
	if r := run(t, &clitest.Fake{Synced: synced, SyncSteps: steps}, clitest.Options{}, "sync"); r.Err != "" {
		t.Fatalf("progress off a terminal: %q", r.Err)
	}
	r := run(t, &clitest.Fake{Synced: synced, SyncSteps: steps}, clitest.Options{Terminal: true}, "sync")
	// The first drawing, then the finished line; the one between comes
	// within a tenth of a second and is skipped.
	if !strings.HasPrefix(r.Err, "\r\x1b[Ksyncing [>                   ]   0%  0 B/2.0 KiB  0/2 files") ||
		!strings.Contains(r.Err, "\r\x1b[Ksyncing [====================] 100%  2.0 KiB/2.0 KiB  2/2 files") || !strings.HasSuffix(r.Err, "\n") {
		t.Fatalf("%q", r.Err)
	}
	r = run(t, &clitest.Fake{Synced: synced, SyncSteps: steps}, clitest.Options{Terminal: true, Env: map[string]string{"TERM": "dumb"}}, "sync")
	if strings.Contains(r.Err, "\x1b") || !strings.HasPrefix(r.Err, "\rsyncing [") {
		t.Fatalf("escape sequences on a dumb terminal: %q", r.Err)
	}
}

func TestReadsRunOffline(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"get", "x"},
		{"export", "x", "-o", "y"},
		{"mget", "x", "-o", "y"},
		{"backup", "dir"},
	} {
		f := &clitest.Fake{Offline: true}
		if r := run(t, f, clitest.Options{}, append(args, "--offline")...); r.Code != 0 || len(f.Calls()) != 1 {
			t.Errorf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	// backup restore writes: it still needs the cloud.
	f := &clitest.Fake{Offline: true}
	if r := run(t, f, clitest.Options{}, "backup", "restore", "dir", "--offline", "--yes"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}
