// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/wiring"
)

func TestBuildSyncer(t *testing.T) {
	t.Parallel()
	on := true
	for name, tc := range map[string]struct {
		mutate func(*wiring.Options)
		err    error
	}{
		"no cache": {mutate: func(o *wiring.Options) { o.NoCache = true }, err: port.ErrNoCacheToSync},
		"offline":  {mutate: func(o *wiring.Options) { o.Offline = &on }, err: port.ErrOffline},
	} {
		opts := options(t)
		tc.mutate(&opts)
		svc, err := wiring.Build(opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.Syncer.Sync(t.Context(), "/", port.SyncOptions{}); !errors.Is(err, tc.err) {
			t.Errorf("%s: %v", name, err)
		}
		_ = svc.Close()
	}
}

func TestBuildCacheBudget(t *testing.T) {
	t.Parallel()
	file := func(o *wiring.Options, toml string) {
		o.ReadConfig = func(string) ([]byte, bool, error) { return []byte(toml), true, nil }
	}
	for name, tc := range map[string]struct {
		mutate func(*wiring.Options)
		want   int64
		source string
	}{
		"the default":     {mutate: func(*wiring.Options) {}, want: wiring.DefaultCacheBytes, source: port.BudgetFromDefault},
		"the environment": {mutate: func(o *wiring.Options) { o.Env["RMCTL_CACHE_BUDGET"] = "2GiB" }, want: 2 << 30, source: port.BudgetFromEnv},
		"the file": {
			mutate: func(o *wiring.Options) { file(o, "[cache]\nbudget = \"64MiB\"\n") }, want: 64 << 20, source: port.BudgetFromConfig,
		},
		"the profile's table": {
			mutate: func(o *wiring.Options) {
				file(o, "[cache]\nbudget = \"64MiB\"\n\n[profiles.default.cache]\nbudget = \"128MiB\"\n")
			},
			want: 128 << 20, source: port.BudgetFromProfile,
		},
		"no limit": {mutate: func(o *wiring.Options) { file(o, "[cache]\nbudget = \"0\"\n") }, want: 0, source: port.BudgetFromConfig},
		"the option wins": {
			mutate: func(o *wiring.Options) { o.Env["RMCTL_CACHE_BUDGET"] = "2GiB"; o.CacheBytes = 1 << 20 }, want: 1 << 20,
		},
	} {
		opts := options(t)
		tc.mutate(&opts)
		svc, err := wiring.Build(opts)
		if err != nil {
			t.Fatal(err)
		}
		report, err := svc.Cache.Status(t.Context(), false)
		if err != nil || report.Disabled || report.Profiles[0].Budget != tc.want || report.Profiles[0].BudgetSource != tc.source {
			t.Errorf("%s: %+v %v", name, report, err)
		}
		_ = svc.Close()
	}
}

// Every profile's cache is bounded by its own budget, and the reports of
// every profile say each one's.
func TestBuildReportsEveryProfilesOwnBudget(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Profile = "work"
	opts.ReadConfig = func(string) ([]byte, bool, error) {
		return []byte("[cache]\nbudget = \"64MiB\"\n\n[profiles.work.cache]\nbudget = \"128MiB\"\n\n" +
			"[profiles.archive.cache]\nbudget = \"0\"\n"), true, nil
	}
	cacheDir := filepath.Join(opts.Dirs.Cache, "rmctl")
	for _, profile := range []string{"archive", "home"} {
		if err := os.MkdirAll(filepath.Join(cacheDir, profile, "state"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	type budget struct {
		bytes  int64
		source string
	}
	want := map[string]budget{
		"archive": {0, port.BudgetFromProfile},
		"home":    {64 << 20, port.BudgetFromConfig},
		"work":    {128 << 20, port.BudgetFromProfile},
	}
	report, err := svc.Cache.Status(t.Context(), true)
	if err != nil || len(report.Profiles) != len(want) {
		t.Fatalf("%+v %v", report, err)
	}
	for _, p := range report.Profiles {
		if got := (budget{p.Budget, p.BudgetSource}); got != want[p.Profile] {
			t.Errorf("debug cache status --all: %s: %+v, want %+v", p.Profile, got, want[p.Profile])
		}
	}
	list, err := svc.Profiles.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if w, ok := want[s.Name]; ok && (budget{s.CacheBudget, s.CacheBudgetSource}) != w {
			t.Errorf("profile ls: %s: %d %q, want %+v", s.Name, s.CacheBudget, s.CacheBudgetSource, w)
		}
	}
	for name, w := range want {
		info, err := svc.Profiles.Info(t.Context(), name, false)
		if err != nil || (budget{info.Cache.Budget, info.Cache.BudgetSource}) != w {
			t.Errorf("profile info %s: %+v %v, want %+v", name, info.Cache, err, w)
		}
	}
	// The environment bounds every profile.
	opts.Env["RMCTL_CACHE_BUDGET"] = "1GiB"
	env, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = env.Close() }()
	report, err = env.Cache.Status(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range report.Profiles {
		if p.Budget != 1<<30 || p.BudgetSource != port.BudgetFromEnv {
			t.Errorf("%s under the environment: %d %q", p.Profile, p.Budget, p.BudgetSource)
		}
	}
}
