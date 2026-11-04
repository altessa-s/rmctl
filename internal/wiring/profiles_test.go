// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// keychain is a Keychain in a map, so no test reaches the login Keychain.
type keychain struct {
	mu    sync.Mutex
	items map[string]string
}

func newKeychain() *keychain { return &keychain{items: map[string]string{}} }

func (k *keychain) Get(service, account string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.items[service+"|"+account]
	return v, ok, nil
}

func (k *keychain) Put(service, account, _, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.items[service+"|"+account] = value
	return nil
}

func (k *keychain) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.items, service+"|"+account)
	return nil
}

func (k *keychain) accounts() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for key := range k.items {
		_, account, _ := strings.Cut(key, "|")
		out = append(out, account)
	}
	slices.Sort(out)
	return out
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A machine of three profiles — one in its file, one in the Keychain by
// its table, one with a cache only — listed, described and removed.
func TestBuildProfiles(t *testing.T) {
	t.Parallel()
	opts := options(t)
	kc := newKeychain()
	opts.Keychain = kc
	// No request leaves the machine: the hosts are a closed local port.
	opts.Env = map[string]string{"RMCLOUD_HOST": "https://127.0.0.1:1"}
	cfgRoot := filepath.Join(opts.Dirs.Config, "rmctl")
	writeFile(t, filepath.Join(cfgRoot, "work", "token.json"), `{"deviceToken":"d","userToken":"u"}`)
	writeFile(t, filepath.Join(cfgRoot, "config.toml"), "# mine\n[cache]\nttl = \"1m\"\n\n[profiles.home.secrets]\nbackend = \"keychain\"\n")
	if err := os.MkdirAll(filepath.Join(opts.Dirs.Cache, "rmctl", "old", "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(opts.Dirs.Cache, "rmctl", "old", "mine.txt"), "")
	if err := kc.Put("io.altessa.rmctl", "home/deviceToken", "", "d"); err != nil {
		t.Fatal(err)
	}
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()

	list, err := svc.Profiles.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"home", "old", "work"}) || !list[0].Registered || list[0].Backend != port.SecretsKeychain ||
		list[1].Registered || !list[2].Registered || list[2].Backend != port.SecretsFile {
		t.Fatalf("%+v", list)
	}

	info, err := svc.Profiles.Info(t.Context(), "home", true)
	if err != nil || !info.Exists || !info.Table || info.BackendSource != "profile" || !info.Registered || info.AccountErr == nil ||
		!strings.Contains(info.TokenLocation, "io.altessa.rmctl") || info.CacheTTL.String() != "1m0s" {
		t.Fatalf("%+v %v", info, err)
	}
	info, err = svc.Profiles.Info(t.Context(), "", false)
	if err != nil || info.Name != "default" || !info.Current || info.Exists {
		t.Fatalf("%+v %v", info, err)
	}

	// The Keychain profile goes from the Keychain and from config.toml,
	// and every other line stays.
	removed, err := svc.Profiles.Remove(t.Context(), "home", port.RemoveProfileOptions{})
	if err != nil || !removed.Table || len(removed.Tokens) != 1 || removed.Tokens[0].Backend != port.SecretsKeychain {
		t.Fatalf("%+v %v", removed, err)
	}
	if len(kc.accounts()) != 0 {
		t.Fatalf("left in the Keychain: %v", kc.accounts())
	}
	if data, _ := os.ReadFile(filepath.Join(cfgRoot, "config.toml")); string(data) != "# mine\n[cache]\nttl = \"1m\"\n" {
		t.Fatalf("%q", data)
	}
	// The file profile goes with its directory.
	if removed, err = svc.Profiles.Remove(t.Context(), "work", port.RemoveProfileOptions{}); err != nil || !removed.Dir.Existed || removed.Dir.Kept {
		t.Fatalf("%+v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(cfgRoot, "work")); !os.IsNotExist(err) {
		t.Fatalf("the directory stayed: %v", err)
	}
	// The cache-only profile goes with its cache, a file the cache did not
	// write kept.
	if removed, err = svc.Profiles.Remove(t.Context(), "old", port.RemoveProfileOptions{}); err != nil || !removed.Cache.Existed || !removed.Cache.Kept {
		t.Fatalf("%+v %v", removed, err)
	}
	if _, err := svc.Profiles.Remove(t.Context(), "nope", port.RemoveProfileOptions{}); !errors.Is(err, port.ErrNoProfile) {
		t.Fatal(err)
	}
	// A revoke reaches for the cloud, which is out of reach: nothing goes.
	writeFile(t, filepath.Join(cfgRoot, "work", "token.json"), `{"deviceToken":"d"}`)
	if _, err := svc.Profiles.Remove(t.Context(), "work", port.RemoveProfileOptions{Revoke: true}); err == nil {
		t.Fatal("a revoke without a cloud succeeded")
	}
	if _, err := os.Stat(filepath.Join(cfgRoot, "work", "token.json")); err != nil {
		t.Fatal("a failed revoke removed the token:", err)
	}
	// So does registering one.
	if err := svc.Profiles.Add(t.Context(), "travel", "abcdefgh"); err == nil {
		t.Fatal("a registration without a cloud succeeded")
	}
	if err := svc.Profiles.Add(t.Context(), "Bad Name", "abcdefgh"); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatal(err)
	}
}

func TestBuildProfilesFailures(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Keychain = newKeychain()
	cfgRoot := filepath.Join(opts.Dirs.Config, "rmctl")
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	// A configuration file broken after the start fails each question.
	writeFile(t, filepath.Join(cfgRoot, "config.toml"), "[nope]\n")
	if _, err := svc.Profiles.List(t.Context()); err == nil {
		t.Fatal("a broken config.toml was read")
	}
	if _, err := svc.Profiles.Info(t.Context(), "work", true); err == nil {
		t.Fatal("a broken config.toml was read")
	}
	writeFile(t, filepath.Join(cfgRoot, "config.toml"), "")
	// A profile directory that cannot be read.
	writeFile(t, filepath.Join(cfgRoot, "work"), "not a directory")
	if _, err := svc.Profiles.Info(t.Context(), "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Profiles.Remove(t.Context(), "work", port.RemoveProfileOptions{}); err == nil {
		t.Fatal("a file was removed as a profile's directory")
	}
}
