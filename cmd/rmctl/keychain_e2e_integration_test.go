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
	if !strings.Contains(out, "now chooses the keychain") {
		t.Fatalf("%q", out)
	}
	cfg, err := os.ReadFile(filepath.Join(os.Getenv("RMCTL_CONFIG_DIR"), "config.toml"))
	if err != nil || !strings.Contains(string(cfg), "[profiles.default.secrets]") {
		t.Fatalf("%q %v", cfg, err)
	}
	if st := e.ok("secrets", "status"); !strings.Contains(st, "backend   keychain (chosen by profile)") || !strings.Contains(st, "deviceToken") {
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
