// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package profile

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/altessa-s/rmctl/internal/app/account"
	"github.com/altessa-s/rmctl/internal/port"
)

// Service implements port.Profiles.
type Service struct {
	store      port.ProfileStore
	caches     port.ProfileCaches
	registries port.ProfileRegistries
	current    string
	offline    bool
}

var _ port.Profiles = (*Service)(nil)

// New is the profile use case of a run that uses profile current; offline
// says the run never reaches the cloud.
func New(store port.ProfileStore, caches port.ProfileCaches, registries port.ProfileRegistries, current string, offline bool) *Service {
	switch {
	case store == nil || caches == nil || registries == nil:
		panic("profile: a store, the caches and the registries are required")
	case current == "":
		panic("profile: the profile in use is required")
	}
	return &Service{store: store, caches: caches, registries: registries, current: current, offline: offline}
}

// known are the profiles with a directory, a table or a cache, by name.
func (s *Service) known() ([]string, error) {
	names, err := s.store.Names()
	if err != nil {
		return nil, err
	}
	names = append(names, s.caches.Names()...)
	slices.Sort(names)
	return slices.Compact(names), nil
}

// tokens are the names of the secrets v holds.
func tokens(ctx context.Context, v port.SecretVault) ([]string, error) {
	var held []string
	for _, name := range v.Names() {
		_, ok, err := v.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		if ok {
			held = append(held, name)
		}
	}
	return held, nil
}

// state is where a profile keeps its tokens and which are there.
type state struct {
	settings port.ProfileSettings
	vaults   []port.SecretVault
	tokens   []string
	tokenErr error
}

func (st state) registered() bool { return slices.Contains(st.tokens, port.DeviceToken) }

func (s *Service) state(ctx context.Context, name string) (state, error) {
	settings, err := s.store.Settings(name)
	if err != nil {
		return state{}, err
	}
	vaults, err := s.store.Vaults(name)
	if err != nil {
		return state{}, err
	}
	if len(vaults) == 0 {
		return state{}, fmt.Errorf("profile %s: no place to keep its tokens", name)
	}
	st := state{settings: settings, vaults: vaults}
	st.tokens, st.tokenErr = tokens(ctx, vaults[0])
	return st, nil
}

// List implements port.Profiles.
func (s *Service) List(ctx context.Context) ([]port.ProfileSummary, error) {
	names, err := s.known()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(names, s.current) {
		cur, err := s.state(ctx, s.current)
		if err != nil {
			return nil, err
		}
		if cur.registered() {
			names = append(names, s.current)
			slices.Sort(names)
		}
	}
	out := make([]port.ProfileSummary, 0, len(names))
	for _, name := range names {
		st, err := s.state(ctx, name)
		if err != nil {
			return nil, err
		}
		cache, err := s.caches.Status(name)
		if err != nil {
			return nil, err
		}
		out = append(out, port.ProfileSummary{
			Name: name, Current: name == s.current, Registered: st.registered(), TokenErr: st.tokenErr,
			Backend: st.settings.Backend, CacheBytes: cache.Bytes,
		})
	}
	return out, nil
}

// exists says this machine keeps something of the profile: a directory, a
// table, a cache, or a token in any place one may be kept.
func (s *Service) exists(ctx context.Context, name string, st state) (bool, error) {
	known, err := s.known()
	if err != nil {
		return false, err
	}
	if slices.Contains(known, name) || len(st.tokens) > 0 {
		return true, nil
	}
	for _, v := range st.vaults[1:] {
		held, err := tokens(ctx, v)
		if errors.Is(err, port.ErrKeychainUnsupported) {
			continue
		}
		if err != nil {
			return false, err
		}
		if len(held) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// Info implements port.Profiles.
func (s *Service) Info(ctx context.Context, name string, withAccount bool) (port.ProfileInfo, error) {
	if name == "" {
		name = s.current
	}
	st, err := s.state(ctx, name)
	if err != nil {
		return port.ProfileInfo{}, err
	}
	exists, err := s.exists(ctx, name, st)
	if err != nil {
		return port.ProfileInfo{}, err
	}
	cache, err := s.caches.Status(name)
	if err != nil {
		return port.ProfileInfo{}, err
	}
	info := port.ProfileInfo{
		ProfileSettings: st.settings, Current: name == s.current, Exists: exists,
		TokenLocation: st.vaults[0].Location(), Tokens: st.tokens, TokenErr: st.tokenErr, Registered: st.registered(),
		Cache: cache,
	}
	if withAccount && info.Registered {
		info.Account, info.AccountErr = s.account(ctx, name)
	}
	return info, nil
}

// account is what the cloud says of the profile's account.
func (s *Service) account(ctx context.Context, name string) (port.AccountInfo, error) {
	if s.offline {
		return port.AccountInfo{}, port.ErrOffline
	}
	var info port.AccountInfo
	err := s.with(name, func(acc *account.Service) error {
		var err error
		info, err = acc.Info(ctx)
		return err
	})
	return info, err
}

// with runs do on the account use case of the profile.
func (s *Service) with(name string, do func(*account.Service) error) error {
	reg, closeReg, err := s.registries.Open(name)
	if err != nil {
		return err
	}
	return errors.Join(do(account.New(reg)), closeReg())
}

// Add implements port.Profiles.
func (s *Service) Add(ctx context.Context, name, code string) error {
	st, err := s.state(ctx, name)
	if err != nil {
		return err
	}
	if st.tokenErr != nil {
		return st.tokenErr
	}
	if st.registered() {
		return fmt.Errorf("%s: %w", name, port.ErrProfileRegistered)
	}
	if s.offline {
		return port.ErrOffline
	}
	return s.with(name, func(acc *account.Service) error { return acc.Register(ctx, code, false) })
}

// Remove implements port.Profiles.
func (s *Service) Remove(ctx context.Context, name string, opts port.RemoveProfileOptions) (port.ProfileRemoval, error) {
	st, err := s.state(ctx, name)
	if err != nil {
		return port.ProfileRemoval{}, err
	}
	exists, err := s.exists(ctx, name, st)
	if err != nil {
		return port.ProfileRemoval{}, err
	}
	if !exists {
		return port.ProfileRemoval{}, fmt.Errorf("%s: %w", name, port.ErrNoProfile)
	}
	done := port.ProfileRemoval{Name: name, ConfigFile: st.settings.ConfigFile}
	if opts.Revoke {
		if s.offline {
			return done, port.ErrOffline
		}
		if st.tokenErr != nil {
			return done, st.tokenErr
		}
		if st.registered() {
			// Logout revokes first and drops the tokens only once the cloud
			// has let the device go: a failed revoke changes nothing.
			if err = s.with(name, func(acc *account.Service) error { return acc.Logout(ctx, true) }); err != nil {
				return done, err
			}
			done.Revoked = true
		}
	}
	for i, v := range st.vaults {
		removed, dropErr := drop(ctx, v)
		if len(removed) > 0 {
			done.Tokens = append(done.Tokens, port.RemovedTokens{Backend: v.Backend(), Location: v.Location(), Names: removed})
		}
		switch {
		case errors.Is(dropErr, port.ErrKeychainUnsupported):
			// A place this build cannot reach holds nothing this build put
			// there; it is worth a word only when the profile keeps its
			// tokens there.
			if i == 0 {
				done.Unreachable = append(done.Unreachable, v.Backend())
			}
		case dropErr != nil:
			return done, fmt.Errorf("remove the tokens from the %s: %w", v.Backend(), dropErr)
		}
	}
	if done.Dir, err = s.store.RemoveDir(name); err != nil {
		return done, fmt.Errorf("remove the profile's directory: %w", err)
	}
	if done.Cache, err = s.caches.Remove(name); err != nil {
		return done, fmt.Errorf("remove the profile's cache: %w", err)
	}
	if done.Table, err = s.store.RemoveTable(name); err != nil {
		return done, fmt.Errorf("remove the profile's tables from %s: %w", st.settings.ConfigFile, err)
	}
	return done, nil
}

// drop removes every secret v holds and names those it removed.
func drop(ctx context.Context, v port.SecretVault) ([]string, error) {
	held, err := tokens(ctx, v)
	if err != nil {
		return nil, err
	}
	for i, name := range held {
		if err := v.Delete(ctx, name); err != nil {
			return held[:i], err
		}
	}
	return held, nil
}
