// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
)

var dirs = config.BaseDirs{Config: "/home/u/.config", Cache: "/home/u/.cache"}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(map[string]string{}, "", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != config.DefaultProfile ||
		cfg.ConfigDir != filepath.Join("/home/u/.config", "rmctl", "default") ||
		cfg.TokenPath != filepath.Join(cfg.ConfigDir, "token.json") ||
		cfg.CacheDir != filepath.Join("/home/u/.cache", "rmctl") {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Endpoints.SyncHost.Host == "" {
		t.Fatal("no default sync host")
	}
}

func TestLoadProfileAndOverrides(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		config.EnvProfile:   "work",
		config.EnvConfigDir: "/etc/rmctl",
		config.EnvCacheDir:  "/var/cache/rm",
		"RMCLOUD_HOST":      "https://cloud.example.com",
	}
	cfg, err := config.Load(env, "", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "work" || cfg.ConfigDir != filepath.Join("/etc/rmctl", "work") || cfg.CacheDir != "/var/cache/rm" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Endpoints.SyncHost.Host != "cloud.example.com" {
		t.Fatalf("hosts %+v", cfg.Endpoints)
	}
	flagged, err := config.Load(env, "home", dirs)
	if err != nil || flagged.Profile != "home" {
		t.Fatalf("the flag did not win: %+v %v", flagged, err)
	}
}

func TestLoadRefuses(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env     map[string]string
		profile string
		dirs    config.BaseDirs
		is      error
	}{
		"path in profile":     {profile: "../x", dirs: dirs, is: config.ErrProfile},
		"upper case profile":  {profile: "Work", dirs: dirs, is: config.ErrProfile},
		"long profile":        {profile: string(make([]byte, 65)), dirs: dirs, is: config.ErrProfile},
		"no config directory": {dirs: config.BaseDirs{Cache: "/c"}},
		"no cache directory":  {dirs: config.BaseDirs{Config: "/c"}},
		"plain http host":     {env: map[string]string{"RMCLOUD_HOST": "http://cloud.example.com"}, dirs: dirs},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := tc.env
			if env == nil {
				env = map[string]string{}
			}
			_, err := config.Load(env, tc.profile, tc.dirs)
			if err == nil {
				t.Fatal("accepted")
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestSystemDirsAndEnviron(t *testing.T) {
	t.Setenv("RMCTL_TEST_MARKER", "a=b")
	if got := config.Environ()["RMCTL_TEST_MARKER"]; got != "a=b" {
		t.Fatalf("Environ split the value: %q", got)
	}
	if _, err := config.SystemDirs(); err != nil {
		t.Fatal(err)
	}
}
