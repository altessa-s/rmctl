// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"slices"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/cloud"
	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/adapter/keychain"
	"github.com/altessa-s/rmctl/internal/adapter/secretfile"
	"github.com/altessa-s/rmctl/internal/adapter/vault"
	"github.com/altessa-s/rmctl/internal/port"

	rmcloud "github.com/altessa-s/rmcloud-go"
)

// tokenStores are the two places a profile's tokens may be kept.
type tokenStores struct {
	file *secretfile.Store
	key  *keychain.Store
}

// secretsOf are the token stores of the profile cfg describes, the
// Keychain's through items (nil is the login Keychain).
func secretsOf(cfg config.Config, items KeychainItems) tokenStores {
	service := cfg.KeychainService
	if service == "" {
		service = keychain.DefaultService
	}
	key := keychain.New(service, cfg.Profile)
	if items != nil {
		key = keychain.NewWith(items, service, cfg.Profile)
	}
	return tokenStores{file: secretfile.New(cfg.TokenPath), key: key}
}

// inUse is the store the configuration chooses.
func (s tokenStores) inUse(cfg config.Config) rmcloud.SecretStore {
	if cfg.Secrets == config.BackendKeychain {
		return s.key
	}
	return s.file
}

// vaults are the file's vault and the Keychain's.
func (s tokenStores) vaults() (file, key port.SecretVault) {
	return vault.New(s.file, port.SecretsFile, s.file.Path()), vault.New(s.key, port.SecretsKeychain, s.key.Location())
}

// profileStore implements port.ProfileStore over the profile directories,
// config.toml and the token stores, resolving each profile as a run of
// rmctl -P <name> would.
type profileStore struct {
	env      map[string]string
	dirs     config.BaseDirs
	read     config.ReadFunc
	root     string
	file     string
	keychain KeychainItems
}

var _ port.ProfileStore = profileStore{}

func newProfileStore(cfg config.Config, opts Options, read config.ReadFunc) profileStore {
	return profileStore{env: opts.Env, dirs: opts.Dirs, read: read, root: cfg.ConfigRoot, file: cfg.ConfigFile, keychain: opts.Keychain}
}

// Names implements port.ProfileStore.
func (p profileStore) Names() ([]string, error) {
	dirs, err := config.ProfileDirs(p.root)
	if err != nil {
		return nil, err
	}
	tables, err := config.TableProfiles(p.file, p.read)
	if err != nil {
		return nil, err
	}
	return sortedUnion(dirs, tables), nil
}

func (p profileStore) config(name string) (config.Config, error) {
	return config.LoadFrom(p.env, name, p.dirs, p.read)
}

// Settings implements port.ProfileStore.
func (p profileStore) Settings(name string) (port.ProfileSettings, error) {
	cfg, err := p.config(name)
	if err != nil {
		return port.ProfileSettings{}, err
	}
	exists, err := config.DirExists(cfg.ConfigDir)
	if err != nil {
		return port.ProfileSettings{}, err
	}
	return port.ProfileSettings{
		Name: cfg.Profile, ConfigDir: cfg.ConfigDir, DirExists: exists,
		Backend: port.SecretsBackend(cfg.Secrets), BackendSource: cfg.SecretsSource,
		ConfigFile: cfg.ConfigFile, Table: cfg.ProfileTable, CacheTTL: cfg.CacheTTL, Offline: cfg.Offline,
	}, nil
}

// Vaults implements port.ProfileStore.
func (p profileStore) Vaults(name string) ([]port.SecretVault, error) {
	cfg, err := p.config(name)
	if err != nil {
		return nil, err
	}
	file, key := secretsOf(cfg, p.keychain).vaults()
	if cfg.Secrets == config.BackendKeychain {
		return []port.SecretVault{key, file}, nil
	}
	return []port.SecretVault{file, key}, nil
}

// RemoveDir implements port.ProfileStore.
func (p profileStore) RemoveDir(name string) (port.RemovedDir, error) {
	cfg, err := p.config(name)
	if err != nil {
		return port.RemovedDir{}, err
	}
	existed, kept, err := config.RemoveProfileDir(cfg)
	return port.RemovedDir{Path: cfg.ConfigDir, Existed: existed, Kept: kept}, err
}

// RemoveTable implements port.ProfileStore.
func (p profileStore) RemoveTable(name string) (bool, error) {
	return config.Writer{Path: p.file, Read: p.read, Env: p.env}.RemoveProfile(name)
}

// registries implements port.ProfileRegistries: the registration of any
// profile, on a transport of its own, clearing that profile's cache when
// its account changes hands as the run's own registration does.
type registries struct {
	opts Options
	read config.ReadFunc
	root *blobcache.Root
}

var _ port.ProfileRegistries = registries{}

// Open implements port.ProfileRegistries.
func (r registries) Open(name string) (port.Registry, func() error, error) {
	cfg, err := config.LoadFrom(r.opts.Env, name, r.opts.Dirs, r.read)
	if err != nil {
		return nil, nil, err
	}
	cache, err := r.root.Profile(cfg.Profile)
	if err != nil {
		return nil, nil, err
	}
	client, err := newClient(secretsOf(cfg, r.opts.Keychain).inUse(cfg), cfg, r.opts)
	if err != nil {
		return nil, nil, err
	}
	return indexcache.NewRegistry(cloud.New(client), cache), client.Close, nil
}

// sortedUnion is every name of a and b once, sorted.
func sortedUnion(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}
