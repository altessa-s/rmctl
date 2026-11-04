// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/altessa-s/rmctl/internal/port"
)

// Vault is a port.SecretVault in a map. Fail makes the named method (Get,
// Put, Delete) return the error. It is safe for concurrent use.
type Vault struct {
	mu      sync.Mutex
	backend port.SecretsBackend
	values  map[string]string
	Fail    map[string]error
}

var _ port.SecretVault = (*Vault)(nil)

// NewVault is a vault of backend holding values.
func NewVault(backend port.SecretsBackend, values map[string]string) *Vault {
	if values == nil {
		values = map[string]string{}
	}
	return &Vault{backend: backend, values: maps.Clone(values), Fail: map[string]error{}}
}

// Backend implements port.SecretVault.
func (v *Vault) Backend() port.SecretsBackend { return v.backend }

// Location implements port.SecretVault.
func (v *Vault) Location() string { return "in the " + string(v.backend) }

// Names implements port.SecretVault: the transport's secrets.
func (v *Vault) Names() []string { return []string{port.DeviceToken, "deviceID", "userToken"} }

// Get implements port.SecretVault.
func (v *Vault) Get(_ context.Context, name string) (string, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.Fail["Get"]; err != nil {
		return "", false, err
	}
	value, ok := v.values[name]
	return value, ok, nil
}

// Put implements port.SecretVault.
func (v *Vault) Put(_ context.Context, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.Fail["Put"]; err != nil {
		return err
	}
	v.values[name] = value
	return nil
}

// Delete implements port.SecretVault.
func (v *Vault) Delete(_ context.Context, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.Fail["Delete"]; err != nil {
		return err
	}
	delete(v.values, name)
	return nil
}

// Values are what the vault holds.
func (v *Vault) Values() map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return maps.Clone(v.values)
}

// ProfileState is what a [Profiles] keeps of one profile.
type ProfileState struct {
	// Settings are what the configuration says; Name is filled in.
	Settings port.ProfileSettings
	// Dir, Table and Cache say the profile has a directory, a table in
	// config.toml and a cache. KeptDir and KeptCache make their removal
	// leave them, as for files rmctl did not write.
	Dir, Table, Cache  bool
	KeptDir, KeptCache bool
	CacheBytes         int64
	// File and Keychain are its vaults; nil is an empty one.
	File, Keychain *Vault
}

// Profiles implements port.ProfileStore over a map of profiles, through
// [Profiles.Caches] port.ProfileCaches, and port.ProfileRegistries over a
// [Cloud] per profile whose
// registration fills the profile's vault in use. Fail makes the named
// method (Names, Settings, Vaults, RemoveDir, RemoveTable, Status,
// RemoveCache, Open) return the error; Calls records them in order. It is
// safe for concurrent use.
type Profiles struct {
	mu       sync.Mutex
	profiles map[string]*ProfileState
	clouds   map[string]*Cloud
	calls    []string
	Fail     map[string]error
	// Closed counts the registries released.
	Closed int
}

var (
	_ port.ProfileStore      = (*Profiles)(nil)
	_ port.ProfileCaches     = profileCaches{}
	_ port.ProfileRegistries = (*Profiles)(nil)
)

// CacheBudget is the budget of every cache [Profiles] reports.
const CacheBudget int64 = 1 << 20

// ErrBadName is what Profiles answers for a name ValidName refuses.
var ErrBadName = errors.New("porttest: not a profile name")

// ValidName is the profile names [Profiles] accepts: not empty and no
// upper case or space.
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == ' ' || (r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// NewProfiles is a machine keeping profiles.
func NewProfiles(profiles map[string]*ProfileState) *Profiles {
	p := &Profiles{profiles: map[string]*ProfileState{}, clouds: map[string]*Cloud{}, Fail: map[string]error{}}
	maps.Copy(p.profiles, profiles)
	return p
}

// State is the profile's state, created empty when missing.
func (p *Profiles) State(name string) *ProfileState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state(name)
}

func (p *Profiles) state(name string) *ProfileState {
	st, ok := p.profiles[name]
	if !ok {
		st = &ProfileState{}
		p.profiles[name] = st
	}
	if st.Settings.Backend == "" {
		st.Settings.Backend = port.SecretsFile
	}
	if st.File == nil {
		st.File = NewVault(port.SecretsFile, nil)
	}
	if st.Keychain == nil {
		st.Keychain = NewVault(port.SecretsKeychain, nil)
	}
	return st
}

// Cloud is the cloud a profile's registry talks to, created with
// ValidCode "abcdefgh" when missing.
func (p *Profiles) Cloud(name string) *Cloud {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.clouds[name]
	if !ok {
		c = NewCloud()
		c.ValidCode = "abcdefgh"
		p.clouds[name] = c
	}
	return c
}

// Calls are the calls made, each "Method name".
func (p *Profiles) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func (p *Profiles) begin(method, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, method+" "+name)
	if err := p.Fail[method]; err != nil {
		return err
	}
	if name != "" && !ValidName(name) {
		return ErrBadName
	}
	return nil
}

func (p *Profiles) names(keep func(*ProfileState) bool) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for name, st := range p.profiles {
		if keep(st) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Names implements port.ProfileStore.
func (p *Profiles) Names() ([]string, error) {
	if err := p.begin("Names", ""); err != nil {
		return nil, err
	}
	return p.names(func(st *ProfileState) bool { return st.Dir || st.Table }), nil
}

// Settings implements port.ProfileStore.
func (p *Profiles) Settings(name string) (port.ProfileSettings, error) {
	if err := p.begin("Settings", name); err != nil {
		return port.ProfileSettings{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	s := st.Settings
	s.Name, s.DirExists, s.Table = name, st.Dir, st.Table
	if s.ConfigDir == "" {
		s.ConfigDir = "/config/" + name
	}
	if s.ConfigFile == "" {
		s.ConfigFile = "/config/config.toml"
	}
	return s, nil
}

// Vaults implements port.ProfileStore.
func (p *Profiles) Vaults(name string) ([]port.SecretVault, error) {
	if err := p.begin("Vaults", name); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	if st.Settings.Backend == port.SecretsKeychain {
		return []port.SecretVault{st.Keychain, st.File}, nil
	}
	return []port.SecretVault{st.File, st.Keychain}, nil
}

// RemoveDir implements port.ProfileStore.
func (p *Profiles) RemoveDir(name string) (port.RemovedDir, error) {
	if err := p.begin("RemoveDir", name); err != nil {
		return port.RemovedDir{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	dir := port.RemovedDir{Path: "/config/" + name, Existed: st.Dir, Kept: st.Dir && st.KeptDir}
	st.Dir = dir.Kept
	return dir, nil
}

// RemoveTable implements port.ProfileStore.
func (p *Profiles) RemoveTable(name string) (bool, error) {
	if err := p.begin("RemoveTable", name); err != nil {
		return false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	had := st.Table
	st.Table = false
	return had, nil
}

// Status implements port.ProfileCaches.
func (p *Profiles) Status(name string) (port.ProfileCache, error) {
	if err := p.begin("Status", name); err != nil {
		return port.ProfileCache{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	return port.ProfileCache{Profile: name, Dir: "/cache/" + name, Bytes: st.CacheBytes, Budget: CacheBudget}, nil
}

// Remove implements port.ProfileCaches.
func (p *Profiles) Remove(name string) (port.RemovedDir, error) {
	if err := p.begin("RemoveCache", name); err != nil {
		return port.RemovedDir{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.state(name)
	dir := port.RemovedDir{Path: "/cache/" + name, Existed: st.Cache, Kept: st.Cache && st.KeptCache}
	st.Cache, st.CacheBytes = dir.Kept, 0
	return dir, nil
}

// cacheNames are the profiles with a cache.
func (p *Profiles) cacheNames() []string {
	return p.names(func(st *ProfileState) bool { return st.Cache })
}

// Caches is p as port.ProfileCaches, whose Names are the profiles with a
// cache.
func (p *Profiles) Caches() port.ProfileCaches { return profileCaches{p} }

type profileCaches struct{ *Profiles }

func (c profileCaches) Names() []string { return c.cacheNames() }

// Open implements port.ProfileRegistries: the profile's [Cloud], whose
// Register puts a device token in the profile's vault in use and whose
// Forget empties it.
func (p *Profiles) Open(name string) (port.Registry, func() error, error) {
	if err := p.begin("Open", name); err != nil {
		return nil, nil, err
	}
	c := p.Cloud(name)
	p.mu.Lock()
	st := p.state(name)
	p.mu.Unlock()
	inUse := st.File
	if st.Settings.Backend == port.SecretsKeychain {
		inUse = st.Keychain
	}
	return registry{Cloud: c, vault: inUse}, func() error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.Closed++
		return nil
	}, nil
}

// registry keeps a profile's vault in step with its Cloud's registration.
type registry struct {
	*Cloud
	vault *Vault
}

func (r registry) Register(ctx context.Context, code string) error {
	if err := r.Cloud.Register(ctx, code); err != nil {
		return err
	}
	return r.vault.Put(ctx, port.DeviceToken, "device-token")
}

func (r registry) IsRegistered(ctx context.Context) (bool, error) {
	if _, err := r.Cloud.IsRegistered(ctx); err != nil {
		return false, err
	}
	_, ok, err := r.vault.Get(ctx, port.DeviceToken)
	return ok, err
}

func (r registry) Forget(ctx context.Context) error {
	if err := r.Cloud.Forget(ctx); err != nil {
		return err
	}
	var errs []error
	for _, name := range r.vault.Names() {
		errs = append(errs, r.vault.Delete(ctx, name))
	}
	return errors.Join(errs...)
}
