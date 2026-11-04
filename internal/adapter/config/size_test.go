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
	const (
		global   = "[cache]\nbudget = \"2GiB\"\n"
		override = "[profiles.work.cache]\nbudget = \"4GiB\"\n"
		nolimit  = "[profiles.archive.cache]\nbudget = \"0\"\n"
	)
	for _, tt := range []struct {
		name, file, profile, env string
		want                     config.Budget
	}{
		{name: "nothing set", profile: "work", want: config.Budget{Bytes: config.DefaultCacheBudget, Source: config.SourceDefault}},
		{name: "the global table only", file: global, profile: "work", want: config.Budget{Bytes: 2 << 30, Source: config.SourceConfig}},
		{name: "the profile's table only", file: override, profile: "work", want: config.Budget{Bytes: 4 << 30, Source: config.SourceProfile}},
		{name: "another profile's table", file: override, profile: "home", want: config.Budget{Bytes: config.DefaultCacheBudget, Source: config.SourceDefault}},
		{name: "the profile's table over the global", file: global + "\n" + override, profile: "work", want: config.Budget{Bytes: 4 << 30, Source: config.SourceProfile}},
		{name: "the global table for the others", file: global + "\n" + override, profile: "home", want: config.Budget{Bytes: 2 << 30, Source: config.SourceConfig}},
		{name: "the environment over both", file: global + "\n" + override, profile: "work", env: "768MiB", want: config.Budget{Bytes: 768 << 20, Source: config.SourceEnv}},
		{name: "the environment for every profile", file: override, profile: "home", env: "768MiB", want: config.Budget{Bytes: 768 << 20, Source: config.SourceEnv}},
		{name: "no limit in the profile's table", file: global + "\n" + nolimit, profile: "archive", want: config.Budget{Source: config.SourceProfile}},
		{name: "no limit globally", file: "[cache]\nbudget = \"0GiB\"\n", profile: "work", want: config.Budget{Source: config.SourceConfig}},
		{name: "no limit from the environment", file: override, profile: "work", env: "0", want: config.Budget{Source: config.SourceEnv}},
		{name: "a limit from the environment over no limit", file: nolimit, profile: "archive", env: "1MiB", want: config.Budget{Bytes: 1 << 20, Source: config.SourceEnv}},
		{name: "the smallest budget", file: "[cache]\nbudget = \"1MiB\"\n", profile: "work", want: config.Budget{Bytes: 1 << 20, Source: config.SourceConfig}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			if tt.env != "" {
				env[config.EnvCacheBudget] = tt.env
			}
			cfg, err := config.LoadFrom(env, tt.profile, dirs, files(map[string]string{path: tt.file}))
			if err != nil || cfg.CacheBudget != tt.want || cfg.CacheBudgets.Of(tt.profile) != tt.want {
				t.Fatalf("%+v %v, want %+v", cfg.CacheBudget, err, tt.want)
			}
			if cfg.CacheBudget.Unlimited() != (tt.want.Bytes == 0) {
				t.Fatalf("unlimited %v", cfg.CacheBudget.Unlimited())
			}
		})
	}
}

// One configuration answers every profile's budget, as the reports of
// every profile need.
func TestCacheBudgetsOfEveryProfile(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	file := "[cache]\nbudget = \"2GiB\"\n\n[profiles.work.cache]\nbudget = \"4GiB\"\n\n[profiles.archive.cache]\nbudget = \"0\"\n"
	cfg, err := config.LoadFrom(nil, "home", dirs, files(map[string]string{path: file}))
	if err != nil {
		t.Fatal(err)
	}
	for profile, want := range map[string]config.Budget{
		"home":    {Bytes: 2 << 30, Source: config.SourceConfig},
		"work":    {Bytes: 4 << 30, Source: config.SourceProfile},
		"archive": {Source: config.SourceProfile},
		"other":   {Bytes: 2 << 30, Source: config.SourceConfig},
	} {
		if got := cfg.CacheBudgets.Of(profile); got != want {
			t.Errorf("%s: %+v, want %+v", profile, got, want)
		}
	}
	if got := (config.Budgets{}).Of("work"); got != (config.Budget{Bytes: config.DefaultCacheBudget, Source: config.SourceDefault}) {
		t.Fatalf("no configuration: %+v", got)
	}
}

func TestCacheBudgetRefused(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	for name, tc := range map[string]struct {
		file string
		env  map[string]string
	}{
		"a size that is not one":    {file: "[cache]\nbudget = \"big\"\n"},
		"less than a mebibyte":      {file: "[profiles.work.cache]\nbudget = \"512KiB\"\n"},
		"a byte":                    {file: "[cache]\nbudget = \"1\"\n"},
		"half a byte, not 0":        {file: "[cache]\nbudget = \"0.5\"\n"},
		"a number, not a string":    {file: "[cache]\nbudget = 4\n"},
		"zero as a number":          {file: "[cache]\nbudget = 0\n"},
		"negative":                  {file: "[cache]\nbudget = \"-1GiB\"\n"},
		"a bad environment":         {env: map[string]string{config.EnvCacheBudget: "1KiB"}},
		"a bad table of the other":  {file: "[profiles.home.cache]\nbudget = \"-1GiB\"\n"},
		"a word in the environment": {env: map[string]string{config.EnvCacheBudget: "unlimited"}},
	} {
		_, err := config.LoadFrom(tc.env, "work", dirs, files(map[string]string{path: tc.file}))
		if !errors.Is(err, config.ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := config.LoadFrom(map[string]string{config.EnvCacheBudget: "512KiB"}, "work", dirs, nil)
	if err == nil || !strings.Contains(err.Error(),
		`RMCTL_CACHE_BUDGET = "512KiB": a budget is a size of at least 1MiB, such as "512MiB" or "4GiB", or 0 for no limit`) {
		t.Fatal(err)
	}
}
