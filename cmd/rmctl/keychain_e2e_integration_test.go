//go:build integration && darwin && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// The whole round trip through the real login Keychain: register into the
// file, move to the Keychain recording the choice, work from there, and
// move back. Skipped unless RMCTL_KEYCHAIN_TESTS=1; the items go under a
// service of the test's own and are removed at the end.
func TestE2E_KeychainRoundTrip(t *testing.T) {
	if os.Getenv("RMCTL_KEYCHAIN_TESTS") != "1" {
		t.Skip("set RMCTL_KEYCHAIN_TESTS=1 to use the login Keychain")
	}
	e := newE2E(t)
	t.Setenv("RMCTL_KEYCHAIN_SERVICE", "io.altessa.rmctl.e2e."+rand.Text())
	e.register()
	t.Cleanup(func() {
		// Whatever the test left in the Keychain goes.
		_, _, _ = e.rmctl("logout", "--yes")
	})

	out := e.ok("secrets", "migrate", "--to", "keychain", "--set-default", "--yes")
	if !strings.Contains(out, "default_set\ttrue\n") {
		t.Fatalf("%q", out)
	}
	cfg, err := os.ReadFile(filepath.Join(os.Getenv("RMCTL_CONFIG_DIR"), "config.toml"))
	if err != nil || !strings.Contains(string(cfg), "[profiles.default.secrets]") {
		t.Fatalf("%q %v", cfg, err)
	}
	if st := e.ok("secrets", "status"); !strings.Contains(st, "backend\tkeychain\nsource\tprofile\n") || !strings.Contains(st, "deviceToken") {
		t.Fatalf("%q", st)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("RMCTL_CONFIG_DIR"), "default", "token.json")); !os.IsNotExist(err) {
		t.Fatalf("the token file is still there: %v", err)
	}
	e.ok("ls")
	// Back to the file, by the environment this time: the file chose the
	// Keychain, so the move is explicit.
	e.ok("secrets", "migrate", "--to", "file", "--yes")
	t.Setenv("RMCTL_SECRETS", "file")
	e.ok("ls")
}

// profile add and del through the real login Keychain: a profile whose
// table keeps it in the Keychain is registered there, and deleting it takes
// its items out and its table out of config.toml. Skipped unless
// RMCTL_KEYCHAIN_TESTS=1; the items go under a service of the test's own.
func TestE2E_KeychainProfileDelete(t *testing.T) {
	if os.Getenv("RMCTL_KEYCHAIN_TESTS") != "1" {
		t.Skip("set RMCTL_KEYCHAIN_TESTS=1 to use the login Keychain")
	}
	e := newE2E(t)
	t.Setenv("RMCTL_KEYCHAIN_SERVICE", "io.altessa.rmctl.e2e."+rand.Text())
	t.Cleanup(func() {
		_, _, _ = e.rmctl("-P", "work", "logout", "--yes")
	})
	cfg := filepath.Join(os.Getenv("RMCTL_CONFIG_DIR"), "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[profiles.work.secrets]\nbackend = \"keychain\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("profile", "add", "work", clouddouble.ValidCode)
	if st := e.ok("-P", "work", "secrets", "status"); !strings.Contains(st, "backend\tkeychain\n") || !strings.Contains(st, "deviceToken") {
		t.Fatalf("%q", st)
	}
	if out := e.ok("profile", "del", "work", "--yes"); !strings.Contains(out, "keychain_tokens\tdeviceToken") {
		t.Fatalf("%q", out)
	}
	// The table is gone, so the Keychain is named by the environment.
	t.Setenv("RMCTL_SECRETS", "keychain")
	if st := e.ok("-P", "work", "secrets", "status"); strings.Contains(st, "deviceToken") {
		t.Fatalf("left in the Keychain: %q", st)
	}
	if data, err := os.ReadFile(cfg); err != nil || strings.Contains(string(data), "profiles.work") {
		t.Fatalf("%q %v", data, err)
	}
}
