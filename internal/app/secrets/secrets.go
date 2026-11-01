// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/altessa-s/rmctl/internal/port"
)

// Service implements port.Secrets for one profile.
type Service struct {
	profile    string
	source     string
	configFile string
	current    port.SecretVault
	vaults     map[port.SecretsBackend]port.SecretVault
	config     port.ConfigWriter
}

var _ port.Secrets = (*Service)(nil)

// New is the secrets service of profile. current is the vault in use;
// vaults are every vault the profile can move to, current included; source
// says what chose current; config records a new choice in configFile.
func New(profile, source, configFile string, current port.SecretVault, vaults []port.SecretVault, config port.ConfigWriter) *Service {
	switch {
	case profile == "":
		panic("secrets: a profile is required")
	case current == nil:
		panic("secrets: the vault in use is required")
	case config == nil:
		panic("secrets: a config writer is required")
	}
	byBackend := make(map[port.SecretsBackend]port.SecretVault, len(vaults)+1)
	for _, v := range vaults {
		byBackend[v.Backend()] = v
	}
	byBackend[current.Backend()] = current
	return &Service{profile: profile, source: source, configFile: configFile, current: current, vaults: byBackend, config: config}
}

// Status implements port.Secrets.
func (s *Service) Status(ctx context.Context) (port.SecretsStatus, error) {
	present, err := held(ctx, s.current)
	if err != nil {
		return port.SecretsStatus{}, err
	}
	names := make([]string, 0, len(present))
	for _, p := range present {
		names = append(names, p.name)
	}
	return port.SecretsStatus{
		Profile: s.profile, Backend: s.current.Backend(), Location: s.current.Location(),
		Source: s.source, ConfigFile: s.configFile, Present: names,
	}, nil
}

// secret is one token in hand during a migration.
type secret struct{ name, value string }

func held(ctx context.Context, v port.SecretVault) ([]secret, error) {
	var out []secret
	for _, name := range v.Names() {
		value, ok, err := v.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, secret{name, value})
		}
	}
	return out, nil
}

// Migrate implements port.Secrets.
func (s *Service) Migrate(ctx context.Context, to port.SecretsBackend, opts port.MigrateOptions) (port.SecretsMigration, error) {
	from := s.current
	if to == from.Backend() {
		return port.SecretsMigration{}, fmt.Errorf("%s: %w", to, port.ErrSameBackend)
	}
	target, ok := s.vaults[to]
	if !ok {
		return port.SecretsMigration{}, fmt.Errorf("unknown backend %q", to)
	}
	var override string
	if variable, chosen, ok := s.config.Override(); ok && chosen != string(to) {
		override = variable + "=" + chosen
		if opts.SetDefault {
			// The file would name the target and the next run would still
			// read the other backend, emptied by the move.
			return port.SecretsMigration{}, fmt.Errorf("%s: %w", override, port.ErrOverridden)
		}
	}
	tokens, err := held(ctx, from)
	if err != nil {
		return port.SecretsMigration{}, err
	}
	if len(tokens) == 0 {
		return port.SecretsMigration{}, port.ErrNothingToMigrate
	}
	before, err := held(ctx, target)
	if err != nil {
		return port.SecretsMigration{}, err
	}
	if len(before) > 0 && !opts.Replace {
		return port.SecretsMigration{}, port.ErrTargetOccupied
	}
	result := port.SecretsMigration{From: from.Backend(), To: to, Location: target.Location(), Override: override}
	if err := s.copyAll(ctx, target, tokens, before); err != nil {
		return port.SecretsMigration{}, err
	}
	if opts.SetDefault {
		path, cerr := s.config.SetSecretsBackend(s.profile, string(to))
		if cerr != nil {
			return port.SecretsMigration{}, errors.Join(cerr, restore(ctx, target, tokens, before))
		}
		result.DefaultSet, result.ConfigFile = true, path
	} else {
		result.ConfigFile, result.ConfigLines = s.configFile, s.config.SecretsLines(s.profile, string(to))
	}
	// Every token is in the target and reads back: only now does the source
	// let go of them.
	for _, t := range tokens {
		if err := from.Delete(ctx, t.name); err != nil {
			return result, fmt.Errorf("the tokens are in %s, but %s could not be removed from %s: %w", to, t.name, from.Backend(), err)
		}
		result.Moved = append(result.Moved, t.name)
	}
	return result, nil
}

// copyAll writes every token to the target and reads each back. On any
// failure the target is put back as it was.
func (s *Service) copyAll(ctx context.Context, target port.SecretVault, tokens, before []secret) error {
	for _, t := range tokens {
		if err := target.Put(ctx, t.name, t.value); err != nil {
			return errors.Join(err, restore(ctx, target, tokens, before))
		}
	}
	for _, t := range tokens {
		got, ok, err := target.Get(ctx, t.name)
		if err != nil {
			return errors.Join(err, restore(ctx, target, tokens, before))
		}
		if !ok || got != t.value {
			return errors.Join(fmt.Errorf("%s: %w", t.name, port.ErrReadBack), restore(ctx, target, tokens, before))
		}
	}
	return nil
}

// restore puts the target back as it was before the migration wrote to it:
// the tokens it held are written again, the ones it did not are removed.
func restore(ctx context.Context, target port.SecretVault, tokens, before []secret) error {
	had := make(map[string]string, len(before))
	for _, b := range before {
		had[b.name] = b.value
	}
	var errs []error
	for _, t := range tokens {
		if v, ok := had[t.name]; ok {
			errs = append(errs, target.Put(ctx, t.name, v))
			continue
		}
		errs = append(errs, target.Delete(ctx, t.name))
	}
	return errors.Join(errs...)
}
