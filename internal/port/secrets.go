// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
)

// SecretsBackend is where a profile keeps its tokens.
type SecretsBackend string

// The backends a profile can keep its tokens in.
const (
	// SecretsFile is a JSON file of mode 0600 in the profile's directory.
	SecretsFile SecretsBackend = "file"
	// SecretsKeychain is the macOS login Keychain.
	SecretsKeychain SecretsBackend = "keychain"
)

// The failures of the secrets use case.
var (
	// ErrKeychainUnsupported means this build reaches no Keychain: it is not
	// a macOS build, or it was built without cgo.
	ErrKeychainUnsupported = errors.New("the macOS Keychain needs a macOS build of rmctl with cgo")
	// ErrSameBackend means a migration was asked to the backend in use.
	ErrSameBackend = errors.New("the tokens are already kept there")
	// ErrNothingToMigrate means the backend in use holds no token.
	ErrNothingToMigrate = errors.New("the profile holds no token to move")
	// ErrTargetOccupied means the target already holds a token.
	ErrTargetOccupied = errors.New("the target already holds a token of this profile; pass --replace to overwrite it")
	// ErrReadBack means a token written to the target did not read back the
	// same. Nothing was removed from the source.
	ErrReadBack = errors.New("a token did not read back the same from the target; nothing was removed from the source")
	// ErrOverridden means the environment chooses another backend than the
	// one --set-default would record, so the next run would not use it.
	// Nothing was written.
	ErrOverridden = errors.New("the environment overrides the configuration file; unset it or set it to the target first")
)

// SecretVault is one place a profile's tokens are kept. Values pass
// through it and are never logged or put in an error.
type SecretVault interface {
	Backend() SecretsBackend
	// Location says where, for a person: a file path, or the Keychain
	// service and account prefix.
	Location() string
	// Names are the secrets a vault may hold, in a fixed order.
	Names() []string
	Get(ctx context.Context, name string) (string, bool, error)
	Put(ctx context.Context, name, value string) error
	// Delete removes a secret; a missing one is not an error.
	Delete(ctx context.Context, name string) error
}

// ConfigWriter records a profile's backend in the configuration file.
type ConfigWriter interface {
	// SetSecretsBackend makes backend the profile's own and returns the
	// path of the file it wrote.
	SetSecretsBackend(profile, backend string) (string, error)
	// SecretsLines are the lines that make backend the profile's own, for a
	// person to add by hand.
	SecretsLines(profile, backend string) string
	// Override is the environment variable that chooses the backend over
	// the file, and the backend it names, when it is set.
	Override() (variable, backend string, ok bool)
}

// SecretsStatus is where the profile's tokens are and which are there.
type SecretsStatus struct {
	Profile  string
	Backend  SecretsBackend
	Location string
	// Source is what chose the backend: "env", "profile", "config" or
	// "default".
	Source string
	// ConfigFile is the configuration file that is read, present or not.
	ConfigFile string
	// Present are the names of the secrets held; never their values.
	Present []string
}

// MigrateOptions shape a migration.
type MigrateOptions struct {
	// Replace overwrites tokens the target already holds.
	Replace bool
	// SetDefault records the target as the profile's backend in the
	// configuration file before the source is cleared.
	SetDefault bool
}

// SecretsMigration is what a migration did.
type SecretsMigration struct {
	From, To SecretsBackend
	Location string
	Moved    []string
	// DefaultSet says the configuration file now names the target, and
	// ConfigFile which file; otherwise ConfigLines are what to add to it.
	DefaultSet  bool
	ConfigFile  string
	ConfigLines string
	// Override is the setting of the environment, VARIABLE=backend, that
	// keeps choosing another backend than the target until it is unset.
	Override string
}

// Secrets reports and moves the tokens of the profile.
type Secrets interface {
	Status(ctx context.Context) (SecretsStatus, error)
	// Migrate copies every token to the target, reads each back, and only
	// then removes them from the source: no failure loses a token.
	Migrate(ctx context.Context, to SecretsBackend, opts MigrateOptions) (SecretsMigration, error)
}
