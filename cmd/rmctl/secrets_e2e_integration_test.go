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
	"testing"
)

func TestE2E_SecretsFromTheConfigFile(t *testing.T) {
	e := newE2E(t)
	e.register()

	var st map[string]any
	if err := json.Unmarshal([]byte(e.ok("secrets", "status", "--json")), &st); err != nil {
		t.Fatal(err)
	}
	if st["backend"] != "file" || st["source"] != "default" || !strings.HasSuffix(st["config_file"].(string), "config.toml") {
		t.Fatalf("%v", st)
	}
	if present, _ := st["present"].([]any); len(present) == 0 {
		t.Fatalf("a registered profile holds no token: %v", st)
	}
	// Moving to where the tokens already are is refused, and changes nothing.
	if code, _, errOut := e.rmctl("secrets", "migrate", "--to", "file", "--yes"); code != 1 || !strings.Contains(errOut, "already kept there") {
		t.Fatalf("exit %d %q", code, errOut)
	}
	// A configuration file that names a backend rmctl does not know stops
	// every command, naming the key.
	cfgDir := os.Getenv("RMCTL_CONFIG_DIR")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[secrets]\nbackend = \"vault\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := e.rmctl("ls"); code == 0 || !strings.Contains(errOut, "secrets.backend") {
		t.Fatalf("exit %d %q", code, errOut)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[secrets]\nbackend = \"file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := e.ok("secrets", "status"); !strings.Contains(out, "source\tconfig\n") {
		t.Fatalf("%q", out)
	}
	if out := e.ok("account"); !strings.Contains(out, "secrets\tfile\n") {
		t.Fatalf("%q", out)
	}
}
