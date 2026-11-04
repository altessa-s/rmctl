// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
)

func TestParseSize(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int64{
		"512MiB":   512 << 20,
		"4GiB":     4 << 30,
		"4 gib":    4 << 30,
		"1.5GiB":   3 << 29,
		"100MB":    100_000_000,
		"2KB":      2000,
		"1TiB":     1 << 40,
		"1048576":  1 << 20,
		"1048576B": 1 << 20,
		" 64KiB ":  64 << 10,
	} {
		if got, ok := config.ParseSize(in); !ok || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "GiB", "4XB", "-1GiB", "1.2.3MiB", "4 G iB", "99999999999TiB", "lots"} {
		if got, ok := config.ParseSize(in); ok {
			t.Errorf("ParseSize(%q) = %d, accepted", in, got)
		}
	}
}

func TestCacheBudget(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	file := "[cache]\nbudget = \"2GiB\"\n\n[profiles.work.cache]\nbudget = \"4GiB\"\n"
	for _, tt := range []struct {
		name, profile, env string
		want               int64
	}{
		{name: "the profile's table", profile: "work", want: 4 << 30},
		{name: "the global table", profile: "home", want: 2 << 30},
		{name: "the environment wins", profile: "work", env: "768MiB", want: 768 << 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			if tt.env != "" {
				env[config.EnvCacheBudget] = tt.env
			}
			cfg, err := config.LoadFrom(env, tt.profile, dirs, files(map[string]string{path: file}))
			if err != nil || cfg.CacheBudget != tt.want {
				t.Fatalf("%d %v", cfg.CacheBudget, err)
			}
		})
	}
	if cfg, err := config.LoadFrom(nil, "", dirs, nil); err != nil || cfg.CacheBudget != 0 {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	for name, tc := range map[string]struct {
		file string
		env  map[string]string
	}{
		"a size that is not one":   {file: "[cache]\nbudget = \"big\"\n"},
		"zero":                     {file: "[cache]\nbudget = \"0\"\n"},
		"less than a mebibyte":     {file: "[profiles.work.cache]\nbudget = \"512KiB\"\n"},
		"a number, not a string":   {file: "[cache]\nbudget = 4\n"},
		"a bad environment":        {env: map[string]string{config.EnvCacheBudget: "0"}},
		"a bad table of the other": {file: "[profiles.home.cache]\nbudget = \"-1GiB\"\n"},
	} {
		_, err := config.LoadFrom(tc.env, "work", dirs, files(map[string]string{path: tc.file}))
		if !errors.Is(err, config.ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := config.LoadFrom(map[string]string{config.EnvCacheBudget: "0"}, "work", dirs, nil)
	if err == nil || !strings.Contains(err.Error(), `RMCTL_CACHE_BUDGET = "0": a budget is a size such as "512MiB" or "4GiB", at least 1MiB`) {
		t.Fatal(err)
	}
}
