// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/secrets"
	"github.com/altessa-s/rmctl/internal/port"
)

// vault is a SecretVault in a map, with failures to inject.
type vault struct {
	mu      sync.Mutex
	backend port.SecretsBackend
	values  map[string]string
	failPut map[string]error
	failGet error
	failDel error
	// corrupt makes Get answer another value than the one put.
	corrupt bool
}

func newVault(b port.SecretsBackend, values map[string]string) *vault {
	if values == nil {
		values = map[string]string{}
	}
	return &vault{backend: b, values: values, failPut: map[string]error{}}
}

func (v *vault) Backend() port.SecretsBackend { return v.backend }
func (v *vault) Location() string             { return "at " + string(v.backend) }
func (v *vault) Names() []string              { return []string{"deviceToken", "deviceID", "userToken"} }

func (v *vault) Get(_ context.Context, name string) (string, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.failGet != nil {
		return "", false, v.failGet
	}
	value, ok := v.values[name]
	if ok && v.corrupt {
		value += "-changed"
	}
	return value, ok, nil
}

func (v *vault) Put(_ context.Context, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.failPut[name]; err != nil {
		return err
	}
	v.values[name] = value
	return nil
}

func (v *vault) Delete(_ context.Context, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.failDel != nil {
		return v.failDel
	}
	delete(v.values, name)
	return nil
}

func (v *vault) snapshot() map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return maps.Clone(v.values)
}

// config records what the use case asks of the configuration file.
type config struct {
	set  []string
	fail error
	// env is what RMCTL_SECRETS says, or "".
	env string
}

func (c *config) Override() (string, string, bool) { return "RMCTL_SECRETS", c.env, c.env != "" }

func (c *config) SetSecretsBackend(profile, backend string) (string, error) {
	if c.fail != nil {
		return "", c.fail
	}
	c.set = append(c.set, profile+"="+backend)
	return "/c/config.toml", nil
}

func (*config) SecretsLines(profile, backend string) string { return profile + ":" + backend }

var tokens = map[string]string{"deviceToken": "dev", "userToken": "usr"}

func service(file, key *vault, cfg *config) *secrets.Service {
	return secrets.New("work", "default", "/c/config.toml", file, []port.SecretVault{file, key}, cfg)
}

func TestStatus(t *testing.T) {
	t.Parallel()
	file, key := newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil)
	st, err := service(file, key, &config{}).Status(t.Context())
	if err != nil || st.Backend != port.SecretsFile || st.Profile != "work" || st.Source != "default" ||
		st.ConfigFile != "/c/config.toml" || !slices.Equal(st.Present, []string{"deviceToken", "userToken"}) {
		t.Fatalf("%+v %v", st, err)
	}
	file.failGet = errors.New("locked")
	if _, err := service(file, key, &config{}).Status(t.Context()); err == nil {
		t.Fatal("a read failure was ignored")
	}
}

func TestMigrateBothWays(t *testing.T) {
	t.Parallel()
	file, key := newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil)
	cfg := &config{}
	m, err := service(file, key, cfg).Migrate(t.Context(), port.SecretsKeychain, port.MigrateOptions{})
	if err != nil || m.From != port.SecretsFile || m.To != port.SecretsKeychain || !slices.Equal(m.Moved, []string{"deviceToken", "userToken"}) ||
		m.DefaultSet || m.ConfigLines != "work:keychain" || m.ConfigFile != "/c/config.toml" {
		t.Fatalf("%+v %v", m, err)
	}
	if len(file.snapshot()) != 0 || !maps.Equal(key.snapshot(), tokens) || len(cfg.set) != 0 {
		t.Fatalf("file %v key %v", file.snapshot(), key.snapshot())
	}
	// And back, recording the choice this time.
	back := secrets.New("work", "profile", "/c/config.toml", key, []port.SecretVault{file, key}, cfg)
	m, err = back.Migrate(t.Context(), port.SecretsFile, port.MigrateOptions{SetDefault: true})
	if err != nil || !m.DefaultSet || m.ConfigLines != "" || !slices.Equal(cfg.set, []string{"work=file"}) {
		t.Fatalf("%+v %v %v", m, err, cfg.set)
	}
	if !maps.Equal(file.snapshot(), tokens) || len(key.snapshot()) != 0 {
		t.Fatalf("file %v key %v", file.snapshot(), key.snapshot())
	}
}

// Every refusal and every failure before the source is cleared leaves the
// source as it was, and the target as it was.
func TestMigrateNeverLosesAToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	occupied := map[string]string{"deviceToken": "theirs"}
	tests := []struct {
		name    string
		setup   func(file, key *vault, cfg *config)
		opts    port.MigrateOptions
		to      port.SecretsBackend
		want    error
		keyWant map[string]string
	}{
		{name: "same backend", to: port.SecretsFile, want: port.ErrSameBackend},
		{name: "unknown backend", to: "vault"},
		{name: "target occupied", to: port.SecretsKeychain, setup: func(_, key *vault, _ *config) { key.values = maps.Clone(occupied) },
			want: port.ErrTargetOccupied, keyWant: occupied},
		{name: "write fails half way", to: port.SecretsKeychain, setup: func(_, key *vault, _ *config) { key.failPut["userToken"] = errors.New("full") }},
		{name: "reads back wrong", to: port.SecretsKeychain, setup: func(_, key *vault, _ *config) { key.corrupt = true }, want: port.ErrReadBack},
		{name: "read back fails", to: port.SecretsKeychain, setup: func(_, key *vault, _ *config) { key.failGet = errors.New("locked") }},
		{name: "config refuses", to: port.SecretsKeychain, opts: port.MigrateOptions{SetDefault: true},
			setup: func(_, _ *vault, cfg *config) { cfg.fail = errors.New("edit by hand") }},
		{name: "replace restores what it overwrote", to: port.SecretsKeychain, opts: port.MigrateOptions{Replace: true, SetDefault: true},
			setup: func(_, key *vault, cfg *config) {
				key.values = maps.Clone(occupied)
				cfg.fail = errors.New("edit by hand")
			}, keyWant: occupied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, key, cfg := newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil), &config{}
			if tt.setup != nil {
				tt.setup(file, key, cfg)
			}
			_, err := service(file, key, cfg).Migrate(ctx, tt.to, tt.opts)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err %v, want %v", err, tt.want)
			}
			if !maps.Equal(file.snapshot(), tokens) {
				t.Fatalf("the source changed: %v", file.snapshot())
			}
			key.failGet, key.corrupt = nil, false
			want := tt.keyWant
			if want == nil {
				want = map[string]string{}
			}
			if !maps.Equal(key.snapshot(), want) {
				t.Fatalf("the target was not put back: %v", key.snapshot())
			}
		})
	}
}

// RMCTL_SECRETS outranks the file: recording the target there would leave
// the next run reading the emptied source.
func TestMigrateUnderAnOverride(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	file, key := newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil)
	cfg := &config{env: "file"}
	s := secrets.New("work", "env", "/c/config.toml", file, []port.SecretVault{file, key}, cfg)
	_, err := s.Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{SetDefault: true})
	if !errors.Is(err, port.ErrOverridden) || !strings.Contains(err.Error(), "RMCTL_SECRETS=file") {
		t.Fatalf("err %v", err)
	}
	if !maps.Equal(file.snapshot(), tokens) || len(key.snapshot()) != 0 || len(cfg.set) != 0 {
		t.Fatalf("written: file %v key %v config %v", file.snapshot(), key.snapshot(), cfg.set)
	}
	m, err := s.Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{})
	if err != nil || m.Override != "RMCTL_SECRETS=file" || m.ConfigLines == "" {
		t.Fatalf("%+v %v", m, err)
	}
	// An override that names the target already agrees with the file.
	file, key = newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil)
	cfg = &config{env: "keychain"}
	m, err = secrets.New("work", "env", "/c/config.toml", file, []port.SecretVault{file, key}, cfg).
		Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{SetDefault: true})
	if err != nil || m.Override != "" || !m.DefaultSet {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestMigrateEdges(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	empty := newVault(port.SecretsFile, nil)
	if _, err := service(empty, newVault(port.SecretsKeychain, nil), &config{}).Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{}); !errors.Is(err, port.ErrNothingToMigrate) {
		t.Fatalf("err %v", err)
	}
	// The tokens are safe in the target when the source cannot let go.
	file, key := newVault(port.SecretsFile, maps.Clone(tokens)), newVault(port.SecretsKeychain, nil)
	file.failDel = errors.New("read-only disk")
	m, err := service(file, key, &config{}).Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "could not be removed") || !maps.Equal(key.snapshot(), tokens) || m.To != port.SecretsKeychain {
		t.Fatalf("%+v %v %v", m, err, key.snapshot())
	}
	// A source read failure stops before anything is written.
	broken := newVault(port.SecretsFile, maps.Clone(tokens))
	broken.failGet = errors.New("locked")
	if _, err := service(broken, newVault(port.SecretsKeychain, nil), &config{}).Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{}); err == nil {
		t.Fatal("a source failure was ignored")
	}
	target := newVault(port.SecretsKeychain, nil)
	target.failGet = errors.New("locked")
	if _, err := service(newVault(port.SecretsFile, maps.Clone(tokens)), target, &config{}).Migrate(ctx, port.SecretsKeychain, port.MigrateOptions{}); err == nil {
		t.Fatal("a target failure was ignored")
	}
}

func TestNewNeedsItsParts(t *testing.T) {
	t.Parallel()
	v := newVault(port.SecretsFile, nil)
	for _, build := range []func(){
		func() { secrets.New("", "", "", v, nil, &config{}) },
		func() { secrets.New("w", "", "", nil, nil, &config{}) },
		func() { secrets.New("w", "", "", v, nil, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			build()
		}()
	}
}
