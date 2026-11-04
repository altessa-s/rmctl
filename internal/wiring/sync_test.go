// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
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
	for name, tc := range map[string]struct {
		mutate func(*wiring.Options)
		want   int64
	}{
		"the default":     {mutate: func(*wiring.Options) {}, want: wiring.DefaultCacheBytes},
		"the environment": {mutate: func(o *wiring.Options) { o.Env["RMCTL_CACHE_BUDGET"] = "2GiB" }, want: 2 << 30},
		"the file": {mutate: func(o *wiring.Options) {
			o.ReadConfig = func(string) ([]byte, bool, error) { return []byte("[cache]\nbudget = \"64MiB\"\n"), true, nil }
		}, want: 64 << 20},
		"the option wins": {mutate: func(o *wiring.Options) { o.Env["RMCTL_CACHE_BUDGET"] = "2GiB"; o.CacheBytes = 1 << 20 }, want: 1 << 20},
	} {
		opts := options(t)
		tc.mutate(&opts)
		svc, err := wiring.Build(opts)
		if err != nil {
			t.Fatal(err)
		}
		report, err := svc.Cache.Status(t.Context(), false)
		if err != nil || report.Profiles[0].Budget != tc.want {
			t.Errorf("%s: %+v %v", name, report, err)
		}
		_ = svc.Close()
	}
}
