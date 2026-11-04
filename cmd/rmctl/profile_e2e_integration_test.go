//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// memKeychain is a Keychain in a map: the profile commands reach for the
// Keychain of every profile, and no test reaches the login Keychain.
type memKeychain struct {
	mu    sync.Mutex
	items map[string]string
}

func (k *memKeychain) Get(service, account string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.items[service+"|"+account]
	return v, ok, nil
}

func (k *memKeychain) Put(service, account, _, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.items[service+"|"+account] = value
	return nil
}

func (k *memKeychain) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.items, service+"|"+account)
	return nil
}

func (k *memKeychain) len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.items)
}

// Profiles end to end: add two beside the default, one keeping its tokens
// in the Keychain by its table in config.toml, list and describe them, and
// delete them again, leaving the default and every other line of
// config.toml as they were.
func TestE2E_Profiles(t *testing.T) {
	e := newE2E(t)
	kc := &memKeychain{items: map[string]string{}}
	e.keychain = kc
	cfgDir := os.Getenv("RMCTL_CONFIG_DIR")

	if out := e.ok("profile", "ls"); out != "" {
		t.Fatalf("a fresh machine lists %q", out)
	}
	e.register()
	e.ok("profile", "add", "work", clouddouble.ValidCode)
	if code, _, errOut := e.rmctl("profile", "add", "work", clouddouble.ValidCode); code != 1 || !strings.Contains(errOut, "rmctl -P work register --replace") {
		t.Fatalf("a second add: exit %d %q", code, errOut)
	}
	if code, _, errOut := e.rmctl("profile", "add", "Bad Name", clouddouble.ValidCode); code != 1 || !strings.Contains(errOut, "a profile name is") {
		t.Fatalf("a bad name: exit %d %q", code, errOut)
	}
	const config = "# mine\n[cache]\nttl = \"0\"\n\n# home keeps its tokens in the Keychain\n[profiles.home.secrets]\nbackend = \"keychain\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("profile", "add", "home", clouddouble.ValidCode)
	if kc.len() == 0 {
		t.Fatal("home's tokens are not in the Keychain")
	}

	list := e.ok("profile", "ls")
	for _, want := range []string{"default\ttrue\ttrue\tfile\t", "home\tfalse\ttrue\tkeychain\t", "work\tfalse\ttrue\tfile\t"} {
		if !strings.Contains(list, want) {
			t.Errorf("profile ls lacks %q:\n%s", want, list)
		}
	}
	if out := e.ok("-P", "work", "profile", "ls"); !strings.Contains(out, "work\ttrue\t") || !strings.Contains(out, "default\tfalse\t") {
		t.Fatalf("-P work marks another profile:\n%s", out)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(e.ok("profile", "info", "home", "--json")), &info); err != nil {
		t.Fatal(err)
	}
	if info["registered"] != true || info["account"] != "asked" || info["secrets"] != "keychain" || info["secrets_source"] != "profile" ||
		info["config_table"] != true || info["current"] != false {
		t.Fatalf("%v", info)
	}
	code, out, errOut := e.rmctl("profile", "info", "work", "--offline")
	if code != 0 || !strings.Contains(out, "account\toffline\n") || !strings.Contains(errOut, "--offline") {
		t.Fatalf("offline: exit %d %q %q", code, out, errOut)
	}

	if code, _, _ := e.rmctl("profile", "del", "home"); code != 2 {
		t.Fatalf("a delete without --yes: exit %d", code)
	}
	if out := e.ok("profile", "del", "home", "--yes"); !strings.Contains(out, "keychain_tokens\tdeviceToken") || !strings.Contains(out, "table_removed\ttrue") {
		t.Fatalf("%q", out)
	}
	if kc.len() != 0 {
		t.Fatal("home's tokens stayed in the Keychain")
	}
	if data, err := os.ReadFile(filepath.Join(cfgDir, "config.toml")); err != nil || string(data) != "# mine\n[cache]\nttl = \"0\"\n\n# home keeps its tokens in the Keychain\n" {
		t.Fatalf("%q %v", data, err)
	}

	revoked := e.cloud.Revoked()
	e.ok("profile", "del", "work", "--revoke", "--yes")
	if e.cloud.Revoked() != revoked+1 {
		t.Fatal("--revoke did not reach the cloud")
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "work")); !os.IsNotExist(err) {
		t.Fatalf("work's directory stayed: %v", err)
	}
	if code, _, errOut := e.rmctl("profile", "del", "work", "--yes"); code != 1 || !strings.Contains(errOut, "no such profile") {
		t.Fatalf("a second delete: exit %d %q", code, errOut)
	}
	if list := e.ok("profile", "ls"); !strings.HasPrefix(list, "default\ttrue\ttrue\tfile\t") || strings.Count(list, "\n") != 1 {
		t.Fatalf("%q", list)
	}
	e.ok("ls")
}
