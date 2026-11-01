// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/wiring"
)

func options(t *testing.T) wiring.Options {
	t.Helper()
	dir := t.TempDir()
	return wiring.Options{
		Env:    map[string]string{},
		Dirs:   config.BaseDirs{Config: filepath.Join(dir, "config"), Cache: filepath.Join(dir, "cache")},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()
	for _, cache := range []int64{0, -1, 1 << 20} {
		opts := options(t)
		opts.Profile = "work"
		opts.CacheBytes = cache
		svc, err := wiring.Build(opts)
		if err != nil {
			t.Fatal(err)
		}
		if svc.Account == nil || svc.Browser == nil || svc.Fetcher == nil || svc.Writer == nil || svc.Watcher == nil || svc.Inspector == nil || svc.Pages == nil ||
			svc.Archives == nil {
			t.Fatalf("cache %d: %+v", cache, svc)
		}
		if svc.Config.Profile != "work" || filepath.Base(svc.Config.TokenPath) != "token.json" {
			t.Fatalf("config %+v", svc.Config)
		}
		info, err := svc.Account.Info(t.Context())
		if err != nil || info.Registered {
			t.Fatalf("a fresh profile: %+v %v", info, err)
		}
		if err := svc.Close(); err != nil {
			t.Fatal(err)
		}
		if err := svc.Close(); err != nil {
			t.Fatal("a second Close failed:", err)
		}
	}
}

func TestBuildRefuses(t *testing.T) {
	t.Parallel()
	noLogger := options(t)
	noLogger.Logger = nil
	if _, err := wiring.Build(noLogger); !errors.Is(err, wiring.ErrNoLogger) {
		t.Fatalf("no logger: %v", err)
	}
	badProfile := options(t)
	badProfile.Profile = "../escape"
	if _, err := wiring.Build(badProfile); err == nil {
		t.Fatal("an unsafe profile was accepted")
	}
	badHost := options(t)
	badHost.Env = map[string]string{"RMCLOUD_HOST": "http://plain.example"}
	if _, err := wiring.Build(badHost); err == nil {
		t.Fatal("a plain-HTTP host was accepted")
	}
}

// The configuration file chooses the backend, the environment overrides
// it, and the secrets service reports the file in use without touching a
// Keychain.
func TestBuildChoosesTheSecretsBackend(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Profile = "work"
	opts.ReadConfig = func(string) ([]byte, bool, error) {
		return []byte("[profiles.work.secrets]\nbackend = \"keychain\"\n"), true, nil
	}
	svc, err := wiring.Build(opts)
	if err != nil || svc.Config.Secrets != "keychain" || svc.Config.SecretsSource != "profile" || svc.Secrets == nil {
		t.Fatalf("%+v %v", svc.Config, err)
	}
	_ = svc.Close()
	opts.Env = map[string]string{"RMCTL_SECRETS": "file"}
	svc, err = wiring.Build(opts)
	if err != nil || svc.Config.Secrets != "file" {
		t.Fatalf("%+v %v", svc.Config, err)
	}
	st, err := svc.Secrets.Status(t.Context())
	if err != nil || st.Backend != "file" || st.Source != "env" || len(st.Present) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	// The override reaches the use case, which refuses before any vault is read.
	if _, err := svc.Secrets.Migrate(t.Context(), port.SecretsKeychain, port.MigrateOptions{SetDefault: true}); !errors.Is(err, port.ErrOverridden) {
		t.Fatalf("err %v", err)
	}
	_ = svc.Close()
	opts.ReadConfig = func(string) ([]byte, bool, error) { return []byte("[secrets]\nbackend = 1\n"), true, nil }
	opts.Env = nil
	if _, err := wiring.Build(opts); err == nil {
		t.Fatal("an invalid configuration file was accepted")
	}
}
