// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"time"
)

// The failures of the profile use case.
var (
	// ErrNoProfile means this machine keeps nothing of the profile: no
	// directory, no table in config.toml, no cache and no token.
	ErrNoProfile = errors.New("no such profile")
	// ErrProfileRegistered means the profile already holds a device token.
	ErrProfileRegistered = errors.New("the profile is already registered")
)

// DeviceToken is the secret whose presence makes a profile registered.
const DeviceToken = "deviceToken" // #nosec G101 -- the name of a secret, not one

// ProfileSettings are what the configuration says of one profile, and
// where it keeps what it keeps.
type ProfileSettings struct {
	Name string
	// ConfigDir is the profile's directory, and DirExists whether it is
	// there.
	ConfigDir string
	DirExists bool
	// Backend is where its tokens are kept, and BackendSource what chose
	// it: "env", "profile", "config" or "default".
	Backend       SecretsBackend
	BackendSource string
	// ConfigFile is config.toml, present or not, and Table whether it holds
	// a table of the profile's own.
	ConfigFile string
	Table      bool
	// CacheTTL and Offline are the profile's cache settings.
	CacheTTL time.Duration
	Offline  bool
}

// RemovedDir is what removing a directory did.
type RemovedDir struct {
	Path string
	// Existed says there was a directory. Kept says it is still there,
	// holding files rmctl did not write: they are never removed.
	Existed, Kept bool
}

// ProfileStore is what this machine keeps of every profile apart from its
// cache: the profile directories and config.toml.
type ProfileStore interface {
	// Names are the profiles that have a directory or a table in
	// config.toml, sorted.
	Names() ([]string, error)
	// Settings resolves a profile's settings. A name that is not a profile
	// name is an error.
	Settings(name string) (ProfileSettings, error)
	// Vaults are every place the profile's tokens may be kept, the one its
	// settings choose first.
	Vaults(name string) ([]SecretVault, error)
	// RemoveDir removes the profile's directory and the files rmctl keeps
	// in it.
	RemoveDir(name string) (RemovedDir, error)
	// RemoveTable removes the profile's tables from config.toml and keeps
	// every other line as it is; removed is false when it had none.
	RemoveTable(name string) (removed bool, err error)
}

// ProfileCaches are the caches on disk of every profile.
type ProfileCaches interface {
	// Names are the profiles that have a cache, sorted.
	Names() []string
	// Status is one profile's cache, whether it has one or not.
	Status(name string) (ProfileCache, error)
	// Remove empties the profile's cache and removes its directory.
	Remove(name string) (RemovedDir, error)
}

// ProfileRegistries open the device registration of any profile. release
// lets go of what the registry holds.
type ProfileRegistries interface {
	Open(name string) (reg Registry, release func() error, err error)
}

// ProfileSummary is one profile as profile ls lists it.
type ProfileSummary struct {
	Name string
	// Current says this run uses the profile.
	Current bool
	// Registered says the profile holds a device token; TokenErr says the
	// place its tokens are kept could not be read, so it is unknown.
	Registered bool
	TokenErr   error
	Backend    SecretsBackend
	// CacheBytes is what its cache takes.
	CacheBytes int64
}

// ProfileInfo is one profile in detail.
type ProfileInfo struct {
	ProfileSettings
	// Current says this run uses the profile, and Exists that this machine
	// keeps something of it.
	Current, Exists bool
	// TokenLocation is where its tokens are kept, for a person, and Tokens
	// the names of those there; TokenErr says that place could not be read.
	TokenLocation string
	Tokens        []string
	TokenErr      error
	Registered    bool
	// Account is what the cloud says of a registered profile's account.
	// AccountErr says why it was not asked (ErrOffline) or did not answer;
	// both are zero for a profile that is not registered or when the
	// account was not asked for.
	Account    AccountInfo
	AccountErr error
	Cache      ProfileCache
}

// RemovedTokens are the tokens removed from one place.
type RemovedTokens struct {
	Backend  SecretsBackend
	Location string
	Names    []string
}

// ProfileRemoval is what removing a profile did, step by step; a step that
// was not reached is zero.
type ProfileRemoval struct {
	Name string
	// Revoked says the device was removed from the account at the cloud.
	Revoked bool
	Tokens  []RemovedTokens
	// Unreachable is the place the profile's settings keep its tokens when
	// this build cannot reach it (the Keychain off macOS): nothing there
	// was removed.
	Unreachable []SecretsBackend
	Dir         RemovedDir
	Cache       RemovedDir
	// Table says config.toml lost the profile's tables, and ConfigFile is
	// that file.
	Table      bool
	ConfigFile string
}

// RemoveProfileOptions shape a profile's removal.
type RemoveProfileOptions struct {
	// Revoke first removes the device from the account at the cloud.
	Revoke bool
}

// Profiles lists, adds, describes and removes the profiles of this
// machine.
type Profiles interface {
	// List is every profile this machine keeps something of, and the one
	// in use when it is registered, by name.
	List(ctx context.Context) ([]ProfileSummary, error)
	// Info is one profile; "" is the one in use. account asks the cloud
	// what the account of a registered profile is.
	Info(ctx context.Context, name string, account bool) (ProfileInfo, error)
	// Add registers a new profile with a one-time code. A profile that
	// holds a device token is refused.
	Add(ctx context.Context, name, code string) error
	// Remove removes the profile's tokens from every place they may be
	// kept, its directory, its cache and its tables in config.toml, in that
	// order, stopping at the first step that fails.
	Remove(ctx context.Context, name string, opts RemoveProfileOptions) (ProfileRemoval, error)
}
